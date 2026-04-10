# MBG Smart Logistics (Makanan Bergizi Gratis) - Malang Area

![Use Case MBG Smart Logistics](USE%20CASE%20MBG%20SMART%20LOGISTICS.png)

Proyek Sistem Logistik Cerdas untuk distribusi makanan bergizi di area Malang menggunakan arsitektur Microservices dengan integrasi Laravel & Go.

##  Arsitektur Sistem

Sistem ini terbagi menjadi dua bagian utama:

### 1. `laravel-core` (Backend Admin & Frontend Web)
- **Role**: Pengelola Database, Migrations, dan Antarmuka Web (Blade + Tailwind).
- **Teknologi**: PHP 8.2+, Laravel 12.
- **Tanggung Jawab**:
    - Manajemen Struktur Database (Migrations).
    - UI Dashboard Admin & Dapur (Desktop).
    - UI Kurir & Sekolah (Mobile-First/PWA).
    - Client-side data fetching (menghubungi Go API via JS).

### 2. `go-api` (REST API Service)
- **Role**: Penyedia layanan API utama dan Mock AI logic.
- **Teknologi**: Golang (Gin Framework), GORM.
- **Tanggung Jawab**:
    - RESTful API endpoints untuk seluruh entitas (Users, Schools, Menus, dsb).
    - Integrasi basis data MySQL (membaca DB yang dibuat Laravel).
    - Endpoint Mock AI (Optimize Route HRL & OCR Scan Receipt).

---

## Cara Menjalankan Proyek

### Fase 1: Database & Laravel Setup
1. Pastikan MySQL sudah menyala. Buat database bernama `mbg_smart_logistics`.
2. Masuk ke folder `/laravel-core`.
3. Jalankan migrasi:
   ```bash
   php artisan migrate:fresh
   ```
4. Jalankan server Laravel (untuk Frontend):
   ```bash
   php artisan serve
   ```

### Fase 2: Go API Setup
1. Pastikan Go sudah terinstall.
2. Masuk ke folder `/go-api`.
3. Jalankan server API:
   ```bash
   go run main.go database.go models.go
   ```
   *Server akan berjalan di `http://localhost:8080`*

---

## Endpoint API Utama (Lokal)
- **Auth**: `POST /api/auth/login`
- **Schools**: `GET /api/schools`
- **Inventory**: `POST /api/inventory`
- **AI Route**: `GET /api/ai/optimize-route`

---
*Developed as part of the MBG Smart Logistics modernization.*
