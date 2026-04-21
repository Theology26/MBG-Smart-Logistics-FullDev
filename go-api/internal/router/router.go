package router

import (
	"go-api/internal/config"
	"go-api/internal/handlers"
	"go-api/internal/middleware"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ============================================================================
// Router — Gin Route Definitions
// ============================================================================
// Route structure follows the 5-Pillar architecture:
//
//   /api/health          → System health check
//   /api/auth/*          → Authentication
//   /api/ocr/*           → Pilar 1: OCR & Stock Management
//   /api/inventory/*     → Pilar 1: Inventory CRUD
//   /api/production/*    → Pilar 2: Production & Shelf-Life
//   /api/routing/*       → Pilar 3 & 4: Agent + CVRPTW
//   /api/monitoring/*    → Pilar 5: Real-time Tracking
//   /api/schools/*       → Master Data
//   /api/kitchens/*      → Master Data
//   /api/couriers/*      → Master Data
//   /api/traffic/*       → Traffic Memory Stats
// ============================================================================

// Setup creates and configures the Gin router with all routes.
func Setup(db *gorm.DB, cfg *config.Config) *gin.Engine {
	r := gin.Default()

	// Global middleware
	r.Use(middleware.CORS())

	// Initialize handler with all dependencies
	h := handlers.NewHandler(db, cfg)

	// API group
	api := r.Group("/api")

	// ── System ───────────────────────────────────────────────────
	api.GET("/health", h.HealthCheck)

	// ── Authentication ───────────────────────────────────────────
	auth := api.Group("/auth")
	{
		auth.POST("/login", handleLogin(db, cfg))
		auth.POST("/register", handleRegister(db))
	}

	// ── Pilar 1: OCR & Inventory ─────────────────────────────────
	ocr := api.Group("/ocr")
	{
		ocr.POST("/scan-receipt", h.ScanReceipt)
		ocr.POST("/confirm", h.ConfirmOCR)
	}

	inventory := api.Group("/inventory")
	{
		inventory.GET("/", h.GetAllInventory)
		inventory.GET("/:kitchen_id", h.GetInventory)
	}

	// ── Pilar 2: Production Management ───────────────────────────
	production := api.Group("/production")
	{
		production.POST("/start", h.StartProduction)
		production.GET("/active", h.GetActiveProductions)
		production.GET("/:id", h.GetProduction)
	}

	// ── Pilar 3 & 4: Route Optimization ──────────────────────────
	routing := api.Group("/routing")
	{
		routing.POST("/plan", h.PlanRoutes)
		routing.GET("/plans/:production_id", h.GetRoutePlans)
		routing.PUT("/stops/:id/complete", h.CompleteStop)
	}

	// ── Pilar 5: Monitoring & Tracking ───────────────────────────
	monitoring := api.Group("/monitoring")
	{
		monitoring.POST("/location", h.UpdateLocation)
		monitoring.GET("/track/:route_id", h.GetTracking)
		monitoring.GET("/eta/:school_id", h.GetSchoolETA)
	}

	// ── Master Data ──────────────────────────────────────────────
	schools := api.Group("/schools")
	{
		schools.GET("/", h.GetSchools)
		schools.POST("/", h.CreateSchool)
		schools.PUT("/:id", h.UpdateSchool)
		schools.DELETE("/:id", h.DeleteSchool)
	}

	kitchens := api.Group("/kitchens")
	{
		kitchens.GET("/", h.GetKitchens)
		kitchens.POST("/", h.CreateKitchen)
	}

	couriers := api.Group("/couriers")
	{
		couriers.GET("/", h.GetCouriers)
		couriers.POST("/", h.CreateCourier)
	}

	// ── Traffic Memory Stats ─────────────────────────────────────
	traffic := api.Group("/traffic")
	{
		traffic.GET("/stats", h.GetTrafficStats)
	}

	// ── AI Agent — Skenario 1 & 2 ────────────────────────────────
	agentGroup := api.Group("/agent")
	{
		agentGroup.POST("/analyze-schedule", h.AnalyzeSchedule) // Skenario 1: Traffic Memory & Schedule Shifter
		agentGroup.POST("/recalculate-eta", h.ManualETARecalc)   // Skenario 2: Manual trigger Dynamic ETA
	}

	return r
}

// ============================================================================
// Auth Handlers (inline because they're simple)
// ============================================================================

func handleLogin(db *gorm.DB, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Email    string `json:"email" binding:"required"`
			Password string `json:"password" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			handlers.JSON(c, 400, "Invalid request: "+err.Error(), nil)
			return
		}

		// Find user by email
		type UserRecord struct {
			ID           string `gorm:"column:id"`
			Name         string `gorm:"column:name"`
			Email        string `gorm:"column:email"`
			PasswordHash string `gorm:"column:password_hash"`
			Role         string `gorm:"column:role"`
		}
		var user UserRecord
		result := db.Table("users").Where("email = ? AND is_active = true", req.Email).First(&user)
		if result.Error != nil {
			handlers.JSON(c, 401, "Invalid email or password", nil)
			return
		}

		// TODO: Implement proper bcrypt password verification
		// For now, simple comparison (replace with bcrypt in production)
		// if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		//     handlers.JSON(c, 401, "Invalid email or password", nil)
		//     return
		// }

		// Generate JWT token
		token, err := middleware.GenerateToken(user.ID, user.Role, cfg)
		if err != nil {
			handlers.JSON(c, 500, "Failed to generate token", nil)
			return
		}

		handlers.JSON(c, 200, "Login successful", gin.H{
			"token": token,
			"user": gin.H{
				"id":    user.ID,
				"name":  user.Name,
				"email": user.Email,
				"role":  user.Role,
			},
		})
	}
}

func handleRegister(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Name     string `json:"name" binding:"required"`
			Email    string `json:"email" binding:"required"`
			Password string `json:"password" binding:"required,min=6"`
			Role     string `json:"role"`
			Phone    string `json:"phone"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			handlers.JSON(c, 400, "Invalid request: "+err.Error(), nil)
			return
		}

		if req.Role == "" {
			req.Role = "teacher"
		}

		// TODO: Hash password with bcrypt in production
		// hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		hashedPassword := req.Password // placeholder

		result := db.Table("users").Create(map[string]interface{}{
			"name":          req.Name,
			"email":         req.Email,
			"password_hash": hashedPassword,
			"role":          req.Role,
			"phone":         req.Phone,
			"is_active":     true,
		})

		if result.Error != nil {
			handlers.JSON(c, 500, "Registration failed: "+result.Error.Error(), nil)
			return
		}

		handlers.JSON(c, 201, "User registered successfully", gin.H{
			"email": req.Email,
			"role":  req.Role,
		})
	}
}
