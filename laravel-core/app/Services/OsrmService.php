<?php

namespace App\Services;

use Illuminate\Support\Facades\Http;

class OsrmService
{
    protected $baseUrl;

    public function __construct()
    {
        $this->baseUrl = env('OSRM_BASE_URL', 'http://localhost:5000');
    }

    /**
     * Hitung rute menggunakan docker OSRM lokal
     */
    public function getRoute($origin, $destination)
    {
        $coordinates = "{$origin['lng']},{$origin['lat']};{$destination['lng']},{$destination['lat']}";
        $url = "{$this->baseUrl}/route/v1/driving/{$coordinates}?overview=full&geometries=geojson";
        
        try {
            $response = Http::timeout(5)->get($url);
            if ($response->successful()) {
                return $response->json();
            }
        } catch (\Exception $e) {
            // Log error
        }
        
        return null;
    }
}
