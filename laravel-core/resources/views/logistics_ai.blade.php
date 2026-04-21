<!DOCTYPE html>
<html lang="id">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>MBG Laravel OSRM + AI Engine</title>
    <link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700;800&display=swap">
    <link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css" />
    <script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js"></script>
    <script defer src="https://cdn.jsdelivr.net/npm/alpinejs@3.x.x/dist/cdn.min.js"></script>
    <meta name="csrf-token" content="{{ csrf_token() }}">
    <style>
        :root {
            --bg-body: #0b1120;
            --bg-surface: #0f172a;
            --blue-400: #60a5fa;
            --blue-500: #3b82f6;
            --blue-600: #2563eb;
            --emerald-400: #34d399;
            --text-primary: #f1f5f9;
            --text-secondary: #94a3b8;
            --border-glass: rgba(148,163,184,0.12);
        }
        * { box-sizing: border-box; }
        body { font-family: 'Inter', sans-serif; background: var(--bg-body); color: var(--text-primary); margin: 0; padding: 30px 20px; }
        .container { max-width: 1200px; margin: 0 auto; display: grid; grid-template-columns: 1fr 400px; gap: 24px; }
        @media (max-width: 900px) { .container { grid-template-columns: 1fr; } }
        .card { background: var(--bg-surface); border: 1px solid var(--border-glass); border-radius: 16px; padding: 24px; box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.1), 0 2px 4px -1px rgba(0, 0, 0, 0.06); }
        h1 { margin-top: 0; font-size: 1.35rem; color: var(--text-primary); display: flex; align-items: center; gap: 10px; margin-bottom: 8px; }
        p { color: var(--text-secondary); font-size: 0.9rem; line-height: 1.5; margin-bottom: 20px; }
        #map { height: 500px; width: 100%; border-radius: 12px; z-index: 1; border: 1px solid var(--border-glass); }
        .btn { display: flex; align-items: center; justify-content: center; gap: 8px; width: 100%; padding: 12px; background: var(--blue-600); color: #fff; border: none; border-radius: 8px; font-weight: 600; cursor: pointer; transition: 0.2s; font-size: 0.9rem; }
        .btn:hover:not(:disabled) { background: #1d4ed8; transform: translateY(-1px); }
        .btn:disabled { opacity: 0.5; cursor: not-allowed; }
        .loader { width: 16px; height: 16px; border: 2px solid rgba(255,255,255,0.3); border-bottom-color: #fff; border-radius: 50%; animation: spin 1s linear infinite; }
        @keyframes spin { 100% { transform: rotate(360deg); } }
        
        .coord-box { background: rgba(255,255,255,0.03); border: 1px solid var(--border-glass); padding: 12px; border-radius: 8px; margin-bottom: 12px; }
        .coord-label { font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; color: var(--text-secondary); margin-bottom: 4px; }
        .coord-value { font-weight: 600; font-size: 0.9rem; font-family: monospace; }
        
        .result-box { margin-top: auto; background: rgba(59,130,246,0.1); border: 1px solid rgba(59,130,246,0.25); padding: 20px; border-radius: 12px; }
        .metrics { display: flex; gap: 12px; margin-bottom: 16px; }
        .metric-badge { background: var(--blue-500); color: white; padding: 6px 12px; border-radius: 6px; font-size: 0.85rem; font-weight: 700; display: flex; align-items: center; gap: 6px;}
        .ai-label { font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; color: var(--blue-400); font-weight: 700; margin-bottom: 8px; display: flex; align-items: center; gap: 6px; }
        .ai-text { font-size: 0.95rem; line-height: 1.6; color: var(--text-primary); }
    </style>
</head>
<body>
    <div class="container" x-data="laravelLogisticsAgent()">
        
        <div class="card">
            <h1>
                <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="color: var(--blue-400)"><path d="M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5"/></svg>
                Laravel Logistics Engine (OSRM + AI)
            </h1>
            <p>Simulasi kalkulasi rute CVRPTW. Klik 2 titik pada peta di bawah (1. Dapur, 2. Sekolah Tujuan) untuk test integrasi langsung ke Engine OSRM lokal dan mendapatkan analisis Backup Plan dari agen rute cerdas (tanpa API eksternal).</p>
            <div id="map"></div>
        </div>

        <div class="card" style="display: flex; flex-direction: column;">
            <h2 style="margin-top: 0; font-size: 1.1rem; border-bottom: 1px solid var(--border-glass); padding-bottom: 12px; margin-bottom: 20px;">
                Panel Analisis Rute
            </h2>
            
            <div class="coord-box">
                <div class="coord-label">📍 Titik A (Dapur Produksi)</div>
                <div class="coord-value" x-text="origin ? origin.lat.toFixed(5) + ', ' + origin.lng.toFixed(5) : 'Belum ditentukan'"></div>
            </div>
            
            <div class="coord-box">
                <div class="coord-label">🏫 Titik B (Sekolah Tujuan)</div>
                <div class="coord-value" x-text="dest ? dest.lat.toFixed(5) + ', ' + dest.lng.toFixed(5) : 'Belum ditentukan'"></div>
            </div>

            <button class="btn" style="margin-top: 12px;" @click="analyzeRoute" :disabled="!origin || !dest || loading">
                <template x-if="loading"><div class="loader"></div></template>
                <svg x-show="!loading" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
                <span x-text="loading ? 'Memproses OSRM & Agent...' : 'Jalankan Analisis Tercepat'"></span>
            </button>

            <button class="btn" style="background: transparent; border: 1px solid var(--border-glass); margin-top: 12px; color: var(--text-secondary)" @click="resetMap" :disabled="loading">
                Reset Titik Peta
            </button>

            <!-- Hasil Analisis -->
            <div style="flex-grow: 1;"></div>
            
            <template x-if="result">
                <div class="result-box">
                    <div class="metrics">
                        <div class="metric-badge">
                            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 10c0 7-9 13-9 13s-9-6-9-13a9 9 0 0118 0z"/><circle cx="12" cy="10" r="3"/></svg>
                            <span x-text="result.distance_km + ' km'"></span>
                        </div>
                        <div class="metric-badge" style="background: var(--emerald-400);">
                            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 15 15"/></svg>
                            <span x-text="result.duration_min + ' menit'"></span>
                        </div>
                    </div>
                    
                    <div class="ai-label">
                        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2v20M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6"/></svg>
                        Keputusan Local AI Agent & Backup Plan:
                    </div>
                    <div class="ai-text" x-text="result.ai_analysis"></div>
                </div>
            </template>
        </div>

    </div>

    <script>
        function laravelLogisticsAgent() {
            return {
                map: null,
                origin: null,
                dest: null,
                markerA: null,
                markerB: null,
                routeLayer: null,
                loading: false,
                result: null,

                init() {
                    // Inisialisasi peta di Malang
                    this.map = L.map('map').setView([-7.9500, 112.6200], 13);
                    L.tileLayer('https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png', {
                        attribution: '© CartoDB'
                    }).addTo(this.map);

                    // Custom icons
                    const iconDapur = L.divIcon({ html: '<div style="width:30px;height:30px;background:#3b82f6;border-radius:50%;color:white;display:flex;align-items:center;justify-content:center;font-weight:bold;border:3px solid rgba(255,255,255,0.3)">A</div>', className: '', iconSize: [30, 30], iconAnchor: [15,15] });
                    const iconSekolah = L.divIcon({ html: '<div style="width:30px;height:30px;background:#34d399;border-radius:50%;color:black;display:flex;align-items:center;justify-content:center;font-weight:bold;border:3px solid rgba(255,255,255,0.3)">B</div>', className: '', iconSize: [30, 30], iconAnchor: [15,15] });

                    this.map.on('click', (e) => {
                        if (!this.origin) {
                            this.origin = e.latlng;
                            this.markerA = L.marker(this.origin, {icon: iconDapur}).addTo(this.map).bindPopup('Titik A: Dapur').openPopup();
                        } else if (!this.dest) {
                            this.dest = e.latlng;
                            this.markerB = L.marker(this.dest, {icon: iconSekolah}).addTo(this.map).bindPopup('Titik B: Sekolah').openPopup();
                            // Optional auto trigger
                        }
                    });
                },

                resetMap() {
                    this.origin = null;
                    this.dest = null;
                    this.result = null;
                    if (this.markerA) this.map.removeLayer(this.markerA);
                    if (this.markerB) this.map.removeLayer(this.markerB);
                    if (this.routeLayer) this.map.removeLayer(this.routeLayer);
                },

                async analyzeRoute() {
                    if (!this.origin || !this.dest) return;
                    this.loading = true;
                    this.result = null;
                    if (this.routeLayer) this.map.removeLayer(this.routeLayer);

                    try {
                        const token = document.querySelector('meta[name="csrf-token"]').getAttribute('content');
                        const res = await fetch('/api/analyze-route', {
                            method: 'POST',
                            headers: {
                                'Content-Type': 'application/json',
                                'X-CSRF-TOKEN': token
                            },
                            body: JSON.stringify({
                                origin_lat: this.origin.lat, origin_lng: this.origin.lng,
                                dest_lat: this.dest.lat, dest_lng: this.dest.lng
                            })
                        });

                        const data = await res.json();
                        
                        if (data.success) {
                            this.result = data.data;
                            
                            // Visualize standard GeoJSON received from OSRM
                            this.routeLayer = L.geoJSON(data.data.geometry, {
                                style: { color: '#60a5fa', weight: 4, opacity: 0.9, dashArray: '8, 8' }
                            }).addTo(this.map);
                            
                            this.map.fitBounds(this.routeLayer.getBounds(), { padding: [50, 50] });
                        } else {
                            alert('Gagal: ' + data.message);
                        }
                    } catch (e) {
                        alert('Error koneksi, pastikan container OSRM lokal berjalan pada port 5000.');
                    } finally {
                        this.loading = false;
                    }
                }
            };
        }
    </script>
</body>
</html>
