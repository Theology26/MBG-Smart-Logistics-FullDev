package main

import (
	"log"
	"os"

	"go-api/internal/config"
	"go-api/internal/database"
	"go-api/internal/router"
)

// ============================================================================
// MBG Smart Logistics — API Server
// ============================================================================
// Architecture: 5-Pillar System
//   1. AI OCR & Stock Management (Gemini Vision)
//   2. Production Trigger & Shelf-Life AI (Gemini)
//   3. AI Agent - Dynamic Scheduler (The Brain)
//   4. High-Precision Multi-Stop Routing (OSRM + CVRPTW)
//   5. Teacher Monitoring & Dynamic ETA (WebSocket)
// ============================================================================

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	// Load configuration from .env
	cfg := config.Load()

	// Connect to PostgreSQL
	db := database.Connect(cfg)

	// Setup Gin router with all routes, middleware, and services
	r := router.Setup(db, cfg)

	port := cfg.ServerPort
	if port == "" {
		port = "8080"
	}

	log.Println("╔═══════════════════════════════════════════════════════╗")
	log.Println("║   🚀 MBG Smart Logistics API Server                 ║")
	log.Println("║   📍 Kota Malang, Jawa Timur                        ║")
	log.Printf("║   🌐 http://localhost:%s                          ║\n", port)
	log.Println("║   📦 PostgreSQL + OSRM + Gemini AI                  ║")
	log.Println("╚═══════════════════════════════════════════════════════╝")

	if err := r.Run(":" + port); err != nil {
		log.Fatalf("❌ Failed to start server: %v", err)
		os.Exit(1)
	}
}
