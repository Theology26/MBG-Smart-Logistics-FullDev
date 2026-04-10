package main

import (
	"log"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var DB *gorm.DB

func InitDB() {
	// Standard MySQL DSN matching Laravel default (.env)
	// We'll use a placeholder db name 'laravel' or 'mbg_smart_logistics'
	dsn := "root:@tcp(127.0.0.1:3306)/mbg_smart_logistics?charset=utf8mb4&parseTime=True&loc=Local"
	
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Println("WARNING: Failed to connect to MySQL. Continuing without DB connection (MOCK mode). Error:", err)
	} else {
		log.Println("Successfully connected to MySQL database!")
		DB = db
	}
}
