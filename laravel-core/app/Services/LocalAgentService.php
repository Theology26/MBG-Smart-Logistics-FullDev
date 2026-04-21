<?php

namespace App\Services;

class LocalAgentService
{
    /**
     * Agent Lokal (Heuristics) untuk menggantikan external API.
     * Menganalisis potensi macet berdasarkan jarak dan waktu OSRM, lalu membuat Backup Plan.
     */
    public function analyzeRoute($distanceMeters, $durationSeconds)
    {
        $distanceKm = round($distanceMeters / 1000, 1);
        $durationMin = round($durationSeconds / 60, 1);
        
        // Asumsi kecepatan normal di Kota Malang tanpa macet adalah sekitar 30 km/jam
        $idealMinutes = ($distanceKm / 30) * 60;
        
        // Hindari division by zero preventif
        if ($idealMinutes == 0) $idealMinutes = 1;

        $congestionRatio = $durationMin / $idealMinutes;

        $analysis = "Agent memproses rute sepanjang {$distanceKm} km (Waktu OSRM: {$durationMin} mnt). ";
        
        if ($congestionRatio >= 1.5) {
            $analysis .= "⚠️ DETEKSI MACET PARAH! Rute lebih lambat 50% dari waktu normal. BACKUP PLAN: Aktifkan re-routing otomatis ke jalur sekunder, dan instruksikan dapur untuk memajukan jadwal keberangkatan kurir sebanyak " . round($durationMin - $idealMinutes) . " menit agar makanan tiba sebelum jam istirahat.";
        } elseif ($congestionRatio >= 1.2) {
            $analysis .= "🚦 LALU LINTAS PADAT. Waktu tempuh sedikit molor. BACKUP PLAN: Pastikan Thermal Box tertutup rapat karena porsi akan diam di jalan lebih lama. Prioritaskan sekolah ini di urutan pertama (Visual Re-ordering).";
        } else {
            $analysis .= "✅ LALU LINTAS LANCAR. Waktu dan jarak seimbang. Rencana utama (Primary Plan) dapat dijalankan tanpa modifikasi, daya tahan gizi masakan dijamin optimal saat tiba.";
        }

        return $analysis;
    }
}
