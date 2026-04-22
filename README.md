# 🚀 MBG Smart Logistics — Makanan Bergizi Gratis

**Sistem Logistik Cerdas untuk distribusi makanan bergizi di Kota Malang, Jawa Timur.**

MBG Smart Logistics adalah platform cerdas yang menggabungkan Laravel (Frontend/Admin), Golang (Core API), OSRM (Routing Engine), dan Gemini/Local AI (Intelligence) untuk memastikan pengiriman ransum makanan bergizi bagi anak sekolah tiba tepat waktu dengan kualitas gizi yang masih terjaga.

![Arsitektur Alur Sistem](USE%20CASE%20MBG%20SMART%20LOGISTICS.png)

---

## 🌟 Fitur Utama (Arsitektur 5 Pilar)

1. **AI OCR & Stock Management**: Ubah foto nota belanja mentah menjadi data stok terstruktur otomatis.
2. **Production Trigger & Shelf-Life AI**: Analisis daya tahan makanan secara dinamis oleh AI untuk menetapkan batas waktu pengiriman (*Maximum Delivery Window*).
3. **AI Agent / Local Heuristics**: Otak penjadwalan cerdas yang membaca traffic memory untuk memberikan instruksi Backup Plan otomatis jika macet.
4. **Multi-Stop Routing (CVRPTW)**: Implementasi algoritma OSRM mencari rute tercepat antar-sekolah secara *multi-stop* berdasarkan kapasitas kendaraan.
5. **Teacher Monitoring & Dynamic ETA**: Dashboard monitoring untuk di mana setiap "Selesai Drop-Off" di sekolah A, akan langsung otomatis mengkalkulasi ulang sisa waktu (ETA) untuk Sekolah B, C, dst-nya.

---

## 🗄️ Penjelasan Database & Tabel Lengkap

![ERD PostgreSQL PostgreSQL 16](ERD%20MBG%20SMART%20LOGISTICS.png)
![Class Diagram MBG Backend](CLASS%20DIAGRAM%20MBG%20SMART%20LOGISTICS.png)

Database menggunakan **PostgreSQL 16**.
1. **users**: Entitas kredensial (Role: Admin, Kurir, Guru).
2. **kitchens**: Data basecamp titik awal.
3. **schools**: Titik tujuan pengiriman yang wajib dicapai sebelum `delivery_window` habis.
4. **couriers**: Master agent dengan atribut batas bawa porsi (`max_capacity_portions`).
5. **inventory**: Tabel rekap stok bahan mentah di dapur.
6. **inventory_transactions**: Histori masuk/keluar barang (OCR tersimpan di tabel ini pada atribut *notes*).
7. **production_logs**: Titik awal bergeraknya sistem! Menyimpan *dish_name* (nama resep) serta **batas kedaluwarsa** (_shelf_life_minutes_) yang akan dijaga ketat oleh sistem OSRM.
8. **school_assignments**: Penugasan porsi makanan untuk masing-masing porsi sekolah.
9. **route_plans**: Bundel jadwal OSRM per kurir. 1 Kurir = 1 *route_plan*.
10. **route_stops**: Rantai urutan sekolah yang dilalui dalam 1 route plan (Terdapat *dynamic_eta*).
11. **delivery_tracking**: Sinkronisasi GPS live dari HP Kurir.
12. **traffic_history**: Catatan kecerdasan AI terkait status kepadatan wilayah.

---

## 🔄 Alur Sistem (Sequence Diagram)

Berikut adalah urutan alur kerja (*sequence diagram*) untuk fitur-fitur utama di MBG Smart Logistics:

1. **Alur Dapur - Scan Nota Belanja (AI OCR)**
![Sequence Diagram Dapur Scan Nota](SEQUENCE%20DIAGRAM%20DAPUR%20SCAN%20NOTA.png)

2. **Alur Dapur - Produksi & Prediksi Ketahanan Makanan (AI Shelf-Life)**
![Sequence Diagram Dapur Pilih Menu](SEQUENCE%20DIAGRAM%20DAPUR%20PILIH%20MENU.png)

3. **Alur Kurir - Pengiriman & OSRM Routing**
![Sequence Diagram Kurir](SEQUENCE%20DIAGRAM%20KURIR.png)

---

## 📡 Dokumentasi Full REST API (Postman / cURL Ready)
Golang Backend Server: `http://localhost:8080/api`

### 1. Authentikasi (`/auth`)
* `POST /auth/login`  
  *Payload:* `{"email": "admin@mbg.com", "password": "password"}`
* `POST /auth/register`  
  *Payload:* `{"name": "Budi", "email": "kurir@mbg.com", "password": "password", "role": "courier"}`

  ![Bukti API Register Berhasil](http___localhost_8080_api_auth_register%20-%20mbg_smart_logistics%2022_04_2026%2001_12_45.png)

### 2. OCR & Manajemen Stok Dapur (`/ocr`, `/inventory`, `/kitchens`)
* **`POST /ocr/scan-receipt`**  *(Uji Coba AI Vision!)*
  * Tipe Body: `multipart/form-data`
  * Key: `receipt_image` (Upload File Nota .png/.jpg). Fitur Mock-up akan jalan bila API limit!
* **`POST /ocr/confirm`**
  * *Payload:* Hasil JSON kembalian scan-receipt dikirm kemari untuk meng-insert ke Database.
* **`GET /inventory/{kitchen_id}`**
  * Tarik seluruh laporan sisa barang di dapur.
* **`GET /kitchens`** | **`POST /kitchens`**

### 3. Produksi Pangan (`/production`)
* **`GET /production/active`**  
  * Menampilkan data produksi hari ini yang statusnya 'cooking', 'ready', atau 'dispatched'.
* **`POST /production/start`** *(Uji Coba Analisis Masa Tahan AI!)*
  * *Payload (Raw JSON):*
  ```json
  {
      "kitchen_id": "uuid-dari-tabel-kitchen",
      "dish_name": "Sayur Lodeh Daging Sapi",
      "total_portions": 50,
      "cooked_at": "2026-04-22T08:00:00Z",
      "assignments": [
          {"school_id": "uuid-dari-tabel-school", "allocated_portions": 25}
      ]
  }
  ```
  *Output:* AI akan mengembalikan `shelf_life_minutes` untuk masakan tersebut berdasarkan komposisi kaldu/sayurannya.

  ![Bukti API Produksi AI Berhasil](http___localhost_8080_api_production_start%20-%20mbg_smart_logistics%2022_04_2026%2001_19_23.png)

### 4. Rute, Kurir & Traffic (`/routing`, `/couriers`, `/traffic`, `/schools`)
* **`GET /schools`** | **`POST /schools`** | **`PUT /schools/:id`** | **`DELETE /schools/:id`**
* **`GET /couriers`** | **`POST /couriers`**
* **`GET /traffic/stats`** *(Lihat Memory Traffic AI)*
* **`POST /routing/plan`** *(Uji Coba Otak OSRM!)*
  * *Payload:* `{"production_id": "uuid"}` 
  * Server Go akan mencarikan kurir nganggur, dan melukis peta ke Schools.
* **`PUT /routing/stops/{route_stop_id}/complete`**  
  * *Payload:* `{"notes": "Diterima kepala sekolah"}`
  * Endpoint aksi pencet "Selesai" oleh Kurir, akan memicu pembaruan jadwal sekolah setelahnya.

### 5. Tracking & ETA Realtime (`/monitoring`, `/agent`)
* **`POST /monitoring/location`**
  * *Payload:* `{"route_plan_id": "uuid", "lat": -7.95, "lng": 112.63}`
* **`GET /monitoring/track/{route_plan_id}`**  
  * Mengembalikan array polyline OSRM untuk digambar di peta.
* **`GET /monitoring/eta/{school_id}`**  
  * Endpoint polling (5 detik) untuk Guru melihat kapan masakan datang siang ini.
* **`POST /agent/analyze-schedule`**  
  * Mengecek jadwal kurir sebelum berangkat untuk *Backup Plan* (Sistem Agent Cerdas di Go).

---

## 🛠️ Panduan Eksekusi Setup
```powershell
# 1. Hidupkan Database + Engine OSRM
podman compose up -d

# 2. Hidupkan Core API Logic (Golang)
cd go-api
go run main.go

# 3. Hidupkan Admin Panel UI (Laravel)
cd laravel-core
php artisan serve
```
