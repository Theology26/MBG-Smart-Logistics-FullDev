package main

import (
	"log"

	"github.com/gin-gonic/gin"
)

// Response format
func JSONResponse(c *gin.Context, status int, message string, data interface{}) {
	c.JSON(status, gin.H{
		"status":  status,
		"message": message,
		"data":    data,
	})
}

func main() {
	// Initialize Database
	InitDB()

	// Initialize Gin
	r := gin.Default()

	// CORS Middleware (simple for development)
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	api := r.Group("/api")

	// Authentication (Mock)
	auth := api.Group("/auth")
	{
		auth.POST("/login", func(c *gin.Context) {
			// Mock JWT
			data := map[string]string{"token": "dummy_jwt_token_12345", "role": "admin"}
			JSONResponse(c, 200, "Login successful", data)
		})
		auth.POST("/register", func(c *gin.Context) {
			JSONResponse(c, 200, "User registered", nil)
		})
	}

	// Schools CRUD
	schools := api.Group("/schools")
	{
		schools.GET("/", func(c *gin.Context) {
			var list []School
			if DB != nil {
				DB.Find(&list)
			}
			JSONResponse(c, 200, "Schools retrieved", list)
		})
		schools.POST("/", func(c *gin.Context) {
			var s School
			if err := c.ShouldBindJSON(&s); err == nil && DB != nil {
				DB.Create(&s)
			}
			JSONResponse(c, 200, "School created", s)
		})
		schools.PUT("/:id", func(c *gin.Context) {
			JSONResponse(c, 200, "School updated", nil)
		})
		schools.DELETE("/:id", func(c *gin.Context) {
			JSONResponse(c, 200, "School deleted", nil)
		})
	}

	// Inventory CRUD
	inventory := api.Group("/inventory")
	{
		inventory.GET("/", func(c *gin.Context) {
			var list []Ingredient
			if DB != nil {
				DB.Find(&list)
			}
			JSONResponse(c, 200, "Inventory retrieved", list)
		})
		inventory.POST("/", func(c *gin.Context) {
			var item Ingredient
			if err := c.ShouldBindJSON(&item); err == nil && DB != nil {
				DB.Create(&item)
			}
			JSONResponse(c, 200, "Inventory created", item)
		})
	}

	// Menus CRUD
	menus := api.Group("/menus")
	{
		menus.GET("/", func(c *gin.Context) {
			var list []Menu
			if DB != nil {
				DB.Find(&list)
			}
			JSONResponse(c, 200, "Menus retrieved", list)
		})
		menus.POST("/", func(c *gin.Context) {
			var m Menu
			if err := c.ShouldBindJSON(&m); err == nil && DB != nil {
				DB.Create(&m)
			}
			JSONResponse(c, 200, "Menu created", m)
		})
	}

	// Deliveries CRUD
	deliveries := api.Group("/deliveries")
	{
		deliveries.GET("/", func(c *gin.Context) {
			var list []Delivery
			if DB != nil {
				DB.Find(&list)
			}
			JSONResponse(c, 200, "Deliveries retrieved", list)
		})
		deliveries.PUT("/:id/status", func(c *gin.Context) {
			// e.g. status: "picked_up", "delivered"
			var payload struct {
				Status string `json:"status"`
			}
			c.ShouldBindJSON(&payload)
			if DB != nil {
				DB.Model(&Delivery{}).Where("id = ?", c.Param("id")).Update("status", payload.Status)
			}
			JSONResponse(c, 200, "Delivery status updated to "+payload.Status, nil)
		})
	}

	// MOCK AI Endpoints
	ai := api.Group("/ai")
	{
		ai.POST("/scan-receipt", func(c *gin.Context) {
			mockData := map[string]interface{}{
				"detected_items": []interface{}{
					map[string]interface{}{"name": "Beras", "added_stock": 50, "unit": "kg"},
					map[string]interface{}{"name": "Telur", "added_stock": 20, "unit": "kg"},
				},
				"total_cost": 750000,
			}
			JSONResponse(c, 200, "Receipt scanned successfully (MOCK OCR)", mockData)
		})

		ai.GET("/optimize-route", func(c *gin.Context) {
			mockData := map[string]interface{}{
				"recommended_departure": "06:30:00",
				"route_sequence": []int{4, 1, 3, 2},
				"estimated_duration": "45 mins",
				"safe_to_eat_deadline": "11:30:00",
			}
			JSONResponse(c, 200, "Route combined and optimized (MOCK HRL AI)", mockData)
		})
	}

	log.Println("Server running at http://localhost:8080")
	r.Run(":8080")
}
