package handlers

import (
	"io"
	"log"
	"net/http"
	"time"

	"go-api/internal/config"
	"go-api/internal/models"
	"go-api/internal/services/agent"
	"go-api/internal/services/gemini"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ============================================================================
// HTTP Handlers — All API Domains
// ============================================================================

// Handler holds shared dependencies for all HTTP handlers.
type Handler struct {
	DB           *gorm.DB
	Config       *config.Config
	Gemini       *gemini.Client
	Scheduler    *agent.Scheduler
	AgentService *agent.AgentService // Skenario 1 & 2: Schedule Shifter + Dynamic ETA
}

// NewHandler creates a new Handler with all dependencies injected.
func NewHandler(db *gorm.DB, cfg *config.Config) *Handler {
	return &Handler{
		DB:           db,
		Config:       cfg,
		Gemini:       gemini.NewClient(cfg.GeminiAPIKey, cfg.GeminiModel),
		Scheduler:    agent.NewScheduler(db, cfg),
		AgentService: agent.NewAgentService(db, cfg),
	}
}

// JSON is a shorthand for standardized API response.
func JSON(c *gin.Context, status int, message string, data interface{}) {
	c.JSON(status, gin.H{
		"status":  status,
		"message": message,
		"data":    data,
	})
}

// ============================================================================
// PILAR 1: OCR & Inventory Management
// ============================================================================

// ScanReceipt handles receipt image upload and OCR via Gemini Vision.
// POST /api/ocr/scan-receipt
func (h *Handler) ScanReceipt(c *gin.Context) {
	file, header, err := c.Request.FormFile("receipt_image")
	if err != nil {
		JSON(c, http.StatusBadRequest, "Receipt image is required (field: receipt_image)", nil)
		return
	}
	defer file.Close()

	// Read image data
	imageData, err := io.ReadAll(file)
	if err != nil {
		JSON(c, http.StatusInternalServerError, "Failed to read image", nil)
		return
	}

	// Determine MIME type
	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "image/jpeg"
	}

	log.Printf("📷 [OCR] Scanning receipt: %s (%d bytes, %s)", header.Filename, len(imageData), mimeType)

	// Call Gemini Vision OCR
	result, err := h.Gemini.ScanReceipt(imageData, mimeType)
	if err != nil {
		JSON(c, http.StatusInternalServerError, "OCR scan failed: "+err.Error(), nil)
		return
	}

	JSON(c, http.StatusOK, "Receipt scanned successfully", result)
}

// ConfirmOCR confirms OCR results and updates inventory stock.
// POST /api/ocr/confirm
func (h *Handler) ConfirmOCR(c *gin.Context) {
	var req models.OCRConfirmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSON(c, http.StatusBadRequest, "Invalid request: "+err.Error(), nil)
		return
	}

	tx := h.DB.Begin()

	for _, item := range req.Items {
		// Find or create inventory record
		var inv models.Inventory
		result := tx.Where("kitchen_id = ? AND ingredient_name = ?", req.KitchenID, item.IngredientName).First(&inv)

		if result.Error == gorm.ErrRecordNotFound {
			// Create new inventory
			inv = models.Inventory{
				KitchenID:      req.KitchenID,
				IngredientName: item.IngredientName,
				Unit:           item.Unit,
				CurrentStock:   item.Quantity,
				UnitPrice:      item.UnitPrice,
			}
			if err := tx.Create(&inv).Error; err != nil {
				tx.Rollback()
				JSON(c, http.StatusInternalServerError, "Failed to create inventory: "+err.Error(), nil)
				return
			}
		} else {
			// Update existing stock
			tx.Model(&inv).Updates(map[string]interface{}{
				"current_stock": gorm.Expr("current_stock + ?", item.Quantity),
				"unit_price":    item.UnitPrice,
			})
		}

		// Log the transaction
		totalPrice := item.TotalPrice
		unitPrice := item.UnitPrice
		rawJSON := req.RawJSON
		imageURL := req.ImageURL

		txRecord := models.InventoryTransaction{
			InventoryID:     inv.ID,
			TransactionType: "purchase",
			Quantity:        item.Quantity,
			UnitPrice:       &unitPrice,
			TotalPrice:      &totalPrice,
			ReceiptImageURL: &imageURL,
			OCRRawJSON:      &rawJSON,
		}
		if err := tx.Create(&txRecord).Error; err != nil {
			tx.Rollback()
			JSON(c, http.StatusInternalServerError, "Failed to log transaction: "+err.Error(), nil)
			return
		}
	}

	tx.Commit()
	JSON(c, http.StatusOK, "OCR confirmed and inventory updated", gin.H{
		"items_processed": len(req.Items),
	})
}

// GetInventory lists inventory for a kitchen.
// GET /api/inventory/:kitchen_id
func (h *Handler) GetInventory(c *gin.Context) {
	kitchenID := c.Param("kitchen_id")

	var items []models.Inventory
	query := h.DB
	if kitchenID != "" {
		query = query.Where("kitchen_id = ?", kitchenID)
	}
	query.Order("ingredient_name ASC").Find(&items)

	JSON(c, http.StatusOK, "Inventory retrieved", items)
}

// GetAllInventory lists all inventory across all kitchens.
// GET /api/inventory
func (h *Handler) GetAllInventory(c *gin.Context) {
	var items []models.Inventory
	h.DB.Preload("Kitchen").Order("ingredient_name ASC").Find(&items)
	JSON(c, http.StatusOK, "All inventory retrieved", items)
}

// ============================================================================
// PILAR 2: Production Management & Shelf-Life AI
// ============================================================================

// StartProduction triggers a new production batch with shelf-life analysis.
// This is the entry point that kicks off the entire Agent workflow.
// POST /api/production/start
func (h *Handler) StartProduction(c *gin.Context) {
	var req models.ProductionStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSON(c, http.StatusBadRequest, "Invalid request: "+err.Error(), nil)
		return
	}

	// Validate total portions match assignments
	totalAllocated := 0
	for _, a := range req.Assignments {
		totalAllocated += a.AllocatedPortions
	}
	if totalAllocated > req.TotalPortions {
		JSON(c, http.StatusBadRequest, "Total allocated portions exceed total portions", gin.H{
			"total_portions":   req.TotalPortions,
			"total_allocated":  totalAllocated,
		})
		return
	}

	// Step 1: Analyze shelf-life via Gemini
	shelfLife, err := h.Gemini.AnalyzeShelfLife(req.DishName)
	if err != nil {
		JSON(c, http.StatusInternalServerError, "Shelf-life analysis failed: "+err.Error(), nil)
		return
	}

	// Step 2: Create production log
	deadline := req.CookedAt.Add(time.Duration(shelfLife.MaxDeliveryWindowMinutes) * time.Minute)

	productionLog := models.ProductionLog{
		KitchenID:                req.KitchenID,
		DishName:                 req.DishName,
		DishCategory:             shelfLife.Category,
		TotalPortions:            req.TotalPortions,
		ShelfLifeMinutes:         shelfLife.ShelfLifeMinutes,
		MaxDeliveryWindowMinutes: shelfLife.MaxDeliveryWindowMinutes,
		RiskLevel:                shelfLife.RiskLevel,
		CookedAt:                 req.CookedAt,
		DeadlineAt:               deadline,
		Status:                   "ready",
	}

	if err := h.DB.Create(&productionLog).Error; err != nil {
		JSON(c, http.StatusInternalServerError, "Failed to create production log: "+err.Error(), nil)
		return
	}

	// Step 3: Create school assignments
	var assignments []models.SchoolAssignment
	for _, a := range req.Assignments {
		sa := models.SchoolAssignment{
			ProductionLogID:   productionLog.ID,
			SchoolID:          a.SchoolID,
			AllocatedPortions: a.AllocatedPortions,
			DeliveryStatus:    "pending",
		}
		if err := h.DB.Create(&sa).Error; err != nil {
			JSON(c, http.StatusInternalServerError, "Failed to create assignment: "+err.Error(), nil)
			return
		}
		assignments = append(assignments, sa)
	}

	JSON(c, http.StatusCreated, "Production started — shelf-life analyzed", gin.H{
		"production_log": productionLog,
		"shelf_life":     shelfLife,
		"deadline":       deadline.Format("2006-01-02 15:04:05"),
		"assignments":    assignments,
		"next_step":      "Call POST /api/routing/plan with production_log_id to optimize delivery routes",
	})
}

// GetProduction gets a production log by ID.
// GET /api/production/:id
func (h *Handler) GetProduction(c *gin.Context) {
	id := c.Param("id")

	var production models.ProductionLog
	if err := h.DB.Preload("SchoolAssignments.School").
		Preload("RoutePlans.Stops").
		First(&production, "id = ?", id).Error; err != nil {
		JSON(c, http.StatusNotFound, "Production not found", nil)
		return
	}

	JSON(c, http.StatusOK, "Production retrieved", production)
}

// GetActiveProductions lists all active (non-completed) productions.
// GET /api/production/active
func (h *Handler) GetActiveProductions(c *gin.Context) {
	var productions []models.ProductionLog
	h.DB.Where("status IN ?", []string{"cooking", "ready", "dispatched"}).
		Preload("SchoolAssignments").
		Order("cooked_at DESC").
		Find(&productions)

	JSON(c, http.StatusOK, "Active productions retrieved", productions)
}

// ============================================================================
// PILAR 3 & 4: Route Optimization (Agent + CVRPTW)
// ============================================================================

// PlanRoutes triggers the AI Agent to plan optimal delivery routes.
// POST /api/routing/plan
func (h *Handler) PlanRoutes(c *gin.Context) {
	var req struct {
		ProductionLogID string `json:"production_log_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		JSON(c, http.StatusBadRequest, "Invalid request: "+err.Error(), nil)
		return
	}

	// Load production log with assignments
	var production models.ProductionLog
	if err := h.DB.Preload("SchoolAssignments.School").
		First(&production, "id = ?", req.ProductionLogID).Error; err != nil {
		JSON(c, http.StatusNotFound, "Production log not found", nil)
		return
	}

	// Build assignment info for the Agent
	var assignmentInfos []agent.AssignmentInfo
	for _, sa := range production.SchoolAssignments {
		school := sa.School
		if school == nil {
			continue
		}

		// Parse school time windows
		windowStart := production.CookedAt
		windowEnd := production.DeadlineAt

		assignmentInfos = append(assignmentInfos, agent.AssignmentInfo{
			AssignmentID:      sa.ID,
			SchoolID:          sa.SchoolID,
			SchoolName:        school.Name,
			Area:              school.Area,
			Lat:               school.Latitude,
			Lng:               school.Longitude,
			AllocatedPortions: sa.AllocatedPortions,
			WindowStart:       windowStart,
			WindowEnd:         windowEnd,
		})
	}

	// Trigger the AI Agent
	planReq := agent.PlanDeliveryRequest{
		ProductionLogID: production.ID,
		DishName:        production.DishName,
		TotalPortions:   production.TotalPortions,
		KitchenID:       production.KitchenID,
		CookedAt:        production.CookedAt,
		Assignments:     assignmentInfos,
	}

	result, err := h.Scheduler.PlanDelivery(c.Request.Context(), planReq)
	if err != nil {
		JSON(c, http.StatusInternalServerError, "Route planning failed: "+err.Error(), nil)
		return
	}

	JSON(c, http.StatusOK, "Routes optimized successfully by AI Agent", result)
}

// GetRoutePlans retrieves the optimized routes for a production.
// GET /api/routing/plans/:production_id
func (h *Handler) GetRoutePlans(c *gin.Context) {
	productionID := c.Param("production_id")

	var plans []models.RoutePlan
	h.DB.Where("production_log_id = ?", productionID).
		Preload("Courier.User").
		Preload("Stops.SchoolAssignment.School").
		Order("created_at ASC").
		Find(&plans)

	JSON(c, http.StatusOK, "Route plans retrieved", plans)
}

// CompleteStop marks a stop as delivered and triggers Dynamic ETA recalculation.
// This is the trigger for SKENARIO 2 — every tap on 'Selesai Drop-off' runs the
// AgentService to recalculate all remaining ETAs based on real-time GPS.
// PUT /api/routing/stops/:id/complete
func (h *Handler) CompleteStop(c *gin.Context) {
	stopID := c.Param("id")

	var req models.StopCompleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSON(c, http.StatusBadRequest, "Invalid request: "+err.Error(), nil)
		return
	}

	// Get the stop with its route plan
	var stop models.RouteStop
	if err := h.DB.Preload("RoutePlan").First(&stop, "id = ?", stopID).Error; err != nil {
		JSON(c, http.StatusNotFound, "Route stop not found", nil)
		return
	}

	// Update school assignment delivery status
	now := time.Now()
	h.DB.Model(&models.SchoolAssignment{}).
		Where("id = ?", stop.SchoolAssignmentID).
		Updates(map[string]interface{}{
			"delivery_status":    "delivered",
			"delivered_portions": req.DeliveredPortions,
			"delivered_at":       now,
		})

	// Get courier's current GPS for dynamic ETA recalculation
	var courier models.Courier
	h.DB.First(&courier, "id = ?", stop.RoutePlan.CourierID)

	if courier.CurrentLatitude == nil || courier.CurrentLongitude == nil {
		// Fallback: no GPS data, just mark as delivered
		JSON(c, http.StatusOK, "Stop completed (no GPS available for ETA recalc)", gin.H{
			"stop_id":            stopID,
			"delivered_at":       now,
			"delivered_portions": req.DeliveredPortions,
		})
		return
	}

	// ── TRIGGER SKENARIO 2: Dynamic ETA Recalculation ──
	etaReq := agent.ETARecalcRequest{
		RoutePlanID:     stop.RoutePlanID,
		CompletedStopID: stopID,
		CourierLat:      *courier.CurrentLatitude,
		CourierLng:      *courier.CurrentLongitude,
		GPSAccuracy:     10.0, // default if not provided
	}

	etaResult, err := h.AgentService.RecalculateDynamicETA(c.Request.Context(), etaReq)
	if err != nil {
		log.Printf("⚠️  ETA recalculation failed: %v", err)
		JSON(c, http.StatusOK, "Stop completed but ETA recalculation failed", gin.H{
			"stop_id":            stopID,
			"delivered_at":       now,
			"delivered_portions": req.DeliveredPortions,
			"eta_error":          err.Error(),
		})
		return
	}

	JSON(c, http.StatusOK, "Stop completed — ETA recalculated for all remaining stops", gin.H{
		"stop_id":            stopID,
		"delivered_at":       now,
		"delivered_portions": req.DeliveredPortions,
		"eta_recalculation":  etaResult,
	})
}

// ============================================================================
// PILAR 5: Monitoring & Tracking
// ============================================================================

// UpdateLocation receives GPS updates from courier app.
// POST /api/monitoring/location
func (h *Handler) UpdateLocation(c *gin.Context) {
	var req models.LocationUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSON(c, http.StatusBadRequest, "Invalid request: "+err.Error(), nil)
		return
	}

	// Store tracking point
	tracking := models.DeliveryTracking{
		RoutePlanID: req.RoutePlanID,
		CourierID:   req.CourierID,
		Latitude:    req.Latitude,
		Longitude:   req.Longitude,
		SpeedKmh:    req.SpeedKmh,
		Heading:     req.Heading,
		AccuracyMeters: req.AccuracyMeters,
	}
	h.DB.Create(&tracking)

	// Update courier's current position
	h.DB.Model(&models.Courier{}).Where("id = ?", req.CourierID).Updates(map[string]interface{}{
		"current_latitude":     req.Latitude,
		"current_longitude":    req.Longitude,
		"last_location_update": time.Now(),
	})

	JSON(c, http.StatusOK, "Location updated", nil)
}

// GetTracking returns latest tracking data for a route.
// GET /api/monitoring/track/:route_id
func (h *Handler) GetTracking(c *gin.Context) {
	routeID := c.Param("route_id")

	// Get route plan with stops
	var plan models.RoutePlan
	h.DB.Preload("Courier.User").
		Preload("Stops.SchoolAssignment.School").
		First(&plan, "id = ?", routeID)

	// Get latest tracking points (last 50)
	var tracks []models.DeliveryTracking
	h.DB.Where("route_plan_id = ?", routeID).
		Order("recorded_at DESC").
		Limit(50).
		Find(&tracks)

	JSON(c, http.StatusOK, "Tracking data retrieved", gin.H{
		"route_plan": plan,
		"tracking":   tracks,
	})
}

// GetSchoolETA returns the dynamic ETA for a specific school.
// GET /api/monitoring/eta/:school_id
func (h *Handler) GetSchoolETA(c *gin.Context) {
	schoolID := c.Param("school_id")

	// Find the latest active route stop for this school
	var stop models.RouteStop
	err := h.DB.
		Joins("JOIN school_assignments sa ON sa.id = route_stops.school_assignment_id").
		Joins("JOIN route_plans rp ON rp.id = route_stops.route_plan_id").
		Where("sa.school_id = ? AND rp.status IN ?", schoolID, []string{"planned", "active"}).
		Order("route_stops.created_at DESC").
		Preload("RoutePlan.Courier.User").
		First(&stop).Error

	if err != nil {
		JSON(c, http.StatusNotFound, "No active delivery found for this school", nil)
		return
	}

	// Get school info
	var school models.School
	h.DB.First(&school, "id = ?", schoolID)

	JSON(c, http.StatusOK, "ETA retrieved", gin.H{
		"school":            school,
		"stop_sequence":     stop.StopSequence,
		"estimated_arrival": stop.EstimatedArrival,
		"dynamic_eta":       stop.DynamicETA,
		"status":            stop.Status,
		"courier":           stop.RoutePlan.Courier,
		"portions":          stop.PortionsToDeliver,
	})
}

// ============================================================================
// Master Data: Schools
// ============================================================================

// GetSchools lists all schools.
// GET /api/schools
func (h *Handler) GetSchools(c *gin.Context) {
	var schools []models.School
	h.DB.Where("is_active = ?", true).Order("name ASC").Find(&schools)
	JSON(c, http.StatusOK, "Schools retrieved", schools)
}

// CreateSchool creates a new school.
// POST /api/schools
func (h *Handler) CreateSchool(c *gin.Context) {
	var school models.School
	if err := c.ShouldBindJSON(&school); err != nil {
		JSON(c, http.StatusBadRequest, "Invalid request: "+err.Error(), nil)
		return
	}
	h.DB.Create(&school)
	JSON(c, http.StatusCreated, "School created", school)
}

// UpdateSchool updates a school.
// PUT /api/schools/:id
func (h *Handler) UpdateSchool(c *gin.Context) {
	id := c.Param("id")
	var school models.School
	if err := h.DB.First(&school, "id = ?", id).Error; err != nil {
		JSON(c, http.StatusNotFound, "School not found", nil)
		return
	}
	if err := c.ShouldBindJSON(&school); err != nil {
		JSON(c, http.StatusBadRequest, "Invalid request: "+err.Error(), nil)
		return
	}
	h.DB.Save(&school)
	JSON(c, http.StatusOK, "School updated", school)
}

// DeleteSchool soft-deletes a school.
// DELETE /api/schools/:id
func (h *Handler) DeleteSchool(c *gin.Context) {
	id := c.Param("id")
	h.DB.Model(&models.School{}).Where("id = ?", id).Update("is_active", false)
	JSON(c, http.StatusOK, "School deactivated", nil)
}

// ============================================================================
// Master Data: Kitchens
// ============================================================================

// GetKitchens lists all kitchens.
// GET /api/kitchens
func (h *Handler) GetKitchens(c *gin.Context) {
	var kitchens []models.Kitchen
	h.DB.Where("is_active = ?", true).Find(&kitchens)
	JSON(c, http.StatusOK, "Kitchens retrieved", kitchens)
}

// CreateKitchen creates a new kitchen.
// POST /api/kitchens
func (h *Handler) CreateKitchen(c *gin.Context) {
	var kitchen models.Kitchen
	if err := c.ShouldBindJSON(&kitchen); err != nil {
		JSON(c, http.StatusBadRequest, "Invalid request: "+err.Error(), nil)
		return
	}
	h.DB.Create(&kitchen)
	JSON(c, http.StatusCreated, "Kitchen created", kitchen)
}

// ============================================================================
// Master Data: Couriers
// ============================================================================

// GetCouriers lists all couriers.
// GET /api/couriers
func (h *Handler) GetCouriers(c *gin.Context) {
	var couriers []models.Courier
	h.DB.Preload("User").Find(&couriers)
	JSON(c, http.StatusOK, "Couriers retrieved", couriers)
}

// CreateCourier creates a new courier.
// POST /api/couriers
func (h *Handler) CreateCourier(c *gin.Context) {
	var courier models.Courier
	if err := c.ShouldBindJSON(&courier); err != nil {
		JSON(c, http.StatusBadRequest, "Invalid request: "+err.Error(), nil)
		return
	}
	h.DB.Create(&courier)
	JSON(c, http.StatusCreated, "Courier created", courier)
}

// ============================================================================
// System Health
// ============================================================================

// HealthCheck returns system status.
// GET /api/health
func (h *Handler) HealthCheck(c *gin.Context) {
	// Check DB connection
	sqlDB, err := h.DB.DB()
	dbStatus := "connected"
	if err != nil || sqlDB.Ping() != nil {
		dbStatus = "disconnected"
	}

	// Check OSRM
	osrmStatus := "unknown"
	osrmClient := h.Scheduler.OSRM
	if osrmClient.IsHealthy() {
		osrmStatus = "healthy"
	} else {
		osrmStatus = "unreachable"
	}

	// Check Gemini
	geminiStatus := "configured"
	if h.Config.GeminiAPIKey == "" {
		geminiStatus = "not configured (using fallback)"
	}

	JSON(c, http.StatusOK, "MBG Smart Logistics API is running", gin.H{
		"version":  "2.0.0",
		"database": dbStatus,
		"osrm":     osrmStatus,
		"gemini":   geminiStatus,
		"time":     time.Now().Format("2006-01-02 15:04:05 MST"),
		"timezone": "Asia/Jakarta (WIB)",
	})
}

// GetTrafficStats returns congestion statistics from Agent memory.
// GET /api/traffic/stats
func (h *Handler) GetTrafficStats(c *gin.Context) {
	stats := h.Scheduler.Memory.GetAreaStats()
	JSON(c, http.StatusOK, "Traffic congestion statistics", stats)
}

// ============================================================================
// SKENARIO 1: Analyze Schedule & Shift Departure (Agent Endpoint)
// ============================================================================

// AnalyzeSchedule triggers the AI Agent to analyze a route plan's traffic
// history and automatically shift the departure time if historical data
// shows that routes consistently take longer than OSRM estimates.
// POST /api/agent/analyze-schedule
func (h *Handler) AnalyzeSchedule(c *gin.Context) {
	var req agent.ScheduleShiftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSON(c, http.StatusBadRequest, "Invalid request: "+err.Error(), nil)
		return
	}

	result, err := h.AgentService.AnalyzeAndShiftSchedule(c.Request.Context(), req.RoutePlanID)
	if err != nil {
		JSON(c, http.StatusInternalServerError, "Schedule analysis failed: "+err.Error(), nil)
		return
	}

	JSON(c, http.StatusOK, "Schedule analyzed by AI Agent", result)
}

// ManualETARecalc allows manually triggering ETA recalculation (for testing).
// POST /api/agent/recalculate-eta
func (h *Handler) ManualETARecalc(c *gin.Context) {
	var req agent.ETARecalcRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		JSON(c, http.StatusBadRequest, "Invalid request: "+err.Error(), nil)
		return
	}

	result, err := h.AgentService.RecalculateDynamicETA(c.Request.Context(), req)
	if err != nil {
		JSON(c, http.StatusInternalServerError, "ETA recalculation failed: "+err.Error(), nil)
		return
	}

	JSON(c, http.StatusOK, "ETA recalculated by AI Agent", result)
}
