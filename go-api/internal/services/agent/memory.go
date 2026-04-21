package agent

import (
	"log"
	"math"

	"gorm.io/gorm"
)

// ============================================================================
// Traffic Memory — AI Agent's Historical Pattern Recognition (PILAR 3)
// ============================================================================
// The Traffic Memory system learns from historical delivery data to predict
// congestion patterns in Kota Malang. It adjusts OSRM's theoretical
// duration estimates with real-world congestion factors.
//
// Key Malang congestion areas (seeded in migration):
//   - Dinoyo/Klojen: Heavy traffic 11:00-13:00 (factor ~1.8)
//   - Blimbing:      Heavy traffic 07:00-08:00, 16:00-17:30 (factor ~1.6)
//   - Sukun:         Moderate at midday (factor ~1.5)
//   - Lowokwaru:     School hour rush 07:00-08:30 (factor ~1.7)
// ============================================================================

// TrafficMemory provides historical traffic pattern analysis.
type TrafficMemory struct {
	DB *gorm.DB
}

// NewTrafficMemory creates a new TrafficMemory instance.
func NewTrafficMemory(db *gorm.DB) *TrafficMemory {
	return &TrafficMemory{DB: db}
}

// CongestionResult contains the congestion analysis for a route segment.
type CongestionResult struct {
	OriginArea      string  `json:"origin_area"`
	DestinationArea string  `json:"destination_area"`
	Factor          float64 `json:"factor"`           // 1.0 = normal, 1.5 = 50% slower
	SampleCount     int     `json:"sample_count"`     // number of historical data points
	Reliability     string  `json:"reliability"`      // low, medium, high
}

// GetCongestionFactor queries traffic_history for the average congestion
// factor between two areas at a specific day and hour.
//
// Returns: congestion multiplier (1.0 = no congestion, 2.0 = double the time)
func (m *TrafficMemory) GetCongestionFactor(originArea, destArea string, dayOfWeek, hourOfDay int) CongestionResult {
	var avgFactor float64
	var sampleCount int64

	// Query historical average for this specific route + time slot
	err := m.DB.Table("traffic_history").
		Where("origin_area = ? AND destination_area = ? AND day_of_week = ? AND hour_of_day = ?",
			originArea, destArea, dayOfWeek, hourOfDay).
		Select("COALESCE(AVG(congestion_factor), 1.0)").
		Scan(&avgFactor).Error

	if err != nil {
		log.Printf("⚠️  [MEMORY] Error querying traffic history: %v", err)
		return CongestionResult{
			OriginArea: originArea, DestinationArea: destArea,
			Factor: 1.0, SampleCount: 0, Reliability: "none",
		}
	}

	// Count samples for reliability assessment
	m.DB.Table("traffic_history").
		Where("origin_area = ? AND destination_area = ? AND day_of_week = ? AND hour_of_day = ?",
			originArea, destArea, dayOfWeek, hourOfDay).
		Count(&sampleCount)

	reliability := "low"
	if sampleCount >= 5 {
		reliability = "medium"
	}
	if sampleCount >= 15 {
		reliability = "high"
	}

	log.Printf("🧠 [MEMORY] %s→%s (day=%d, hour=%d): factor=%.2f, samples=%d, reliability=%s",
		originArea, destArea, dayOfWeek, hourOfDay, avgFactor, sampleCount, reliability)

	return CongestionResult{
		OriginArea:      originArea,
		DestinationArea: destArea,
		Factor:          avgFactor,
		SampleCount:     int(sampleCount),
		Reliability:     reliability,
	}
}

// GetCongestionForHourRange returns the worst-case congestion factor
// across a range of hours. Useful when a delivery might span multiple hours.
func (m *TrafficMemory) GetCongestionForHourRange(originArea, destArea string, dayOfWeek, startHour, endHour int) float64 {
	var maxFactor float64 = 1.0

	for h := startHour; h <= endHour; h++ {
		result := m.GetCongestionFactor(originArea, destArea, dayOfWeek, h)
		if result.Factor > maxFactor {
			maxFactor = result.Factor
		}
	}

	return maxFactor
}

// AdjustDurationMatrix applies congestion factors to an OSRM duration matrix.
// This is the key function that makes the Agent "smart" — it adjusts
// theoretical travel times based on historical real-world data.
//
// Parameters:
//   - durationMatrix: raw OSRM estimates (seconds)
//   - nodeAreas: area name for each node (index 0 = kitchen, 1..n = schools)
//   - dayOfWeek: 0=Sunday, 1=Monday, ..., 6=Saturday
//   - hourOfDay: expected hour of travel (0-23)
//
// Returns:
//   - adjusted duration matrix with congestion applied
func (m *TrafficMemory) AdjustDurationMatrix(
	durationMatrix [][]int,
	nodeAreas []string,
	dayOfWeek, hourOfDay int,
) [][]int {
	n := len(durationMatrix)
	adjusted := make([][]int, n)

	for i := 0; i < n; i++ {
		adjusted[i] = make([]int, n)
		for j := 0; j < n; j++ {
			if i == j {
				adjusted[i][j] = 0
				continue
			}

			baseDuration := durationMatrix[i][j]

			// Get congestion factor for this origin-destination pair
			factor := m.GetCongestionFactor(nodeAreas[i], nodeAreas[j], dayOfWeek, hourOfDay)

			// Apply factor with a cap to prevent unrealistic values
			adjustedDuration := float64(baseDuration) * factor.Factor
			adjustedDuration = math.Min(adjustedDuration, float64(baseDuration)*3.0) // cap at 3x

			adjusted[i][j] = int(adjustedDuration)
		}
	}

	log.Printf("🧠 [MEMORY] Duration matrix adjusted for day=%d, hour=%d (%dx%d)",
		dayOfWeek, hourOfDay, n, n)

	return adjusted
}

// RecordTrip stores actual trip data for future learning.
// Called after each delivery is completed to update the Agent's memory.
func (m *TrafficMemory) RecordTrip(record TrafficRecord) error {
	// Calculate congestion factor
	if record.OSRMEstimatedSeconds <= 0 {
		record.OSRMEstimatedSeconds = 1 // prevent division by zero
	}
	record.CongestionFactor = float64(record.ActualDurationSeconds) / float64(record.OSRMEstimatedSeconds)

	// Cap extreme values
	if record.CongestionFactor > 5.0 {
		record.CongestionFactor = 5.0
	}
	if record.CongestionFactor < 0.5 {
		record.CongestionFactor = 0.5
	}

	result := m.DB.Table("traffic_history").Create(&record)
	if result.Error != nil {
		log.Printf("⚠️  [MEMORY] Failed to record trip: %v", result.Error)
		return result.Error
	}

	log.Printf("🧠 [MEMORY] Recorded trip %s→%s: actual=%ds, estimated=%ds, factor=%.2f",
		record.OriginArea, record.DestinationArea,
		record.ActualDurationSeconds, record.OSRMEstimatedSeconds, record.CongestionFactor)

	return nil
}

// TrafficRecord is the data structure for recording a completed trip segment.
type TrafficRecord struct {
	OriginArea            string  `json:"origin_area" gorm:"column:origin_area"`
	DestinationArea       string  `json:"destination_area" gorm:"column:destination_area"`
	OriginLat             float64 `json:"origin_lat" gorm:"column:origin_lat"`
	OriginLng             float64 `json:"origin_lng" gorm:"column:origin_lng"`
	DestLat               float64 `json:"dest_lat" gorm:"column:dest_lat"`
	DestLng               float64 `json:"dest_lng" gorm:"column:dest_lng"`
	DayOfWeek             int     `json:"day_of_week" gorm:"column:day_of_week"`
	HourOfDay             int     `json:"hour_of_day" gorm:"column:hour_of_day"`
	ActualDurationSeconds int     `json:"actual_duration_seconds" gorm:"column:actual_duration_seconds"`
	OSRMEstimatedSeconds  int     `json:"osrm_estimated_seconds" gorm:"column:osrm_estimated_seconds"`
	CongestionFactor      float64 `json:"congestion_factor" gorm:"column:congestion_factor"`
	RecordedDate          string  `json:"recorded_date" gorm:"column:recorded_date"`
}

// GetAreaStats returns summary congestion statistics for all known areas.
// Useful for dashboard/monitoring to visualize traffic patterns.
func (m *TrafficMemory) GetAreaStats() []AreaCongestionSummary {
	var stats []AreaCongestionSummary

	m.DB.Table("traffic_history").
		Select(`
			origin_area,
			destination_area,
			AVG(congestion_factor) as avg_factor,
			MAX(congestion_factor) as max_factor,
			MIN(congestion_factor) as min_factor,
			COUNT(*) as sample_count
		`).
		Group("origin_area, destination_area").
		Order("avg_factor DESC").
		Scan(&stats)

	return stats
}

// AreaCongestionSummary holds aggregate congestion data for a route segment.
type AreaCongestionSummary struct {
	OriginArea      string  `json:"origin_area"`
	DestinationArea string  `json:"destination_area"`
	AvgFactor       float64 `json:"avg_factor"`
	MaxFactor       float64 `json:"max_factor"`
	MinFactor       float64 `json:"min_factor"`
	SampleCount     int     `json:"sample_count"`
}
