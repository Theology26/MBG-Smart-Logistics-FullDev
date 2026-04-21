package models

import (
	"time"

	"gorm.io/gorm"
)

// ============================================================================
// MBG Smart Logistics — Data Models
// Maps to PostgreSQL schema in migrations/001_init_schema.sql
// ============================================================================

// Kitchen represents a central kitchen (Dapur Umum)
type Kitchen struct {
	ID               string         `json:"id" gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	Name             string         `json:"name" gorm:"type:varchar(255);not null"`
	Address          string         `json:"address" gorm:"type:text;not null"`
	Latitude         float64        `json:"latitude" gorm:"type:double precision;not null"`
	Longitude        float64        `json:"longitude" gorm:"type:double precision;not null"`
	CapacityPortions int            `json:"capacity_portions" gorm:"default:500"`
	IsActive         bool           `json:"is_active" gorm:"default:true"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	DeletedAt        gorm.DeletedAt `json:"-" gorm:"index"`
}

func (Kitchen) TableName() string { return "kitchens" }

// User represents any system user (admin, courier, teacher)
type User struct {
	ID           string         `json:"id" gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	Name         string         `json:"name" gorm:"type:varchar(255);not null"`
	Email        string         `json:"email" gorm:"type:varchar(255);uniqueIndex;not null"`
	PasswordHash string         `json:"-" gorm:"type:varchar(255);not null;column:password_hash"`
	Role         string         `json:"role" gorm:"type:user_role;not null;default:'teacher'"`
	Phone        string         `json:"phone" gorm:"type:varchar(20)"`
	KitchenID    *string        `json:"kitchen_id" gorm:"type:uuid"`
	IsActive     bool           `json:"is_active" gorm:"default:true"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `json:"-" gorm:"index"`

	// Relations
	Kitchen *Kitchen `json:"kitchen,omitempty" gorm:"foreignKey:KitchenID"`
}

func (User) TableName() string { return "users" }

// School represents a target school for food delivery
type School struct {
	ID                  string    `json:"id" gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	Name                string    `json:"name" gorm:"type:varchar(255);not null"`
	Address             string    `json:"address" gorm:"type:text;not null"`
	Area                string    `json:"area" gorm:"type:varchar(100);not null"` // Sukun, Blimbing, etc.
	Latitude            float64   `json:"latitude" gorm:"type:double precision;not null"`
	Longitude           float64   `json:"longitude" gorm:"type:double precision;not null"`
	StudentCount        int       `json:"student_count" gorm:"not null"`
	TeacherPICID        *string   `json:"teacher_pic_id" gorm:"type:uuid"`
	DeliveryWindowStart string    `json:"delivery_window_start" gorm:"type:time;default:'10:00:00'"`
	DeliveryWindowEnd   string    `json:"delivery_window_end" gorm:"type:time;default:'12:00:00'"`
	IsActive            bool      `json:"is_active" gorm:"default:true"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`

	// Relations
	TeacherPIC *User `json:"teacher_pic,omitempty" gorm:"foreignKey:TeacherPICID"`
}

func (School) TableName() string { return "schools" }

// Inventory represents ingredient stock in a kitchen (PILAR 1)
type Inventory struct {
	ID             string    `json:"id" gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	KitchenID      string    `json:"kitchen_id" gorm:"type:uuid;not null"`
	IngredientName string    `json:"ingredient_name" gorm:"type:varchar(255);not null"`
	Unit           string    `json:"unit" gorm:"type:varchar(50);not null"` // kg, liter, butir
	CurrentStock   float64   `json:"current_stock" gorm:"type:decimal(10,2);default:0"`
	MinStockAlert  float64   `json:"min_stock_alert" gorm:"type:decimal(10,2);default:0"`
	UnitPrice      float64   `json:"unit_price" gorm:"type:decimal(12,2);default:0"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`

	// Relations
	Kitchen *Kitchen `json:"kitchen,omitempty" gorm:"foreignKey:KitchenID"`
}

func (Inventory) TableName() string { return "inventory" }

// InventoryTransaction logs stock changes from OCR or manual input (PILAR 1)
type InventoryTransaction struct {
	ID              string     `json:"id" gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	InventoryID     string     `json:"inventory_id" gorm:"type:uuid;not null"`
	TransactionType string     `json:"transaction_type" gorm:"type:transaction_type;not null"`
	Quantity        float64    `json:"quantity" gorm:"type:decimal(10,2);not null"`
	UnitPrice       *float64   `json:"unit_price" gorm:"type:decimal(12,2)"`
	TotalPrice      *float64   `json:"total_price" gorm:"type:decimal(12,2)"`
	ReceiptImageURL *string    `json:"receipt_image_url" gorm:"type:text"`
	OCRRawJSON      *string    `json:"ocr_raw_json" gorm:"type:jsonb"`            // Raw Gemini output
	OCRConfidence   *float64   `json:"ocr_confidence" gorm:"type:decimal(3,2)"`
	VerifiedBy      *string    `json:"verified_by" gorm:"type:uuid"`
	VerifiedAt      *time.Time `json:"verified_at"`
	Notes           *string    `json:"notes" gorm:"type:text"`
	CreatedAt       time.Time  `json:"created_at"`

	// Relations
	Inventory *Inventory `json:"inventory,omitempty" gorm:"foreignKey:InventoryID"`
}

func (InventoryTransaction) TableName() string { return "inventory_transactions" }

// ProductionLog records a batch of cooked food with shelf-life analysis (PILAR 2)
type ProductionLog struct {
	ID                        string    `json:"id" gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	KitchenID                 string    `json:"kitchen_id" gorm:"type:uuid;not null"`
	DishName                  string    `json:"dish_name" gorm:"type:varchar(255);not null"`
	DishCategory              string    `json:"dish_category" gorm:"type:varchar(100)"`
	TotalPortions             int       `json:"total_portions" gorm:"not null"`
	ShelfLifeMinutes          int       `json:"shelf_life_minutes" gorm:"not null"`
	MaxDeliveryWindowMinutes  int       `json:"max_delivery_window_minutes" gorm:"not null"`
	RiskLevel                 string    `json:"risk_level" gorm:"type:varchar(20);default:'medium'"`
	GeminiAnalysisJSON        *string   `json:"gemini_analysis_json" gorm:"type:jsonb"`
	CookedAt                  time.Time `json:"cooked_at" gorm:"not null"`      // ⚠️ JAM MATANG
	DeadlineAt                time.Time `json:"deadline_at" gorm:"not null"`    // Hard constraint
	Status                    string    `json:"status" gorm:"type:production_status;default:'ready'"`
	CreatedBy                 *string   `json:"created_by" gorm:"type:uuid"`
	CreatedAt                 time.Time `json:"created_at"`
	UpdatedAt                 time.Time `json:"updated_at"`

	// Relations
	Kitchen           *Kitchen           `json:"kitchen,omitempty" gorm:"foreignKey:KitchenID"`
	SchoolAssignments []SchoolAssignment `json:"school_assignments,omitempty" gorm:"foreignKey:ProductionLogID"`
	RoutePlans        []RoutePlan        `json:"route_plans,omitempty" gorm:"foreignKey:ProductionLogID"`
}

func (ProductionLog) TableName() string { return "production_logs" }

// SchoolAssignment allocates portions per school for a production batch (PILAR 2)
type SchoolAssignment struct {
	ID                   string     `json:"id" gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	ProductionLogID      string     `json:"production_log_id" gorm:"type:uuid;not null"`
	SchoolID             string     `json:"school_id" gorm:"type:uuid;not null"`
	AllocatedPortions    int        `json:"allocated_portions" gorm:"not null"`
	DeliveredPortions    int        `json:"delivered_portions" gorm:"default:0"`
	DeliveryStatus       string     `json:"delivery_status" gorm:"type:delivery_status;default:'pending'"`
	DeliveredAt          *time.Time `json:"delivered_at"`
	ConfirmedBy          *string    `json:"confirmed_by" gorm:"type:uuid"`
	ConfirmationPhoto    *string    `json:"confirmation_photo" gorm:"type:text"`
	TemperatureOnArrival *float64   `json:"temperature_on_arrival" gorm:"type:decimal(4,1)"`
	Notes                *string    `json:"notes" gorm:"type:text"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`

	// Relations
	ProductionLog *ProductionLog `json:"production_log,omitempty" gorm:"foreignKey:ProductionLogID"`
	School        *School        `json:"school,omitempty" gorm:"foreignKey:SchoolID"`
}

func (SchoolAssignment) TableName() string { return "school_assignments" }

// Courier represents a delivery driver with vehicle info (PILAR 4)
type Courier struct {
	ID                   string     `json:"id" gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	UserID               string     `json:"user_id" gorm:"type:uuid;not null"`
	VehicleType          string     `json:"vehicle_type" gorm:"type:varchar(50);not null"`
	VehiclePlate         string     `json:"vehicle_plate" gorm:"type:varchar(20)"`
	MaxCapacityPortions  int        `json:"max_capacity_portions" gorm:"default:200"`
	IsAvailable          bool       `json:"is_available" gorm:"default:true"`
	CurrentLatitude      *float64   `json:"current_latitude" gorm:"type:double precision"`
	CurrentLongitude     *float64   `json:"current_longitude" gorm:"type:double precision"`
	LastLocationUpdate   *time.Time `json:"last_location_update"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`

	// Relations
	User *User `json:"user,omitempty" gorm:"foreignKey:UserID"`
}

func (Courier) TableName() string { return "couriers" }

// TrafficHistory stores historical travel data for AI Agent memory (PILAR 3)
type TrafficHistory struct {
	ID                    string    `json:"id" gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	OriginArea            string    `json:"origin_area" gorm:"type:varchar(100);not null"`
	DestinationArea       string    `json:"destination_area" gorm:"type:varchar(100);not null"`
	OriginLat             float64   `json:"origin_lat" gorm:"type:double precision;not null"`
	OriginLng             float64   `json:"origin_lng" gorm:"type:double precision;not null"`
	DestLat               float64   `json:"dest_lat" gorm:"type:double precision;not null"`
	DestLng               float64   `json:"dest_lng" gorm:"type:double precision;not null"`
	DayOfWeek             int       `json:"day_of_week" gorm:"not null"`  // 0=Minggu, 6=Sabtu
	HourOfDay             int       `json:"hour_of_day" gorm:"not null"`  // 0-23
	ActualDurationSeconds int       `json:"actual_duration_seconds" gorm:"not null"`
	OSRMEstimatedSeconds  int       `json:"osrm_estimated_seconds" gorm:"not null"`
	CongestionFactor      float64   `json:"congestion_factor" gorm:"type:decimal(4,2);default:1.00"`
	RecordedDate          time.Time `json:"recorded_date" gorm:"type:date;not null"`
	CreatedAt             time.Time `json:"created_at"`
}

func (TrafficHistory) TableName() string { return "traffic_history" }

// RoutePlan represents an optimized route for one courier (PILAR 4)
type RoutePlan struct {
	ID                       string     `json:"id" gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	ProductionLogID          string     `json:"production_log_id" gorm:"type:uuid;not null"`
	CourierID                string     `json:"courier_id" gorm:"type:uuid;not null"`
	KitchenID                string     `json:"kitchen_id" gorm:"type:uuid;not null"`
	PlannedDeparture         time.Time  `json:"planned_departure" gorm:"not null"`
	ActualDeparture          *time.Time `json:"actual_departure"`
	TotalPortions            int        `json:"total_portions" gorm:"not null"`
	TotalStops               int        `json:"total_stops" gorm:"default:0"`
	TotalDistanceMeters      *int       `json:"total_distance_meters"`
	TotalEstimatedDurationS  *int       `json:"total_estimated_duration_s"`
	OptimizationScore        *float64   `json:"optimization_score" gorm:"type:decimal(5,2)"`
	SolverMetadata           *string    `json:"solver_metadata" gorm:"type:jsonb"`
	Status                   string     `json:"status" gorm:"type:route_status;default:'planned'"`
	CompletedAt              *time.Time `json:"completed_at"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                time.Time  `json:"updated_at"`

	// Relations
	ProductionLog *ProductionLog `json:"production_log,omitempty" gorm:"foreignKey:ProductionLogID"`
	Courier       *Courier       `json:"courier,omitempty" gorm:"foreignKey:CourierID"`
	Kitchen       *Kitchen       `json:"kitchen,omitempty" gorm:"foreignKey:KitchenID"`
	Stops         []RouteStop    `json:"stops,omitempty" gorm:"foreignKey:RoutePlanID"`
}

func (RoutePlan) TableName() string { return "route_plans" }

// RouteStop represents one delivery stop in a route (PILAR 4)
type RouteStop struct {
	ID                   string     `json:"id" gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	RoutePlanID          string     `json:"route_plan_id" gorm:"type:uuid;not null"`
	SchoolAssignmentID   string     `json:"school_assignment_id" gorm:"type:uuid;not null"`
	StopSequence         int        `json:"stop_sequence" gorm:"not null"`
	EstimatedArrival     *time.Time `json:"estimated_arrival"`
	ActualArrival        *time.Time `json:"actual_arrival"`
	EstimatedDeparture   *time.Time `json:"estimated_departure"`
	ActualDeparture      *time.Time `json:"actual_departure"`
	DistanceFromPrevM    *int       `json:"distance_from_prev_m"`
	DurationFromPrevS    *int       `json:"duration_from_prev_s"`
	DynamicETA           *time.Time `json:"dynamic_eta"`
	PortionsToDeliver    int        `json:"portions_to_deliver" gorm:"not null"`
	Status               string     `json:"status" gorm:"type:delivery_status;default:'pending'"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`

	// Relations
	RoutePlan        *RoutePlan        `json:"route_plan,omitempty" gorm:"foreignKey:RoutePlanID"`
	SchoolAssignment *SchoolAssignment `json:"school_assignment,omitempty" gorm:"foreignKey:SchoolAssignmentID"`
}

func (RouteStop) TableName() string { return "route_stops" }

// DeliveryTracking stores GPS data from courier devices (PILAR 5)
type DeliveryTracking struct {
	ID             string    `json:"id" gorm:"type:uuid;primaryKey;default:uuid_generate_v4()"`
	RoutePlanID    string    `json:"route_plan_id" gorm:"type:uuid;not null"`
	CourierID      string    `json:"courier_id" gorm:"type:uuid;not null"`
	Latitude       float64   `json:"latitude" gorm:"type:double precision;not null"`
	Longitude      float64   `json:"longitude" gorm:"type:double precision;not null"`
	SpeedKmh       *float64  `json:"speed_kmh" gorm:"type:decimal(5,1)"`
	Heading        *float64  `json:"heading" gorm:"type:decimal(5,1)"`
	AccuracyMeters *float64  `json:"accuracy_meters" gorm:"type:decimal(6,1)"`
	RecordedAt     time.Time `json:"recorded_at" gorm:"default:NOW()"`
}

func (DeliveryTracking) TableName() string { return "delivery_tracking" }

// ============================================================================
// Request/Response DTOs (not stored in DB)
// ============================================================================

// ProductionStartRequest is the input when kitchen admin triggers production
type ProductionStartRequest struct {
	DishName    string              `json:"dish_name" binding:"required"`
	TotalPortions int              `json:"total_portions" binding:"required,min=1"`
	KitchenID   string              `json:"kitchen_id" binding:"required"`
	CookedAt    time.Time           `json:"cooked_at" binding:"required"`
	Assignments []AssignmentInput   `json:"assignments" binding:"required,min=1"`
}

// AssignmentInput is the portion allocation per school
type AssignmentInput struct {
	SchoolID          string `json:"school_id" binding:"required"`
	AllocatedPortions int    `json:"allocated_portions" binding:"required,min=1"`
}

// OCRConfirmRequest is used to confirm OCR results and update stock
type OCRConfirmRequest struct {
	KitchenID string         `json:"kitchen_id" binding:"required"`
	Items     []OCRItemInput `json:"items" binding:"required"`
	ImageURL  string         `json:"image_url"`
	RawJSON   string         `json:"raw_json"`
}

// OCRItemInput is a single confirmed OCR item
type OCRItemInput struct {
	IngredientName string  `json:"ingredient_name" binding:"required"`
	Quantity       float64 `json:"quantity" binding:"required"`
	Unit           string  `json:"unit" binding:"required"`
	UnitPrice      float64 `json:"unit_price"`
	TotalPrice     float64 `json:"total_price"`
}

// LocationUpdateRequest is sent by courier app
type LocationUpdateRequest struct {
	RoutePlanID    string   `json:"route_plan_id" binding:"required"`
	CourierID      string   `json:"courier_id" binding:"required"`
	Latitude       float64  `json:"latitude" binding:"required"`
	Longitude      float64  `json:"longitude" binding:"required"`
	SpeedKmh       *float64 `json:"speed_kmh"`
	Heading        *float64 `json:"heading"`
	AccuracyMeters *float64 `json:"accuracy_meters"`
}

// StopCompleteRequest is sent when courier completes a drop-off
type StopCompleteRequest struct {
	DeliveredPortions int    `json:"delivered_portions" binding:"required"`
	Notes             string `json:"notes"`
	PhotoURL          string `json:"photo_url"`
}
