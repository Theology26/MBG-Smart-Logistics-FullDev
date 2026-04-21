-- ============================================================================
-- MBG SMART LOGISTICS — PostgreSQL Schema v2.0
-- Kota Malang, Jawa Timur
-- ============================================================================
-- Run: psql -U mbg_admin -d mbg_smart_logistics -f 001_init_schema.sql
-- ============================================================================

-- Extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ============================================================================
-- ENUM TYPES
-- ============================================================================
CREATE TYPE user_role AS ENUM ('super_admin', 'kitchen_admin', 'courier', 'teacher');
CREATE TYPE production_status AS ENUM ('cooking', 'ready', 'dispatched', 'completed', 'expired');
CREATE TYPE delivery_status AS ENUM ('pending', 'in_transit', 'arrived', 'delivered', 'confirmed', 'failed');
CREATE TYPE route_status AS ENUM ('planned', 'active', 'completed', 'cancelled');
CREATE TYPE transaction_type AS ENUM ('purchase', 'usage', 'adjustment', 'wastage');

-- ============================================================================
-- 1. KITCHENS (Dapur Umum)
-- ============================================================================
CREATE TABLE kitchens (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name            VARCHAR(255) NOT NULL,
    address         TEXT NOT NULL,
    latitude        DOUBLE PRECISION NOT NULL,
    longitude       DOUBLE PRECISION NOT NULL,
    capacity_portions INT NOT NULL DEFAULT 500,
    is_active       BOOLEAN DEFAULT true,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

COMMENT ON TABLE kitchens IS 'Dapur umum yang memproduksi makanan bergizi gratis';
COMMENT ON COLUMN kitchens.capacity_portions IS 'Kapasitas maksimal porsi per batch produksi';

-- ============================================================================
-- 2. USERS
-- ============================================================================
CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name            VARCHAR(255) NOT NULL,
    email           VARCHAR(255) UNIQUE NOT NULL,
    password_hash   VARCHAR(255) NOT NULL,
    role            user_role NOT NULL DEFAULT 'teacher',
    phone           VARCHAR(20),
    kitchen_id      UUID REFERENCES kitchens(id) ON DELETE SET NULL,
    is_active       BOOLEAN DEFAULT true,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_users_role ON users(role);
CREATE INDEX idx_users_kitchen ON users(kitchen_id);

COMMENT ON TABLE users IS 'Semua pengguna sistem: admin dapur, kurir, guru';

-- ============================================================================
-- 3. SCHOOLS (Sekolah Tujuan)
-- ============================================================================
CREATE TABLE schools (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name                VARCHAR(255) NOT NULL,
    address             TEXT NOT NULL,
    area                VARCHAR(100) NOT NULL,  -- Sukun, Blimbing, Lowokwaru, etc.
    latitude            DOUBLE PRECISION NOT NULL,
    longitude           DOUBLE PRECISION NOT NULL,
    student_count       INT NOT NULL,
    teacher_pic_id      UUID REFERENCES users(id) ON DELETE SET NULL,
    delivery_window_start TIME DEFAULT '10:00:00',
    delivery_window_end   TIME DEFAULT '12:00:00',
    is_active           BOOLEAN DEFAULT true,
    created_at          TIMESTAMPTZ DEFAULT NOW(),
    updated_at          TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_schools_area ON schools(area);

COMMENT ON TABLE schools IS 'Sekolah penerima makanan bergizi';
COMMENT ON COLUMN schools.area IS 'Area/kecamatan di Kota Malang untuk lookup traffic_history';
COMMENT ON COLUMN schools.delivery_window_start IS 'Jam paling awal sekolah bisa menerima makanan';
COMMENT ON COLUMN schools.delivery_window_end IS 'Jam paling akhir sekolah bisa menerima makanan';

-- ============================================================================
-- 4. INVENTORY (Stok Bahan Baku) — PILAR 1
-- ============================================================================
CREATE TABLE inventory (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    kitchen_id      UUID NOT NULL REFERENCES kitchens(id) ON DELETE CASCADE,
    ingredient_name VARCHAR(255) NOT NULL,
    unit            VARCHAR(50) NOT NULL,  -- kg, liter, butir, ikat, bungkus
    current_stock   DECIMAL(10,2) NOT NULL DEFAULT 0,
    min_stock_alert DECIMAL(10,2) DEFAULT 0,
    unit_price      DECIMAL(12,2) DEFAULT 0,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(kitchen_id, ingredient_name)
);

CREATE INDEX idx_inventory_kitchen ON inventory(kitchen_id);

COMMENT ON TABLE inventory IS 'Stok bahan baku per dapur — diisi via OCR scan nota';

-- ============================================================================
-- 5. INVENTORY TRANSACTIONS (Log Mutasi Stok dari OCR) — PILAR 1
-- ============================================================================
CREATE TABLE inventory_transactions (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    inventory_id        UUID NOT NULL REFERENCES inventory(id) ON DELETE CASCADE,
    transaction_type    transaction_type NOT NULL,
    quantity            DECIMAL(10,2) NOT NULL,
    unit_price          DECIMAL(12,2),
    total_price         DECIMAL(12,2),
    receipt_image_url   TEXT,
    ocr_raw_json        JSONB,           -- Raw output dari Gemini OCR
    ocr_confidence      DECIMAL(3,2),    -- 0.00 - 1.00
    verified_by         UUID REFERENCES users(id),
    verified_at         TIMESTAMPTZ,
    notes               TEXT,
    created_at          TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_inv_tx_inventory ON inventory_transactions(inventory_id);
CREATE INDEX idx_inv_tx_type ON inventory_transactions(transaction_type);
CREATE INDEX idx_inv_tx_date ON inventory_transactions(created_at);

COMMENT ON TABLE inventory_transactions IS 'Log setiap mutasi stok — dari OCR scan atau input manual';
COMMENT ON COLUMN inventory_transactions.ocr_raw_json IS 'Menyimpan JSON mentah dari Gemini untuk audit trail';

-- ============================================================================
-- 6. PRODUCTION LOGS (Masakan + Shelf-Life) — PILAR 2
-- ============================================================================
CREATE TABLE production_logs (
    id                          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    kitchen_id                  UUID NOT NULL REFERENCES kitchens(id) ON DELETE CASCADE,
    dish_name                   VARCHAR(255) NOT NULL,
    dish_category               VARCHAR(100),   -- sayur_kuah, gorengan, nasi, lauk_kering, etc.
    total_portions              INT NOT NULL,
    shelf_life_minutes          INT NOT NULL,    -- Dari Gemini AI
    max_delivery_window_minutes INT NOT NULL,    -- shelf_life - 30 min buffer
    risk_level                  VARCHAR(20) DEFAULT 'medium',  -- low, medium, high, critical
    gemini_analysis_json        JSONB,           -- Full Gemini response
    cooked_at                   TIMESTAMPTZ NOT NULL,  -- ⚠️ JAM MATANG (kritis!)
    deadline_at                 TIMESTAMPTZ NOT NULL,  -- cooked_at + max_delivery_window
    status                      production_status DEFAULT 'ready',
    created_by                  UUID REFERENCES users(id),
    created_at                  TIMESTAMPTZ DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_prod_kitchen ON production_logs(kitchen_id);
CREATE INDEX idx_prod_status ON production_logs(status);
CREATE INDEX idx_prod_deadline ON production_logs(deadline_at);
CREATE INDEX idx_prod_cooked ON production_logs(cooked_at);

COMMENT ON TABLE production_logs IS 'Log setiap batch masakan — shelf-life dianalisis oleh Gemini AI';
COMMENT ON COLUMN production_logs.cooked_at IS 'KRITIS: Jam makanan matang, digunakan Agent untuk hitung mundur';
COMMENT ON COLUMN production_logs.deadline_at IS 'HARD CONSTRAINT: Batas akhir makanan harus diterima semua sekolah';

-- ============================================================================
-- 7. SCHOOL ASSIGNMENTS (Alokasi Porsi per Sekolah) — PILAR 2
-- ============================================================================
CREATE TABLE school_assignments (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    production_log_id   UUID NOT NULL REFERENCES production_logs(id) ON DELETE CASCADE,
    school_id           UUID NOT NULL REFERENCES schools(id) ON DELETE CASCADE,
    allocated_portions  INT NOT NULL,
    delivered_portions  INT DEFAULT 0,
    delivery_status     delivery_status DEFAULT 'pending',
    delivered_at        TIMESTAMPTZ,
    confirmed_by        UUID REFERENCES users(id),    -- Guru yang konfirmasi
    confirmation_photo  TEXT,
    temperature_on_arrival DECIMAL(4,1),  -- Suhu saat tiba (opsional)
    notes               TEXT,
    created_at          TIMESTAMPTZ DEFAULT NOW(),
    updated_at          TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(production_log_id, school_id)
);

CREATE INDEX idx_sa_production ON school_assignments(production_log_id);
CREATE INDEX idx_sa_school ON school_assignments(school_id);
CREATE INDEX idx_sa_status ON school_assignments(delivery_status);

COMMENT ON TABLE school_assignments IS 'Alokasi porsi per sekolah untuk setiap batch produksi';

-- ============================================================================
-- 8. COURIERS (Kurir + Kendaraan) — PILAR 4
-- ============================================================================
CREATE TABLE couriers (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    vehicle_type        VARCHAR(50) NOT NULL,   -- motor, mobil_box, pickup
    vehicle_plate       VARCHAR(20),
    max_capacity_portions INT NOT NULL DEFAULT 200,
    is_available        BOOLEAN DEFAULT true,
    current_latitude    DOUBLE PRECISION,
    current_longitude   DOUBLE PRECISION,
    last_location_update TIMESTAMPTZ,
    created_at          TIMESTAMPTZ DEFAULT NOW(),
    updated_at          TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_courier_available ON couriers(is_available) WHERE is_available = true;

COMMENT ON TABLE couriers IS 'Data kurir dan kendaraan untuk constraint kapasitas CVRPTW';

-- ============================================================================
-- 9. TRAFFIC HISTORY (Memory untuk AI Agent) — PILAR 3
-- ============================================================================
CREATE TABLE traffic_history (
    id                      UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    origin_area             VARCHAR(100) NOT NULL,
    destination_area        VARCHAR(100) NOT NULL,
    origin_lat              DOUBLE PRECISION NOT NULL,
    origin_lng              DOUBLE PRECISION NOT NULL,
    dest_lat                DOUBLE PRECISION NOT NULL,
    dest_lng                DOUBLE PRECISION NOT NULL,
    day_of_week             INT NOT NULL CHECK (day_of_week BETWEEN 0 AND 6),
    hour_of_day             INT NOT NULL CHECK (hour_of_day BETWEEN 0 AND 23),
    actual_duration_seconds INT NOT NULL,
    osrm_estimated_seconds  INT NOT NULL,
    congestion_factor       DECIMAL(4,2) NOT NULL DEFAULT 1.00,
    recorded_date           DATE NOT NULL,
    created_at              TIMESTAMPTZ DEFAULT NOW()
);

-- Composite index for the Agent's primary lookup pattern
CREATE INDEX idx_traffic_lookup ON traffic_history(
    origin_area, destination_area, day_of_week, hour_of_day
);
CREATE INDEX idx_traffic_date ON traffic_history(recorded_date DESC);

COMMENT ON TABLE traffic_history IS 'Memory AI Agent: histori duasi perjalanan untuk prediksi kemacetan';
COMMENT ON COLUMN traffic_history.congestion_factor IS 'Rasio actual/estimated — 1.0=normal, 1.5=macet 50%, 2.0=macet 100%';
COMMENT ON COLUMN traffic_history.day_of_week IS '0=Minggu, 1=Senin, ..., 6=Sabtu';

-- ============================================================================
-- 10. ROUTE PLANS (Output CVRPTW Solver) — PILAR 4
-- ============================================================================
CREATE TABLE route_plans (
    id                          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    production_log_id           UUID NOT NULL REFERENCES production_logs(id) ON DELETE CASCADE,
    courier_id                  UUID NOT NULL REFERENCES couriers(id),
    kitchen_id                  UUID NOT NULL REFERENCES kitchens(id),
    planned_departure           TIMESTAMPTZ NOT NULL,
    actual_departure            TIMESTAMPTZ,
    total_portions              INT NOT NULL,
    total_stops                 INT NOT NULL DEFAULT 0,
    total_distance_meters       INT,
    total_estimated_duration_s  INT,
    optimization_score          DECIMAL(5,2),
    solver_metadata             JSONB,      -- Algorithm metrics, iterations, etc.
    status                      route_status DEFAULT 'planned',
    completed_at                TIMESTAMPTZ,
    created_at                  TIMESTAMPTZ DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_route_production ON route_plans(production_log_id);
CREATE INDEX idx_route_courier ON route_plans(courier_id);
CREATE INDEX idx_route_status ON route_plans(status);

COMMENT ON TABLE route_plans IS 'Rencana rute per kurir — output dari CVRPTW solver';

-- ============================================================================
-- 11. ROUTE STOPS (Detail Setiap Stop dalam Rute) — PILAR 4
-- ============================================================================
CREATE TABLE route_stops (
    id                      UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    route_plan_id           UUID NOT NULL REFERENCES route_plans(id) ON DELETE CASCADE,
    school_assignment_id    UUID NOT NULL REFERENCES school_assignments(id),
    stop_sequence           INT NOT NULL,
    estimated_arrival       TIMESTAMPTZ,
    actual_arrival          TIMESTAMPTZ,
    estimated_departure     TIMESTAMPTZ,
    actual_departure        TIMESTAMPTZ,
    distance_from_prev_m    INT,            -- Jarak dari stop sebelumnya (meter)
    duration_from_prev_s    INT,            -- Durasi dari stop sebelumnya (detik)
    dynamic_eta             TIMESTAMPTZ,    -- ETA real-time yang terus di-update
    portions_to_deliver     INT NOT NULL,
    status                  delivery_status DEFAULT 'pending',
    created_at              TIMESTAMPTZ DEFAULT NOW(),
    updated_at              TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_stops_plan ON route_stops(route_plan_id, stop_sequence);
CREATE INDEX idx_stops_status ON route_stops(status);

COMMENT ON TABLE route_stops IS 'Setiap titik stop dalam rute — urutan kunjungan sekolah';
COMMENT ON COLUMN route_stops.dynamic_eta IS 'ETA yang di-recalculate setiap kali kurir selesai drop-off';

-- ============================================================================
-- 12. DELIVERY TRACKING (GPS Real-time) — PILAR 5
-- ============================================================================
CREATE TABLE delivery_tracking (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    route_plan_id   UUID NOT NULL REFERENCES route_plans(id) ON DELETE CASCADE,
    courier_id      UUID NOT NULL REFERENCES couriers(id),
    latitude        DOUBLE PRECISION NOT NULL,
    longitude       DOUBLE PRECISION NOT NULL,
    speed_kmh       DECIMAL(5,1),
    heading         DECIMAL(5,1),   -- Arah 0-360 derajat
    accuracy_meters DECIMAL(6,1),   -- Akurasi GPS
    recorded_at     TIMESTAMPTZ DEFAULT NOW()
);

-- Time-series index for efficient tracking queries
CREATE INDEX idx_tracking_route_time ON delivery_tracking(route_plan_id, recorded_at DESC);
CREATE INDEX idx_tracking_courier_time ON delivery_tracking(courier_id, recorded_at DESC);

COMMENT ON TABLE delivery_tracking IS 'Data GPS real-time dari HP kurir';
COMMENT ON COLUMN delivery_tracking.accuracy_meters IS 'Akurasi GPS — penting di daerah padat seperti Dinoyo';

-- ============================================================================
-- SEED DATA: Malang Traffic Patterns (Bootstrap Memory)
-- ============================================================================
-- Known congestion points in Kota Malang for initial Agent memory

INSERT INTO kitchens (name, address, latitude, longitude, capacity_portions) VALUES
('Dapur Utama MBG Malang', 'Jl. Veteran No. 1, Lowokwaru, Malang', -7.9666, 112.6326, 1000);

-- Seed traffic patterns (weekday morning + lunch rush)
-- Senin-Jumat (day 1-5), jam sibuk
DO $$
DECLARE
    v_days INT[] := ARRAY[1,2,3,4,5];
    v_day INT;
BEGIN
    FOREACH v_day IN ARRAY v_days
    LOOP
        -- Lowokwaru → Sukun (jam 11 selalu macet)
        INSERT INTO traffic_history (origin_area, destination_area, origin_lat, origin_lng, dest_lat, dest_lng, day_of_week, hour_of_day, actual_duration_seconds, osrm_estimated_seconds, congestion_factor, recorded_date) VALUES
        ('Lowokwaru', 'Sukun', -7.9666, 112.6326, -8.0010, 112.6180, v_day, 11, 2700, 1500, 1.80, CURRENT_DATE - INTERVAL '7 days'),
        ('Lowokwaru', 'Sukun', -7.9666, 112.6326, -8.0010, 112.6180, v_day, 12, 2400, 1500, 1.60, CURRENT_DATE - INTERVAL '7 days');
        
        -- Lowokwaru → Blimbing (jam 7-8 macet)
        INSERT INTO traffic_history (origin_area, destination_area, origin_lat, origin_lng, dest_lat, dest_lng, day_of_week, hour_of_day, actual_duration_seconds, osrm_estimated_seconds, congestion_factor, recorded_date) VALUES
        ('Lowokwaru', 'Blimbing', -7.9666, 112.6326, -7.9520, 112.6550, v_day, 7, 2100, 1200, 1.75, CURRENT_DATE - INTERVAL '7 days'),
        ('Lowokwaru', 'Blimbing', -7.9666, 112.6326, -7.9520, 112.6550, v_day, 8, 1800, 1200, 1.50, CURRENT_DATE - INTERVAL '7 days');
        
        -- Lowokwaru → Klojen (Dinoyo area, jam 11-13 macet)
        INSERT INTO traffic_history (origin_area, destination_area, origin_lat, origin_lng, dest_lat, dest_lng, day_of_week, hour_of_day, actual_duration_seconds, osrm_estimated_seconds, congestion_factor, recorded_date) VALUES
        ('Lowokwaru', 'Klojen', -7.9666, 112.6326, -7.9780, 112.6310, v_day, 11, 1800, 1000, 1.80, CURRENT_DATE - INTERVAL '7 days'),
        ('Lowokwaru', 'Klojen', -7.9666, 112.6326, -7.9780, 112.6310, v_day, 12, 1600, 1000, 1.60, CURRENT_DATE - INTERVAL '7 days');
        
        -- Lowokwaru → Kedungkandang (relatif lancar)
        INSERT INTO traffic_history (origin_area, destination_area, origin_lat, origin_lng, dest_lat, dest_lng, day_of_week, hour_of_day, actual_duration_seconds, osrm_estimated_seconds, congestion_factor, recorded_date) VALUES
        ('Lowokwaru', 'Kedungkandang', -7.9666, 112.6326, -7.9880, 112.6650, v_day, 11, 1700, 1400, 1.21, CURRENT_DATE - INTERVAL '7 days');
    END LOOP;
END $$;
