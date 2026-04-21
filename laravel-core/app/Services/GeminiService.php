<?php

namespace App\Services;

use Illuminate\Support\Facades\Http;

class GeminiService
{
    protected $apiKey;

    public function __construct()
    {
        $this->apiKey = env('GEMINI_API_KEY');
    }

    /**
     * Berikan analisa AI mengenai waktu dan rute pengiriman
     */
    public function analyzeRoute($distanceMeters, $durationSeconds)
    {
        if (!$this->apiKey) {
            return "Peringatan: API Key Gemini belum di-set di .env Laravel.";
        }

        $model = env('GEMINI_MODEL', 'gemini-1.5-flash');
        $url = "https://generativelanguage.googleapis.com/v1beta/models/{$model}:generateContent?key={$this->apiKey}";
        
        $distanceKm = round($distanceMeters / 1000, 1);
        $durationMin = round($durationSeconds / 60);

        $prompt = "Kamu adalah sistem pakar 'MBG Smart Logistics'. Saat ini terdapat rute pengiriman Makanan Bergizi Gratis sejauh {$distanceKm} km dengan estimasi waktu {$durationMin} menit pada suhu ruang. Berikan analisis singkat (maksimal 2 kalimat) tentang rute ini dan 1 saran praktis spesifik untuk kurir agar kualitas gizi dan suhu makanan tetap terjaga sampai ke sekolah (gunakan gaya bahasa profesional yang singkat).";

        try {
            $response = Http::withOptions(['verify' => false]) // Disable SSL check for local XAMPP
                ->timeout(10)
                ->post($url, [
                'contents' => [
                    ['parts' => [['text' => $prompt]]]
                ]
            ]);

            if ($response->successful()) {
                $data = $response->json();
                return $data['candidates'][0]['content']['parts'][0]['text'] ?? 'Analisis sukses namun kosong dari AI.';
            } else {
                // If it fails with 429 quota or anything else, log safely but don't expose URL to front-end
                \Log::error("Gemini API Error: " . $response->status());
            }
        } catch (\Exception $e) {
            \Log::error("Gemini cURL Error: " . $e->getMessage());
        }

        // FALLBACK MODE for Laravel Interface (keeps app functional if quota/SSL fails)
        return "⚠️ (Fallback Mode AI) Jarak {$distanceKm} km dan estimasi {$durationMin} menit. Rute ini tergolong aman, pastikan container makanan selalu tertutup rapat untuk menjaga suhu tetap hangat selama perjalanan.";
    }
}
