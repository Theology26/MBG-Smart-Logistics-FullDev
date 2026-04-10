package main

import "time"

type User struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Password  string    `json:"-"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type School struct {
	ID               uint      `json:"id" gorm:"primaryKey"`
	Name             string    `json:"name"`
	Address          string    `json:"address"`
	Latitude         float64   `json:"latitude"`
	Longitude        float64   `json:"longitude"`
	StudentCount     int       `json:"student_count"`
	DeliveryDeadline string    `json:"delivery_deadline"` 
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Ingredient struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	Name         string    `json:"name"`
	CurrentStock int       `json:"current_stock"`
	UnitPrice    float64   `json:"unit_price"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Menu struct {
	ID            uint      `json:"id" gorm:"primaryKey"`
	Name          string    `json:"name"`
	NutritionInfo string    `json:"nutrition_info"`
	EstimatedCost float64   `json:"estimated_cost"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Schedule struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Date      string    `json:"date"`
	SchoolID  uint      `json:"school_id"`
	MenuID    uint      `json:"menu_id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Delivery struct {
	ID                   uint       `json:"id" gorm:"primaryKey"`
	ScheduleID           uint       `json:"schedule_id"`
	KurirID              *uint      `json:"kurir_id"`
	CookedTime           *time.Time `json:"cooked_time"`
	SafeToEatDeadline    *time.Time `json:"safe_to_eat_deadline"`
	RecommendedDeparture *time.Time `json:"recommended_departure"`
	Status               string     `json:"status"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}
