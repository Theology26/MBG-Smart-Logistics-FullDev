package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"go-api/internal/config"
	"go-api/internal/models"
	"go-api/internal/services/gemini"
	"go-api/internal/services/osrm"
	"go-api/internal/services/routing"

	"gorm.io/gorm"
)

// ============================================================================
// AI Agent — Dynamic Scheduler ("The Brain") — PILAR 3
// ============================================================================
// The Agent orchestrates the entire delivery planning workflow:
//
//   1. Receive production trigger (food is ready)
//   2. Call Gemini AI for shelf-life analysis
//   3. Calculate hard deadline from cooked_at + shelf_life
//   4. Query Traffic Memory for congestion predictions
//   5. Request OSRM for fresh distance/duration matrix
//   6. Apply congestion factors to duration matrix
//   7. Run CVRPTW solver with time window constraints
//   8. Generate route plans and persist to database
//   9. Return optimized routes with departure schedules
//
// The Agent gets "smarter" over time as Traffic Memory accumulates
// real-world delivery data from completed trips.
// ============================================================================

// Scheduler is the main AI Agent that orchestrates delivery planning.
type Scheduler struct {
	DB     *gorm.DB
	Config *config.Config
	Gemini *gemini.Client
	OSRM   *osrm.Client
	Memory *TrafficMemory
}

// NewScheduler creates a new Scheduler with all dependencies.
func NewScheduler(db *gorm.DB, cfg *config.Config) *Scheduler {
	return &Scheduler{
		DB:     db,
		Config: cfg,
		Gemini: gemini.NewClient(cfg.GeminiAPIKey, cfg.GeminiModel),
		OSRM:   osrm.NewClient(cfg.OSRMBaseURL),
		Memory: NewTrafficMemory(db),
	}
}

// PlanDeliveryRequest contains all info needed to plan a delivery.
type PlanDeliveryRequest struct {
	ProductionLogID string
	DishName        string
	TotalPortions   int
	KitchenID       string
	CookedAt        time.Time
	Assignments     []AssignmentInfo
}

// AssignmentInfo links a school to its portion allocation.
type AssignmentInfo struct {
	AssignmentID      string
	SchoolID          string
	SchoolName        string
	Area              string
	Lat               float64
	Lng               float64
	AllocatedPortions int
	WindowStart       time.Time
	WindowEnd         time.Time
}

// PlanDeliveryResult contains the Agent's planning output.
type PlanDeliveryResult struct {
	ProductionLogID         string                       `json:"production_log_id"`
	DishName                string                       `json:"dish_name"`
	ShelfLife               *gemini.ShelfLifeResult      `json:"shelf_life"`
	Deadline                time.Time                    `json:"deadline"`
	Routes                  []routing.VehicleRoute       `json:"routes"`
	Feasible                bool                         `json:"feasible"`
	UnservedSchools         []string                     `json:"unserved_schools,omitempty"`
	Score                   float64                      `json:"score"`
	PlanningDurationMs      int64                        `json:"planning_duration_ms"`
	CongestionFactorsUsed   map[string]float64           `json:"congestion_factors_used"`
}

// PlanDelivery is the main orchestration method — the Agent's agentic workflow.
func (s *Scheduler) PlanDelivery(ctx context.Context, req PlanDeliveryRequest) (*PlanDeliveryResult, error) {
	startTime := time.Now()
	log.Println("╔═══════════════════════════════════════════════════════╗")
	log.Println("║ 🧠 AI AGENT — Starting Delivery Planning            ║")
	log.Println("╚═══════════════════════════════════════════════════════╝")
	log.Printf("🧠 [AGENT] Dish: '%s', Portions: %d, Schools: %d",
		req.DishName, req.TotalPortions, len(req.Assignments))

	// ── STEP 1: Analyze Shelf-Life via Gemini AI ─────────────────────
	log.Println("🧠 [AGENT] Step 1/7: Analyzing shelf-life via Gemini AI...")
	shelfLife, err := s.Gemini.AnalyzeShelfLife(req.DishName)
	if err != nil {
		return nil, fmt.Errorf("step 1 failed — shelf-life analysis: %w", err)
	}

	// ── STEP 2: Calculate Hard Deadline ──────────────────────────────
	log.Println("🧠 [AGENT] Step 2/7: Calculating delivery deadline...")
	deadline := req.CookedAt.Add(time.Duration(shelfLife.MaxDeliveryWindowMinutes) * time.Minute)
	remainingMinutes := time.Until(deadline).Minutes()

	log.Printf("🧠 [AGENT] Cooked at: %s, Deadline: %s (%.0f minutes remaining)",
		req.CookedAt.Format("15:04"), deadline.Format("15:04"), remainingMinutes)

	if remainingMinutes <= 0 {
		return nil, fmt.Errorf("❌ DEADLINE SUDAH LEWAT! Masakan '%s' sudah melewati batas waktu aman", req.DishName)
	}

	if shelfLife.RiskLevel == "critical" {
		log.Printf("⚠️  [AGENT] CRITICAL RISK — '%s' sangat rentan basi. Harus prioritas kirim SEGERA!", req.DishName)
	}

	// ── STEP 3: Build Node List ──────────────────────────────────────
	log.Println("🧠 [AGENT] Step 3/7: Building delivery node list...")

	// Get kitchen coordinates
	var kitchen models.Kitchen
	if err := s.DB.First(&kitchen, "id = ?", req.KitchenID).Error; err != nil {
		return nil, fmt.Errorf("step 3 failed — kitchen not found: %w", err)
	}

	nodes := make([]routing.DeliveryNode, 0, len(req.Assignments)+1)

	// Node 0 = Depot (Kitchen)
	nodes = append(nodes, routing.DeliveryNode{
		Index:           0,
		SchoolID:        "",
		SchoolName:      kitchen.Name,
		Area:            "Dapur",
		Demand:          0,
		TimeWindowStart: req.CookedAt,
		TimeWindowEnd:   deadline,
		ServiceTime:     0,
		Lat:             kitchen.Latitude,
		Lng:             kitchen.Longitude,
	})

	// Nodes 1..n = Schools
	for i, a := range req.Assignments {
		windowEnd := a.WindowEnd
		// Use the earlier of school window end and food deadline
		if deadline.Before(windowEnd) {
			windowEnd = deadline
		}

		nodes = append(nodes, routing.DeliveryNode{
			Index:           i + 1,
			SchoolID:        a.SchoolID,
			SchoolName:      a.SchoolName,
			Area:            a.Area,
			Demand:          a.AllocatedPortions,
			TimeWindowStart: a.WindowStart,
			TimeWindowEnd:   windowEnd,
			ServiceTime:     s.Config.DefaultServiceTimeSeconds,
			Lat:             a.Lat,
			Lng:             a.Lng,
		})
	}

	// ── STEP 4: Get OSRM Distance Matrix ─────────────────────────────
	log.Println("🧠 [AGENT] Step 4/7: Requesting distance matrix from OSRM...")

	coords := make([]osrm.Coordinate, len(nodes))
	for i, node := range nodes {
		coords[i] = osrm.Coordinate{Lat: node.Lat, Lng: node.Lng}
	}

	matrix, err := s.OSRM.GetMatrix(coords)
	if err != nil {
		return nil, fmt.Errorf("step 4 failed — OSRM matrix request: %w", err)
	}

	// Convert float matrices to int
	n := len(nodes)
	durationMatrix := make([][]int, n)
	distanceMatrix := make([][]int, n)
	for i := 0; i < n; i++ {
		durationMatrix[i] = make([]int, n)
		distanceMatrix[i] = make([]int, n)
		for j := 0; j < n; j++ {
			durationMatrix[i][j] = int(matrix.Durations[i][j])
			distanceMatrix[i][j] = int(matrix.Distances[i][j])
		}
	}

	// ── STEP 5: Apply Traffic Memory (Congestion Factors) ────────────
	log.Println("🧠 [AGENT] Step 5/7: Consulting traffic memory for congestion patterns...")

	now := time.Now()
	dayOfWeek := int(now.Weekday())
	hourOfDay := now.Hour()

	// Build area list for matrix adjustment
	nodeAreas := make([]string, len(nodes))
	for i, node := range nodes {
		if node.Area != "" {
			nodeAreas[i] = node.Area
		} else {
			nodeAreas[i] = "Unknown"
		}
	}

	// Adjust durations based on historical congestion
	adjustedDurations := s.Memory.AdjustDurationMatrix(
		durationMatrix, nodeAreas, dayOfWeek, hourOfDay,
	)

	// Collect congestion factors used (for reporting)
	congestionFactors := make(map[string]float64)
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			if i != j && durationMatrix[i][j] > 0 {
				key := fmt.Sprintf("%s→%s", nodeAreas[i], nodeAreas[j])
				factor := float64(adjustedDurations[i][j]) / float64(durationMatrix[i][j])
				congestionFactors[key] = factor
			}
		}
	}

	// ── STEP 6: Get Available Couriers & Run CVRPTW Solver ───────────
	log.Println("🧠 [AGENT] Step 6/7: Finding available couriers and running CVRPTW solver...")

	var couriers []models.Courier
	s.DB.Where("is_available = ?", true).Find(&couriers)

	if len(couriers) == 0 {
		return nil, fmt.Errorf("step 6 failed — no available couriers")
	}

	vehicles := make([]routing.VehicleInfo, len(couriers))
	for i, c := range couriers {
		vehicles[i] = routing.VehicleInfo{
			ID:        c.ID,
			CourierID: c.UserID,
			Capacity:  c.MaxCapacityPortions,
		}
	}

	// Determine departure time (now or ASAP for critical items)
	departureTime := time.Now()
	if shelfLife.RiskLevel == "critical" {
		log.Println("🧠 [AGENT] 🚨 CRITICAL: Immediate departure required!")
	}

	solverParams := routing.SolverParams{
		DurationMatrix: adjustedDurations,
		DistanceMatrix: distanceMatrix,
		Nodes:          nodes,
		Vehicles:       vehicles,
		DepotIndex:     0,
		DepartureTime:  departureTime,
		Deadline:       deadline,
		MaxIterations:  s.Config.MaxImprovementIterations,
	}

	solverResult, err := routing.Solve(solverParams)
	if err != nil {
		return nil, fmt.Errorf("step 6 failed — CVRPTW solver: %w", err)
	}

	// ── STEP 7: Persist Route Plans to Database ──────────────────────
	log.Println("🧠 [AGENT] Step 7/7: Persisting optimized routes to database...")

	err = s.persistRoutes(req, solverResult, shelfLife, deadline)
	if err != nil {
		log.Printf("⚠️  [AGENT] Warning: failed to persist routes: %v", err)
		// Don't fail — still return results
	}

	// ── DONE ─────────────────────────────────────────────────────────
	planningDuration := time.Since(startTime)

	log.Println("╔═══════════════════════════════════════════════════════╗")
	log.Printf("║ 🧠 AI AGENT — Planning Complete in %dms              ║", planningDuration.Milliseconds())
	log.Printf("║ 📊 Routes: %d | Feasible: %v | Score: %.1f          ║",
		len(solverResult.Routes), solverResult.Feasible, solverResult.Score)
	log.Println("╚═══════════════════════════════════════════════════════╝")

	// Map unserved node indices to school names
	var unservedSchools []string
	for _, idx := range solverResult.UnservedNodes {
		unservedSchools = append(unservedSchools, nodes[idx].SchoolName)
	}

	return &PlanDeliveryResult{
		ProductionLogID:       req.ProductionLogID,
		DishName:              req.DishName,
		ShelfLife:             shelfLife,
		Deadline:              deadline,
		Routes:                solverResult.Routes,
		Feasible:              solverResult.Feasible,
		UnservedSchools:       unservedSchools,
		Score:                 solverResult.Score,
		PlanningDurationMs:    planningDuration.Milliseconds(),
		CongestionFactorsUsed: congestionFactors,
	}, nil
}

// persistRoutes saves the CVRPTW solution to the database.
func (s *Scheduler) persistRoutes(req PlanDeliveryRequest, result *routing.SolverResult, shelfLife *gemini.ShelfLifeResult, deadline time.Time) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		// Update production log with shelf-life data
		geminiJSON, _ := json.Marshal(shelfLife)
		tx.Model(&models.ProductionLog{}).Where("id = ?", req.ProductionLogID).Updates(map[string]interface{}{
			"shelf_life_minutes":          shelfLife.ShelfLifeMinutes,
			"max_delivery_window_minutes": shelfLife.MaxDeliveryWindowMinutes,
			"dish_category":               shelfLife.Category,
			"risk_level":                  shelfLife.RiskLevel,
			"gemini_analysis_json":        string(geminiJSON),
			"deadline_at":                 deadline,
			"status":                      "dispatched",
		})

		// Create route plans for each vehicle route
		for _, route := range result.Routes {
			totalDist := route.TotalDist
			totalDuration := route.TotalTime
			score := result.Score
			solverMeta, _ := json.Marshal(map[string]interface{}{
				"iterations": result.Iterations,
				"feasible":   result.Feasible,
				"algorithm":  "nearest-neighbor + 2-opt + relocate",
			})
			solverMetaStr := string(solverMeta)

			routePlan := models.RoutePlan{
				ProductionLogID:         req.ProductionLogID,
				CourierID:               route.VehicleID,
				KitchenID:               req.KitchenID,
				PlannedDeparture:        route.DepartureAt,
				TotalPortions:           route.TotalLoad,
				TotalStops:              len(route.Stops),
				TotalDistanceMeters:     &totalDist,
				TotalEstimatedDurationS: &totalDuration,
				OptimizationScore:       &score,
				SolverMetadata:          &solverMetaStr,
				Status:                  "planned",
			}

			if err := tx.Create(&routePlan).Error; err != nil {
				return fmt.Errorf("failed to create route plan: %w", err)
			}

			// Create route stops
			for _, stop := range route.Stops {
				// Find assignment ID for this school
				assignmentID := ""
				for _, a := range req.Assignments {
					if a.SchoolID == stop.SchoolID {
						assignmentID = a.AssignmentID
						break
					}
				}

				if assignmentID == "" {
					continue
				}

				arrTime := stop.ArrivalTime
				deptTime := stop.DepartTime
				dist := stop.CumDist
				dur := stop.CumTime

				routeStop := models.RouteStop{
					RoutePlanID:        routePlan.ID,
					SchoolAssignmentID: assignmentID,
					StopSequence:       stop.Sequence,
					EstimatedArrival:   &arrTime,
					EstimatedDeparture: &deptTime,
					DynamicETA:         &arrTime,
					DistanceFromPrevM:  &dist,
					DurationFromPrevS:  &dur,
					PortionsToDeliver:  stop.Portions,
					Status:             "pending",
				}

				if err := tx.Create(&routeStop).Error; err != nil {
					return fmt.Errorf("failed to create route stop: %w", err)
				}

				// Update school assignment status
				tx.Model(&models.SchoolAssignment{}).
					Where("id = ?", assignmentID).
					Update("delivery_status", "in_transit")
			}
		}

		return nil
	})
}

// ============================================================================
// Dynamic ETA Recalculation (PILAR 5)
// ============================================================================

// RecalculateETA updates the ETA for all remaining stops after a drop-off.
// Called every time a courier completes a stop.
func (s *Scheduler) RecalculateETA(routePlanID string, completedStopID string, currentLat, currentLng float64) error {
	log.Printf("🧠 [AGENT] Recalculating ETA for route %s after stop %s", routePlanID, completedStopID)

	// Get all remaining stops
	var remainingStops []models.RouteStop
	s.DB.Where("route_plan_id = ? AND status = 'pending'", routePlanID).
		Order("stop_sequence ASC").
		Preload("SchoolAssignment.School").
		Find(&remainingStops)

	if len(remainingStops) == 0 {
		log.Println("🧠 [AGENT] No remaining stops — route complete!")
		s.DB.Model(&models.RoutePlan{}).Where("id = ?", routePlanID).Updates(map[string]interface{}{
			"status":       "completed",
			"completed_at": time.Now(),
		})
		return nil
	}

	// Get fresh ETA from OSRM for each remaining stop
	currentCoord := osrm.Coordinate{Lat: currentLat, Lng: currentLng}
	currentTime := time.Now()

	for i := range remainingStops {
		var school models.School
		s.DB.First(&school, "id = ?", remainingStops[i].SchoolAssignment.SchoolID)

		targetCoord := osrm.Coordinate{Lat: school.Latitude, Lng: school.Longitude}

		duration, _, err := s.OSRM.GetETABetweenPoints(currentCoord, targetCoord)
		if err != nil {
			log.Printf("⚠️  [AGENT] OSRM ETA failed for stop %d: %v", i, err)
			continue
		}

		// Apply congestion factor
		now := time.Now()
		congestion := s.Memory.GetCongestionFactor("current", school.Area, int(now.Weekday()), now.Hour())
		adjustedDuration := float64(duration) * congestion.Factor

		newETA := currentTime.Add(time.Duration(int(adjustedDuration)) * time.Second)

		// Update dynamic ETA in database
		s.DB.Model(&models.RouteStop{}).Where("id = ?", remainingStops[i].ID).
			Update("dynamic_eta", newETA)

		log.Printf("🧠 [AGENT] Stop %d (%s): New ETA = %s (congestion: %.2f)",
			remainingStops[i].StopSequence, school.Name,
			newETA.Format("15:04:05"), congestion.Factor)

		// For chain calculation: use this stop as the new "current" for next stop
		currentCoord = targetCoord
		serviceTime := time.Duration(s.Config.DefaultServiceTimeSeconds) * time.Second
		currentTime = newETA.Add(serviceTime)
	}

	return nil
}
