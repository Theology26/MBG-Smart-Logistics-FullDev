package agent

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"

	"go-api/internal/config"
	"go-api/internal/models"
	"go-api/internal/services/osrm"

	"gorm.io/gorm"
)

// ============================================================================
// AgentService — Predictive Scheduler & Dynamic ETA Calculator
// ============================================================================
//
// Modul ini menangani dua skenario utama:
//
//   SKENARIO 1: Traffic Memory & Schedule Shifter
//     → Membaca histori kemacetan di tabel traffic_history
//     → Menghitung buffer waktu tambahan per segmen rute
//     → Menggeser jadwal keberangkatan lebih awal secara otomatis
//     → Menggunakan algoritma konvergensi iteratif (maks 5 iterasi)
//
//   SKENARIO 2: Dynamic ETA (Waktu Tiba Dinamis)
//     → Dipicu setiap kali kurir menekan 'Selesai Drop-off'
//     → Mengambil GPS terakhir kurir dari database
//     → Menghitung ulang ETA per sekolah yang tersisa via OSRM
//     → Menerapkan faktor kemacetan berdasarkan jam saat itu
//     → Mendeteksi sekolah yang berisiko melampaui deadline
//     → Menyimpan ETA baru ke database (kolom dynamic_eta)
//
// ============================================================================

// AgentService menyediakan kemampuan AI untuk prediksi jadwal dan ETA dinamis.
// Berbeda dari Scheduler (yang menangani orchestrasi penuh PlanDelivery),
// AgentService fokus pada dua fitur spesifik: schedule shifting dan ETA update.
type AgentService struct {
	DB     *gorm.DB        // Koneksi database PostgreSQL
	OSRM   *osrm.Client    // Client ke OSRM Docker (routing engine)
	Memory *TrafficMemory  // Akses ke traffic_history (memory Agent)
	Config *config.Config  // Konfigurasi environment
}

// NewAgentService membuat instance baru AgentService dengan semua dependency.
func NewAgentService(db *gorm.DB, cfg *config.Config) *AgentService {
	return &AgentService{
		DB:     db,
		OSRM:   osrm.NewClient(cfg.OSRMBaseURL),
		Memory: NewTrafficMemory(db),
		Config: cfg,
	}
}

// ╔═══════════════════════════════════════════════════════════════════════════╗
// ║                                                                         ║
// ║   SKENARIO 1: TRAFFIC MEMORY & SCHEDULE SHIFTER                         ║
// ║                                                                         ║
// ╚═══════════════════════════════════════════════════════════════════════════╝

// ────────────────── DATA TYPES untuk Skenario 1 ──────────────────

// RouteSegment merepresentasikan satu kaki perjalanan (dari titik A ke titik B).
type RouteSegment struct {
	FromName string  `json:"from_name"` // Nama titik asal (contoh: "Dapur Utama MBG")
	ToName   string  `json:"to_name"`   // Nama titik tujuan (contoh: "SDN Lowokwaru 3")
	FromArea string  `json:"from_area"` // Kecamatan asal (untuk lookup traffic_history)
	ToArea   string  `json:"to_area"`   // Kecamatan tujuan
	FromLat  float64 `json:"from_lat"`  // Latitude asal
	FromLng  float64 `json:"from_lng"`  // Longitude asal
	ToLat    float64 `json:"to_lat"`    // Latitude tujuan
	ToLng    float64 `json:"to_lng"`    // Longitude tujuan
}

// SegmentTrafficAnalysis adalah hasil analisis traffic untuk satu segmen rute.
// Agent membandingkan estimasi OSRM vs kenyataan historis untuk menentukan buffer.
type SegmentTrafficAnalysis struct {
	Segment            RouteSegment `json:"segment"`
	OSRMDurationSec    int          `json:"osrm_duration_sec"`    // Estimasi OSRM (teori, tanpa macet)
	HistAvgDurationSec int          `json:"hist_avg_duration_sec"` // Rata-rata aktual dari traffic_history
	CongestionFactor   float64      `json:"congestion_factor"`    // Rasio aktual/estimasi (1.0=lancar, 1.8=macet)
	SampleCount        int          `json:"sample_count"`         // Jumlah data historis yang digunakan
	Confidence         string       `json:"confidence"`           // "none"/"low"/"medium"/"high"
	ExtraBufferSec     int          `json:"extra_buffer_sec"`     // Detik tambahan yang harus ditambahkan
	IsPeakHour         bool         `json:"is_peak_hour"`         // Apakah ini jam puncak kemacetan?
}

// ScheduleShiftRequest adalah input dari handler untuk meminta analisis jadwal.
type ScheduleShiftRequest struct {
	RoutePlanID string `json:"route_plan_id" binding:"required"` // ID route_plan yang akan dianalisis
}

// ScheduleShiftResult adalah rekomendasi Agent setelah menganalisis traffic.
// Berisi jadwal keberangkatan yang sudah digeser plus penjelasan reasoning.
type ScheduleShiftResult struct {
	RoutePlanID          string                   `json:"route_plan_id"`
	OriginalDeparture    time.Time                `json:"original_departure"`     // Jadwal awal
	RecommendedDeparture time.Time                `json:"recommended_departure"`  // Jadwal yang direkomendasikan
	ShiftAmountSeconds   int                      `json:"shift_amount_seconds"`   // Total detik penggeseran
	ShiftAmountMinutes   float64                  `json:"shift_amount_minutes"`   // Total menit penggeseran
	TotalRouteOriginal   int                      `json:"total_route_original"`   // Total waktu rute (OSRM, detik)
	TotalRouteAdjusted   int                      `json:"total_route_adjusted"`   // Total waktu rute (setelah adjustment)
	Segments             []SegmentTrafficAnalysis `json:"segments"`               // Analisis per segmen
	RiskLevel            string                   `json:"risk_level"`             // "safe"/"moderate"/"high"/"critical"
	AgentReasoning       []string                 `json:"agent_reasoning"`        // Log pemikiran step-by-step Agent
	FoodDeadline         time.Time                `json:"food_deadline"`          // Batas waktu dari shelf-life
	TimeToDeadlineMin    float64                  `json:"time_to_deadline_min"`   // Menit tersisa setelah deliveri terakhir
	Feasible             bool                     `json:"feasible"`               // Apakah bisa dikirim tepat waktu?
	IterationsUsed       int                      `json:"iterations_used"`        // Berapa kali Agent iterasi konvergensi
	ScheduleUpdated      bool                     `json:"schedule_updated"`       // Apakah database sudah di-update?
}

// ────────────────── ALGORITMA UTAMA Skenario 1 ──────────────────

// AnalyzeAndShiftSchedule menganalisis rencana rute yang sudah ada,
// memeriksa histori kemacetan di setiap segmen, dan menggeser jadwal
// keberangkatan lebih awal jika data historis menunjukkan durasi
// perjalanan yang lebih lama dari estimasi OSRM.
//
// ALGORITMA (Iterative Convergence):
//   Iterasi 1: Hitung buffer berdasarkan jam keberangkatan asli
//   Iterasi 2: Geser jadwal → hitung ulang dengan jam baru (mungkin berbeda)
//   Iterasi 3: Cek apakah sudah konvergen (buffer stabil) → stop
//   Maksimal 5 iterasi untuk menghindari infinite loop.
//
// Contoh:
//   Keberangkatan asli: 10:00, rute ke Sukun jam 11 (macet, factor 1.8)
//   → Buffer = 15 menit → Geser ke 09:45
//   → Di jam 09:45, congestion di Sukun baru jam 10 (factor 1.5)
//   → Buffer = 10 menit → Geser ke 09:50
//   → Konvergen!
func (s *AgentService) AnalyzeAndShiftSchedule(ctx context.Context, routePlanID string) (*ScheduleShiftResult, error) {

	log.Println("╔═══════════════════════════════════════════════════════════════╗")
	log.Println("║  🧠 AGENT SKENARIO 1 — Traffic Analysis & Schedule Shift    ║")
	log.Println("╚═══════════════════════════════════════════════════════════════╝")

	// ── LANGKAH 1: Load route plan beserta semua stop-nya dari database ──
	var routePlan models.RoutePlan
	err := s.DB.
		Preload("Stops", func(db *gorm.DB) *gorm.DB {
			return db.Order("stop_sequence ASC") // Urut berdasarkan sequence
		}).
		Preload("Stops.SchoolAssignment.School"). // Load data sekolah untuk setiap stop
		Preload("Kitchen").                       // Load data dapur (depot)
		First(&routePlan, "id = ?", routePlanID).Error
	if err != nil {
		return nil, fmt.Errorf("route plan '%s' tidak ditemukan: %w", routePlanID, err)
	}

	// ── LANGKAH 2: Load production log untuk mendapatkan deadline makanan ──
	var production models.ProductionLog
	err = s.DB.First(&production, "id = ?", routePlan.ProductionLogID).Error
	if err != nil {
		return nil, fmt.Errorf("production log tidak ditemukan: %w", err)
	}

	// Inisialisasi variabel utama
	originalDeparture := routePlan.PlannedDeparture     // Jadwal keberangkatan asli
	foodDeadline := production.DeadlineAt               // Batas waktu makanan (hard constraint)
	dayOfWeek := int(originalDeparture.Weekday())       // Hari dalam seminggu (0=Minggu)
	reasoning := make([]string, 0)                      // Log reasoning Agent

	reasoning = append(reasoning, fmt.Sprintf(
		"📋 Menganalisis rute %s: %d stop, keberangkatan asli %s, deadline %s",
		routePlanID, len(routePlan.Stops),
		originalDeparture.Format("15:04"), foodDeadline.Format("15:04")))

	// ── LANGKAH 3: Bangun daftar segmen rute (depot→stop1→stop2→...→stopN) ──
	segments := s.buildRouteSegments(routePlan)
	reasoning = append(reasoning, fmt.Sprintf("🔗 Membangun %d segmen rute untuk dianalisis", len(segments)))

	// ── LANGKAH 4: Algoritma Iterative Convergence ──
	// Agent mengiterasi untuk menemukan jadwal keberangkatan yang stabil.
	// Setiap iterasi: hitung buffer → geser jadwal → cek apakah buffer berubah.
	const maxIterations = 5                    // Batas maksimal iterasi
	const convergenceThresholdSec = 60         // Threshold konvergensi: 1 menit
	currentDeparture := originalDeparture      // Dimulai dari jadwal asli
	var lastTotalBuffer int                    // Buffer iterasi sebelumnya
	var finalSegmentAnalyses []SegmentTrafficAnalysis // Hasil analisis final
	iteration := 0

	for iteration = 1; iteration <= maxIterations; iteration++ {
		reasoning = append(reasoning, fmt.Sprintf(
			"🔄 Iterasi %d: Menghitung buffer dengan keberangkatan %s...",
			iteration, currentDeparture.Format("15:04:05")))

		// Hitung buffer untuk setiap segmen rute pada jam keberangkatan saat ini
		segmentAnalyses, totalOSRM, totalAdjusted := s.analyzeAllSegments(
			segments, dayOfWeek, currentDeparture)

		// Total buffer = selisih antara waktu adjusted dan OSRM murni
		totalBuffer := totalAdjusted - totalOSRM

		reasoning = append(reasoning, fmt.Sprintf(
			"   📊 OSRM total: %ds (%s), Adjusted: %ds (%s), Buffer: %ds (%s)",
			totalOSRM, formatDuration(totalOSRM),
			totalAdjusted, formatDuration(totalAdjusted),
			totalBuffer, formatDuration(totalBuffer)))

		// Simpan hasil analisis iterasi terakhir
		finalSegmentAnalyses = segmentAnalyses

		// Cek konvergensi: apakah buffer berubah signifikan dari iterasi sebelumnya?
		bufferDelta := abs(totalBuffer - lastTotalBuffer)
		if iteration > 1 && bufferDelta < convergenceThresholdSec {
			// Buffer sudah stabil → konvergen!
			reasoning = append(reasoning, fmt.Sprintf(
				"✅ Konvergen! Delta buffer = %ds (< threshold %ds)", bufferDelta, convergenceThresholdSec))
			break
		}

		// Geser jadwal keberangkatan lebih awal sebesar total buffer
		currentDeparture = originalDeparture.Add(-time.Duration(totalBuffer) * time.Second)
		lastTotalBuffer = totalBuffer

		reasoning = append(reasoning, fmt.Sprintf(
			"   ⏰ Jadwal digeser ke %s (mundur %s dari asli)",
			currentDeparture.Format("15:04:05"), formatDuration(totalBuffer)))
	}

	// ── LANGKAH 5: Validasi — Pastikan jadwal tidak sebelum makanan matang ──
	if currentDeparture.Before(production.CookedAt) {
		// Tidak bisa berangkat sebelum makanan matang!
		currentDeparture = production.CookedAt
		reasoning = append(reasoning,
			"⚠️  PERINGATAN: Jadwal digeser melebihi waktu matang. Diset ke waktu matang (cooked_at).")
	}

	// ── LANGKAH 6: Hitung waktu tiba terakhir vs deadline ──
	totalRouteOriginal := sumOSRMDurations(finalSegmentAnalyses)
	totalRouteAdjusted := sumAdjustedDurations(finalSegmentAnalyses)
	serviceTimeTotal := len(routePlan.Stops) * s.Config.DefaultServiceTimeSeconds

	// Estimasi waktu tiba di sekolah terakhir
	lastArrivalEstimate := currentDeparture.Add(
		time.Duration(totalRouteAdjusted+serviceTimeTotal) * time.Second)
	timeToDeadline := foodDeadline.Sub(lastArrivalEstimate).Minutes()

	// ── LANGKAH 7: Tentukan risk level berdasarkan sisa waktu ke deadline ──
	riskLevel := classifyRisk(timeToDeadline)
	feasible := timeToDeadline > 0

	if !feasible {
		reasoning = append(reasoning, fmt.Sprintf(
			"🚨 KRITIS: Estimasi tiba terakhir %s MELAMPAUI deadline %s! Tambahkan kurir atau kurangi sekolah.",
			lastArrivalEstimate.Format("15:04"), foodDeadline.Format("15:04")))
	} else {
		reasoning = append(reasoning, fmt.Sprintf(
			"✅ Feasible: Estimasi tiba terakhir %s, sisa %.1f menit sebelum deadline %s",
			lastArrivalEstimate.Format("15:04"), timeToDeadline, foodDeadline.Format("15:04")))
	}

	// ── LANGKAH 8: Hitung total shift dari jadwal asli ──
	shiftSeconds := int(originalDeparture.Sub(currentDeparture).Seconds())

	// ── LANGKAH 9: Update database jika ada pergeseran signifikan ──
	scheduleUpdated := false
	if shiftSeconds >= 60 { // Hanya update jika shift ≥ 1 menit
		err = s.DB.Model(&models.RoutePlan{}).Where("id = ?", routePlanID).
			Update("planned_departure", currentDeparture).Error
		if err == nil {
			scheduleUpdated = true
			reasoning = append(reasoning, fmt.Sprintf(
				"💾 Database di-update: planned_departure = %s (mundur %s dari asli)",
				currentDeparture.Format("15:04:05"), formatDuration(shiftSeconds)))

			// Update juga estimasi arrival untuk setiap stop
			s.updateStopEstimates(routePlan.Stops, currentDeparture, finalSegmentAnalyses)
		}
	} else {
		reasoning = append(reasoning, "ℹ️  Shift < 1 menit — tidak perlu update database.")
	}

	log.Printf("🧠 [AGENT] Schedule shift selesai: %s → %s (mundur %s, risk: %s)",
		originalDeparture.Format("15:04"), currentDeparture.Format("15:04"),
		formatDuration(shiftSeconds), riskLevel)

	return &ScheduleShiftResult{
		RoutePlanID:          routePlanID,
		OriginalDeparture:    originalDeparture,
		RecommendedDeparture: currentDeparture,
		ShiftAmountSeconds:   shiftSeconds,
		ShiftAmountMinutes:   float64(shiftSeconds) / 60.0,
		TotalRouteOriginal:   totalRouteOriginal,
		TotalRouteAdjusted:   totalRouteAdjusted,
		Segments:             finalSegmentAnalyses,
		RiskLevel:            riskLevel,
		AgentReasoning:       reasoning,
		FoodDeadline:         foodDeadline,
		TimeToDeadlineMin:    timeToDeadline,
		Feasible:             feasible,
		IterationsUsed:       iteration,
		ScheduleUpdated:      scheduleUpdated,
	}, nil
}

// ────────────────── HELPER FUNCTIONS Skenario 1 ──────────────────

// buildRouteSegments membangun daftar segmen rute dari data route plan.
// Segmen pertama: Depot (dapur) → Sekolah pertama.
// Segmen berikutnya: Sekolah ke-i → Sekolah ke-(i+1).
func (s *AgentService) buildRouteSegments(plan models.RoutePlan) []RouteSegment {
	segments := make([]RouteSegment, 0, len(plan.Stops))

	// Dapatkan koordinat dapur sebagai titik awal
	var kitchen models.Kitchen
	s.DB.First(&kitchen, "id = ?", plan.KitchenID)

	// Titik "current" dimulai dari dapur
	prevName := kitchen.Name
	prevArea := "Dapur"
	prevLat := kitchen.Latitude
	prevLng := kitchen.Longitude

	for _, stop := range plan.Stops {
		// Ambil data sekolah dari relasi SchoolAssignment → School
		school := stop.SchoolAssignment.School
		if school == nil {
			continue // Skip jika data sekolah tidak ada
		}

		// Bangun segmen dari titik sebelumnya ke sekolah ini
		segments = append(segments, RouteSegment{
			FromName: prevName,
			ToName:   school.Name,
			FromArea: prevArea,
			ToArea:   school.Area,
			FromLat:  prevLat,
			FromLng:  prevLng,
			ToLat:    school.Latitude,
			ToLng:    school.Longitude,
		})

		// Update titik "current" ke sekolah ini untuk segmen berikutnya
		prevName = school.Name
		prevArea = school.Area
		prevLat = school.Latitude
		prevLng = school.Longitude
	}

	return segments
}

// analyzeAllSegments menganalisis setiap segmen rute: memanggil OSRM untuk
// estimasi durasi, lalu membandingkannya dengan data historis traffic_history.
func (s *AgentService) analyzeAllSegments(
	segments []RouteSegment,
	dayOfWeek int,
	departure time.Time,
) ([]SegmentTrafficAnalysis, int, int) {

	analyses := make([]SegmentTrafficAnalysis, 0, len(segments))
	totalOSRM := 0     // Akumulasi durasi OSRM (teori)
	totalAdjusted := 0 // Akumulasi durasi setelah adjustment
	currentTime := departure

	for _, seg := range segments {
		// 1. Tanya OSRM: berapa lama perjalanan A → B (tanpa traffic)?
		osrmDuration, _, err := s.OSRM.GetETABetweenPoints(
			osrm.Coordinate{Lat: seg.FromLat, Lng: seg.FromLng},
			osrm.Coordinate{Lat: seg.ToLat, Lng: seg.ToLng},
		)
		if err != nil {
			log.Printf("⚠️  [AGENT] OSRM gagal untuk segmen %s→%s: %v", seg.FromName, seg.ToName, err)
			osrmDuration = 900 // Fallback: 15 menit
		}

		// 2. Tentukan jam berapa kurir akan melewati segmen ini
		//    (estimasi berdasarkan waktu akumulasi)
		expectedHour := currentTime.Hour()

		// 3. Tanya Traffic Memory: bagaimana histori di rute ini pada hari & jam ini?
		congestion := s.Memory.GetCongestionFactor(
			seg.FromArea, seg.ToArea, dayOfWeek, expectedHour)

		// 4. Hitung durasi yang sudah disesuaikan dengan congestion factor
		//    Contoh: OSRM = 600s, factor = 1.8 → adjusted = 1080s
		adjustedDuration := float64(osrmDuration) * congestion.Factor

		// 5. Pasang cap 3x agar tidak terlalu ekstrem
		adjustedDuration = math.Min(adjustedDuration, float64(osrmDuration)*3.0)
		adjustedDurationInt := int(adjustedDuration)

		// 6. Hitung buffer = selisih antara adjusted dan OSRM asli
		extraBuffer := adjustedDurationInt - osrmDuration
		if extraBuffer < 0 {
			extraBuffer = 0 // Tidak boleh buffer negatif
		}

		// 7. Tentukan apakah ini jam puncak (congestion > 1.3)
		isPeak := congestion.Factor > 1.3

		// 8. Simpan hasil analisis segmen ini
		analyses = append(analyses, SegmentTrafficAnalysis{
			Segment:            seg,
			OSRMDurationSec:    osrmDuration,
			HistAvgDurationSec: adjustedDurationInt,
			CongestionFactor:   congestion.Factor,
			SampleCount:        congestion.SampleCount,
			Confidence:         congestion.Reliability,
			ExtraBufferSec:     extraBuffer,
			IsPeakHour:         isPeak,
		})

		// 9. Akumulasi total durasi
		totalOSRM += osrmDuration
		totalAdjusted += adjustedDurationInt

		// 10. Majukan waktu simulasi ke titik berikutnya (travel + service time)
		currentTime = currentTime.Add(
			time.Duration(adjustedDurationInt+s.Config.DefaultServiceTimeSeconds) * time.Second)
	}

	return analyses, totalOSRM, totalAdjusted
}

// updateStopEstimates memperbarui estimated_arrival di setiap route_stop
// berdasarkan jadwal keberangkatan baru dan durasi yang sudah di-adjust.
func (s *AgentService) updateStopEstimates(
	stops []models.RouteStop,
	departure time.Time,
	analyses []SegmentTrafficAnalysis,
) {
	currentTime := departure

	for i, analysis := range analyses {
		if i >= len(stops) {
			break
		}

		// Waktu tiba = waktu saat ini + durasi perjalanan (adjusted)
		arrivalTime := currentTime.Add(time.Duration(analysis.HistAvgDurationSec) * time.Second)

		// Update estimated_arrival dan dynamic_eta di database
		s.DB.Model(&models.RouteStop{}).Where("id = ?", stops[i].ID).Updates(map[string]interface{}{
			"estimated_arrival": arrivalTime,
			"dynamic_eta":       arrivalTime,
		})

		// Waktu berangkat dari stop ini = waktu tiba + service time (bongkar muat)
		currentTime = arrivalTime.Add(
			time.Duration(s.Config.DefaultServiceTimeSeconds) * time.Second)
	}
}

// ╔═══════════════════════════════════════════════════════════════════════════╗
// ║                                                                         ║
// ║   SKENARIO 2: DYNAMIC ETA (WAKTU TIBA DINAMIS)                          ║
// ║                                                                         ║
// ╚═══════════════════════════════════════════════════════════════════════════╝

// ────────────────── DATA TYPES untuk Skenario 2 ──────────────────

// ETARecalcRequest adalah input ketika kurir menekan tombol 'Selesai Drop-off'.
type ETARecalcRequest struct {
	RoutePlanID     string  `json:"route_plan_id" binding:"required"`     // ID rute aktif
	CompletedStopID string  `json:"completed_stop_id" binding:"required"` // ID stop yang baru selesai
	CourierLat      float64 `json:"courier_lat" binding:"required"`       // Latitude GPS kurir saat ini
	CourierLng      float64 `json:"courier_lng" binding:"required"`       // Longitude GPS kurir saat ini
	GPSAccuracy     float64 `json:"gps_accuracy"`                        // Akurasi GPS dalam meter
}

// StopETAUpdate berisi ETA baru untuk satu sekolah yang belum dikunjungi.
type StopETAUpdate struct {
	StopID               string     `json:"stop_id"`
	StopSequence         int        `json:"stop_sequence"`
	SchoolID             string     `json:"school_id"`
	SchoolName           string     `json:"school_name"`
	SchoolArea           string     `json:"school_area"`
	PreviousETA          *time.Time `json:"previous_eta"`           // ETA sebelumnya (dari dynamic_eta)
	NewETA               time.Time  `json:"new_eta"`                // ETA baru yang dihitung
	ETAChangeSeconds     int        `json:"eta_change_seconds"`     // Perubahan (positif=lebih lambat)
	TravelFromPrevSec    int        `json:"travel_from_prev_sec"`   // Durasi perjalanan dari titik sebelumnya
	OSRMRawDurationSec   int        `json:"osrm_raw_duration_sec"`  // Durasi OSRM murni (tanpa congestion)
	CongestionFactor     float64    `json:"congestion_factor"`      // Faktor kemacetan yang diterapkan
	CongestionConfidence string     `json:"congestion_confidence"`  // "none"/"low"/"medium"/"high"
	PortionsToDeliver    int        `json:"portions_to_deliver"`
	DeadlineAt           time.Time  `json:"deadline_at"`
	MinutesUntilDeadline float64    `json:"minutes_until_deadline"` // Menit tersisa sebelum basi
	IsAtRisk             bool       `json:"is_at_risk"`             // Apakah ETA mendekati/melampaui deadline?
	RiskReason           string     `json:"risk_reason,omitempty"`  // Penjelasan risiko
}

// DynamicETAResult adalah output lengkap dari perhitungan ulang ETA.
type DynamicETAResult struct {
	RoutePlanID      string        `json:"route_plan_id"`
	CompletedStopID  string        `json:"completed_stop_id"`
	CourierPosition  CourierGPS    `json:"courier_position"`
	Timestamp        time.Time     `json:"timestamp"`
	DeadlineAt       time.Time     `json:"deadline_at"`
	CompletedStops   int           `json:"completed_stops"`
	RemainingStops   int           `json:"remaining_stops"`
	Updates          []StopETAUpdate `json:"updates"`
	AnyAtRisk        bool          `json:"any_at_risk"`        // Apakah ada sekolah yang berisiko?
	AtRiskCount      int           `json:"at_risk_count"`      // Berapa sekolah yang berisiko
	RouteStatus      string        `json:"route_status"`       // "on_track"/"delayed"/"critical"
	RecalcDurationMs int64         `json:"recalc_duration_ms"` // Berapa lama proses recalc (ms)
	AgentNotes       []string      `json:"agent_notes"`        // Catatan dan peringatan dari Agent
}

// CourierGPS merepresentasikan posisi GPS kurir saat ini.
type CourierGPS struct {
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	Accuracy  float64   `json:"accuracy_meters"` // Akurasi GPS dalam meter
	Timestamp time.Time `json:"timestamp"`
}

// ────────────────── ALGORITMA UTAMA Skenario 2 ──────────────────

// RecalculateDynamicETA menghitung ulang estimasi waktu tiba untuk
// semua sekolah yang belum dikunjungi setelah kurir menyelesaikan
// satu drop-off.
//
// ALGORITMA:
//   1. Ambil GPS kurir saat ini
//   2. Snap ke jalan terdekat (OSRM nearest) untuk akurasi
//   3. Ambil semua stop yang masih 'pending' (belum dikunjungi)
//   4. Untuk setiap stop (berurutan):
//      a. Hitung durasi perjalanan dari posisi terakhir via OSRM
//      b. Tentukan jam estimasi tiba
//      c. Terapkan faktor kemacetan untuk jam tersebut dari traffic_history
//      d. Hitung ETA baru = waktu sekarang + durasi_adjusted
//      e. Cek apakah ETA melampaui deadline makanan (at_risk)
//      f. Update kolom dynamic_eta di tabel route_stops
//   5. Propagasi berantai: posisi "current" pindah ke sekolah terkini
//      plus service time, untuk menghitung ETA stop berikutnya
//   6. Deteksi dan laporkan sekolah yang at_risk
func (s *AgentService) RecalculateDynamicETA(ctx context.Context, req ETARecalcRequest) (*DynamicETAResult, error) {

	startTime := time.Now()
	agentNotes := make([]string, 0)

	log.Println("╔═══════════════════════════════════════════════════════════════╗")
	log.Println("║  🧠 AGENT SKENARIO 2 — Dynamic ETA Recalculation           ║")
	log.Println("╚═══════════════════════════════════════════════════════════════╝")

	// ── LANGKAH 1: Snap posisi GPS kurir ke jalan terdekat ──
	// GPS di area padat seperti Dinoyo bisa meleset 10-30 meter.
	// OSRM nearest service mengoreksi ke titik di jalan terdekat.
	courierCoord := osrm.Coordinate{Lat: req.CourierLat, Lng: req.CourierLng}
	snappedCoord, err := s.OSRM.SnapToRoad(courierCoord)
	if err != nil {
		// Jika snap gagal, gunakan koordinat mentah
		log.Printf("⚠️  [AGENT] Snap-to-road gagal, menggunakan GPS mentah: %v", err)
		snappedCoord = &courierCoord
		agentNotes = append(agentNotes, "GPS tidak bisa di-snap ke jalan terdekat, menggunakan koordinat mentah")
	} else {
		agentNotes = append(agentNotes, fmt.Sprintf(
			"GPS di-snap ke jalan terdekat: (%.6f, %.6f) → (%.6f, %.6f)",
			req.CourierLat, req.CourierLng, snappedCoord.Lat, snappedCoord.Lng))
	}

	// ── LANGKAH 2: Load route plan dan deadline makanan ──
	var routePlan models.RoutePlan
	err = s.DB.First(&routePlan, "id = ?", req.RoutePlanID).Error
	if err != nil {
		return nil, fmt.Errorf("route plan '%s' tidak ditemukan: %w", req.RoutePlanID, err)
	}

	var production models.ProductionLog
	err = s.DB.First(&production, "id = ?", routePlan.ProductionLogID).Error
	if err != nil {
		return nil, fmt.Errorf("production log tidak ditemukan: %w", err)
	}

	foodDeadline := production.DeadlineAt // Batas waktu makanan masih aman

	// ── LANGKAH 3: Tandai stop yang baru selesai sebagai 'delivered' ──
	now := time.Now()
	s.DB.Model(&models.RouteStop{}).Where("id = ?", req.CompletedStopID).Updates(map[string]interface{}{
		"status":         "delivered",
		"actual_arrival": now,
	})
	agentNotes = append(agentNotes, fmt.Sprintf("Stop %s ditandai sebagai 'delivered' pada %s",
		req.CompletedStopID, now.Format("15:04:05")))

	// ── LANGKAH 4: Hitung berapa stop yang sudah selesai dan yang tersisa ──
	var completedCount int64
	s.DB.Model(&models.RouteStop{}).
		Where("route_plan_id = ? AND status = 'delivered'", req.RoutePlanID).
		Count(&completedCount)

	// ── LANGKAH 5: Ambil semua stop yang masih 'pending' (belum dikunjungi) ──
	var remainingStops []models.RouteStop
	s.DB.Where("route_plan_id = ? AND status = 'pending'", req.RoutePlanID).
		Order("stop_sequence ASC"). // Penting: urut berdasarkan sequence!
		Preload("SchoolAssignment.School").
		Find(&remainingStops)

	// Jika tidak ada stop tersisa, rute sudah selesai
	if len(remainingStops) == 0 {
		log.Println("🧠 [AGENT] Tidak ada stop tersisa — rute selesai!")
		s.DB.Model(&models.RoutePlan{}).Where("id = ?", req.RoutePlanID).Updates(map[string]interface{}{
			"status":       "completed",
			"completed_at": now,
		})

		// Record trip data ke traffic_history untuk pembelajaran
		s.recordCompletedRoute(routePlan)

		return &DynamicETAResult{
			RoutePlanID:      req.RoutePlanID,
			CompletedStopID:  req.CompletedStopID,
			CourierPosition:  CourierGPS{Latitude: snappedCoord.Lat, Longitude: snappedCoord.Lng, Accuracy: req.GPSAccuracy, Timestamp: now},
			Timestamp:        now,
			DeadlineAt:       foodDeadline,
			CompletedStops:   int(completedCount),
			RemainingStops:   0,
			Updates:          []StopETAUpdate{},
			RouteStatus:      "completed",
			RecalcDurationMs: time.Since(startTime).Milliseconds(),
			AgentNotes:       append(agentNotes, "✅ Semua stop selesai — rute completed!"),
		}, nil
	}

	log.Printf("🧠 [AGENT] %d stop tersisa, menghitung ETA dari posisi kurir (%.6f, %.6f)...",
		len(remainingStops), snappedCoord.Lat, snappedCoord.Lng)

	// ── LANGKAH 6: Hitung ETA baru secara berantai (chain calculation) ──
	// Posisi "current" dimulai dari GPS kurir, lalu berpindah ke setiap sekolah
	// setelah dikunjungi. Waktu akumulasi = travel time + service time per stop.
	currentLat := snappedCoord.Lat
	currentLng := snappedCoord.Lng
	currentTime := now            // Waktu acuan dimulai dari sekarang
	dayOfWeek := int(now.Weekday())

	updates := make([]StopETAUpdate, 0, len(remainingStops))
	atRiskCount := 0

	for i, stop := range remainingStops {
		// 6a. Ambil data sekolah tujuan dari relasi
		school := stop.SchoolAssignment.School
		if school == nil {
			agentNotes = append(agentNotes, fmt.Sprintf(
				"⚠️  Stop #%d: data sekolah tidak ditemukan, dilewati", stop.StopSequence))
			continue
		}

		// 6b. Panggil OSRM: berapa detik dari posisi sekarang ke sekolah ini?
		osrmDuration, _, err := s.OSRM.GetETABetweenPoints(
			osrm.Coordinate{Lat: currentLat, Lng: currentLng},
			osrm.Coordinate{Lat: school.Latitude, Lng: school.Longitude},
		)
		if err != nil {
			log.Printf("⚠️  [AGENT] OSRM gagal untuk stop #%d (%s): %v",
				stop.StopSequence, school.Name, err)
			osrmDuration = 600 // Fallback: 10 menit
			agentNotes = append(agentNotes, fmt.Sprintf(
				"⚠️  OSRM gagal untuk %s, menggunakan fallback 10 menit", school.Name))
		}

		// 6c. Tentukan jam estimasi melewati segmen ini
		expectedArrivalHour := currentTime.Add(time.Duration(osrmDuration) * time.Second).Hour()

		// 6d. Terapkan faktor kemacetan dari traffic_history untuk jam tersebut
		// Contoh: Rute ke Sukun jam 11, congestion factor = 1.8
		//         OSRM bilang 15 menit → disesuaikan jadi 27 menit
		var prevArea string
		if i == 0 {
			prevArea = "current" // Stop pertama: dari posisi kurir
		} else {
			// Stop sebelumnya sebagai referensi area
			prevSchool := remainingStops[i-1].SchoolAssignment.School
			if prevSchool != nil {
				prevArea = prevSchool.Area
			} else {
				prevArea = "Unknown"
			}
		}

		congestion := s.Memory.GetCongestionFactor(
			prevArea, school.Area, dayOfWeek, expectedArrivalHour)

		// 6e. Hitung durasi yang sudah disesuaikan
		adjustedDuration := float64(osrmDuration) * congestion.Factor
		adjustedDuration = math.Min(adjustedDuration, float64(osrmDuration)*3.0) // Cap 3x
		adjustedDurationInt := int(adjustedDuration)

		// 6f. Hitung ETA baru
		newETA := currentTime.Add(time.Duration(adjustedDurationInt) * time.Second)

		// 6g. Hitung perubahan dari ETA sebelumnya
		var etaChange int
		if stop.DynamicETA != nil {
			etaChange = int(newETA.Sub(*stop.DynamicETA).Seconds())
		}

		// 6h. Cek apakah ETA melampaui deadline makanan
		minutesToDeadline := foodDeadline.Sub(newETA).Minutes()
		isAtRisk := false
		riskReason := ""

		if newETA.After(foodDeadline) {
			// 🚨 ETA MELAMPAUI deadline — makanan bisa basi!
			isAtRisk = true
			riskReason = fmt.Sprintf("ETA %s melewati deadline %s — makanan berisiko basi!",
				newETA.Format("15:04"), foodDeadline.Format("15:04"))
			atRiskCount++
			agentNotes = append(agentNotes, fmt.Sprintf(
				"🚨 KRITIS: %s — %s", school.Name, riskReason))
		} else if minutesToDeadline < 15 {
			// ⚠️ Kurang dari 15 menit sebelum deadline — sangat mepet
			isAtRisk = true
			riskReason = fmt.Sprintf("Hanya %.0f menit sebelum deadline — sangat mepet!", minutesToDeadline)
			atRiskCount++
			agentNotes = append(agentNotes, fmt.Sprintf(
				"⚠️  MEPET: %s — sisa %.0f menit ke deadline", school.Name, minutesToDeadline))
		}

		// 6i. Simpan ETA baru ke database (kolom dynamic_eta)
		s.DB.Model(&models.RouteStop{}).Where("id = ?", stop.ID).
			Update("dynamic_eta", newETA)

		log.Printf("🧠 [AGENT] Stop #%d %s: ETA = %s (OSRM: %ds × %.2f = %ds, sisa: %.0f min)",
			stop.StopSequence, school.Name,
			newETA.Format("15:04:05"),
			osrmDuration, congestion.Factor, adjustedDurationInt,
			minutesToDeadline)

		// 6j. Simpan hasil update untuk response
		updates = append(updates, StopETAUpdate{
			StopID:               stop.ID,
			StopSequence:         stop.StopSequence,
			SchoolID:             school.ID,
			SchoolName:           school.Name,
			SchoolArea:           school.Area,
			PreviousETA:          stop.DynamicETA,
			NewETA:               newETA,
			ETAChangeSeconds:     etaChange,
			TravelFromPrevSec:    adjustedDurationInt,
			OSRMRawDurationSec:   osrmDuration,
			CongestionFactor:     congestion.Factor,
			CongestionConfidence: congestion.Reliability,
			PortionsToDeliver:    stop.PortionsToDeliver,
			DeadlineAt:           foodDeadline,
			MinutesUntilDeadline: minutesToDeadline,
			IsAtRisk:             isAtRisk,
			RiskReason:           riskReason,
		})

		// 6k. PROPAGASI BERANTAI: Pindahkan "posisi current" ke sekolah ini
		//     dan tambahkan service time (waktu bongkar muat di sekolah, default 5 menit)
		currentLat = school.Latitude
		currentLng = school.Longitude
		currentTime = newETA.Add(time.Duration(s.Config.DefaultServiceTimeSeconds) * time.Second)
	}

	// ── LANGKAH 7: Update status rute berdasarkan analisis ──
	routeStatus := "on_track"
	if atRiskCount > 0 && atRiskCount < len(remainingStops) {
		routeStatus = "delayed"
		// Update route plan status ke 'active' jika belum
		s.DB.Model(&models.RoutePlan{}).Where("id = ? AND status = 'planned'", req.RoutePlanID).
			Update("status", "active")
	} else if atRiskCount >= len(remainingStops) {
		routeStatus = "critical"
		agentNotes = append(agentNotes,
			"🚨 SEMUA stop tersisa berisiko melampaui deadline. Pertimbangkan bantuan kurir tambahan!")
	}

	recalcDuration := time.Since(startTime)
	log.Printf("🧠 [AGENT] ETA recalc selesai dalam %dms: %d updates, %d at-risk, status: %s",
		recalcDuration.Milliseconds(), len(updates), atRiskCount, routeStatus)

	return &DynamicETAResult{
		RoutePlanID:     req.RoutePlanID,
		CompletedStopID: req.CompletedStopID,
		CourierPosition: CourierGPS{
			Latitude:  snappedCoord.Lat,
			Longitude: snappedCoord.Lng,
			Accuracy:  req.GPSAccuracy,
			Timestamp: now,
		},
		Timestamp:        now,
		DeadlineAt:       foodDeadline,
		CompletedStops:   int(completedCount),
		RemainingStops:   len(remainingStops),
		Updates:          updates,
		AnyAtRisk:        atRiskCount > 0,
		AtRiskCount:      atRiskCount,
		RouteStatus:      routeStatus,
		RecalcDurationMs: recalcDuration.Milliseconds(),
		AgentNotes:       agentNotes,
	}, nil
}

// ────────────────── HELPER FUNCTIONS Skenario 2 ──────────────────

// recordCompletedRoute menyimpan data perjalanan yang sudah selesai ke traffic_history.
// Ini adalah fungsi "belajar" — Agent merekam pengalaman untuk prediksi di masa depan.
func (s *AgentService) recordCompletedRoute(plan models.RoutePlan) {
	// Load semua stop dengan data sekolah dan waktu aktual
	var stops []models.RouteStop
	s.DB.Where("route_plan_id = ?", plan.ID).
		Order("stop_sequence ASC").
		Preload("SchoolAssignment.School").
		Find(&stops)

	var kitchen models.Kitchen
	s.DB.First(&kitchen, "id = ?", plan.KitchenID)

	// Titik sebelumnya dimulai dari dapur
	prevArea := "Dapur"
	prevLat := kitchen.Latitude
	prevLng := kitchen.Longitude
	prevDepartureTime := plan.PlannedDeparture
	if plan.ActualDeparture != nil {
		prevDepartureTime = *plan.ActualDeparture
	}

	for _, stop := range stops {
		school := stop.SchoolAssignment.School
		if school == nil || stop.ActualArrival == nil {
			continue
		}

		// Hitung durasi aktual berdasarkan waktu tiba sebenarnya
		actualDuration := int(stop.ActualArrival.Sub(prevDepartureTime).Seconds())
		if actualDuration <= 0 {
			continue
		}

		// Hitung estimasi OSRM untuk segmen ini
		osrmDuration, _, err := s.OSRM.GetETABetweenPoints(
			osrm.Coordinate{Lat: prevLat, Lng: prevLng},
			osrm.Coordinate{Lat: school.Latitude, Lng: school.Longitude},
		)
		if err != nil {
			continue
		}

		// Rekam ke traffic_history — Agent belajar dari pengalaman
		record := TrafficRecord{
			OriginArea:            prevArea,
			DestinationArea:       school.Area,
			OriginLat:             prevLat,
			OriginLng:             prevLng,
			DestLat:               school.Latitude,
			DestLng:               school.Longitude,
			DayOfWeek:             int(stop.ActualArrival.Weekday()),
			HourOfDay:             prevDepartureTime.Hour(),
			ActualDurationSeconds: actualDuration,
			OSRMEstimatedSeconds:  osrmDuration,
			RecordedDate:          stop.ActualArrival.Format("2006-01-02"),
		}
		s.Memory.RecordTrip(record)

		// Update referensi untuk segmen berikutnya
		prevArea = school.Area
		prevLat = school.Latitude
		prevLng = school.Longitude
		if stop.ActualDeparture != nil {
			prevDepartureTime = *stop.ActualDeparture
		}
	}
}

// ╔═══════════════════════════════════════════════════════════════════════════╗
// ║   UTILITY FUNCTIONS                                                     ║
// ╚═══════════════════════════════════════════════════════════════════════════╝

// classifyRisk menentukan level risiko berdasarkan menit tersisa ke deadline.
func classifyRisk(minutesToDeadline float64) string {
	switch {
	case minutesToDeadline <= 0:
		return "critical" // Sudah melampaui deadline!
	case minutesToDeadline < 10:
		return "high" // Kurang dari 10 menit — darurat
	case minutesToDeadline < 30:
		return "moderate" // Kurang dari 30 menit — hati-hati
	default:
		return "safe" // Masih cukup waktu
	}
}

// formatDuration mengubah detik menjadi string yang mudah dibaca.
func formatDuration(seconds int) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	secs := seconds % 60
	if minutes < 60 {
		return fmt.Sprintf("%dm%ds", minutes, secs)
	}
	hours := minutes / 60
	mins := minutes % 60
	return fmt.Sprintf("%dh%dm", hours, mins)
}

// sumOSRMDurations menjumlahkan durasi OSRM dari semua analisis segmen.
func sumOSRMDurations(analyses []SegmentTrafficAnalysis) int {
	total := 0
	for _, a := range analyses {
		total += a.OSRMDurationSec
	}
	return total
}

// sumAdjustedDurations menjumlahkan durasi yang sudah di-adjust dari semua segmen.
func sumAdjustedDurations(analyses []SegmentTrafficAnalysis) int {
	total := 0
	for _, a := range analyses {
		total += a.HistAvgDurationSec
	}
	return total
}

// abs mengembalikan nilai absolut dari integer.
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
