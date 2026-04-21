<?php

namespace App\Http\Controllers;

use Illuminate\Http\Request;
use App\Services\OsrmService;
use App\Services\LocalAgentService;

class LogisticsAiController extends Controller
{
    protected $osrm;
    protected $localAgent;

    public function __construct(OsrmService $osrm, LocalAgentService $localAgent)
    {
        $this->osrm = $osrm;
        $this->localAgent = $localAgent;
    }

    public function index()
    {
        return view('logistics_ai');
    }

    public function analyze(Request $request)
    {
        $request->validate([
            'origin_lat' => 'required|numeric',
            'origin_lng' => 'required|numeric',
            'dest_lat' => 'required|numeric',
            'dest_lng' => 'required|numeric',
        ]);

        $origin = ['lat' => $request->origin_lat, 'lng' => $request->origin_lng];
        $dest = ['lat' => $request->dest_lat, 'lng' => $request->dest_lng];

        // 1. Tarik rute ke OSRM docker
        $routeData = $this->osrm->getRoute($origin, $dest);
        
        if (!$routeData || $routeData['code'] !== 'Ok') {
            return response()->json([
                'success' => false,
                'message' => 'Gagal terhubung ke engine OSRM lokal. Pastikan container OSRM berjalan di port 5000.'
            ], 500);
        }

        $distance = $routeData['routes'][0]['distance'];
        $duration = $routeData['routes'][0]['duration'];
        $geometry = $routeData['routes'][0]['geometry'];

        // 2. Kalkulasi Agent Heuristik Lokal secara mendalam tentang durasi dan Backup Plan
        $aiAnalysis = $this->localAgent->analyzeRoute($distance, $duration);

        return response()->json([
            'success' => true,
            'data' => [
                'distance_km' => round($distance / 1000, 2),
                'duration_min' => round($duration / 60, 1),
                'geometry' => $geometry,
                'ai_analysis' => $aiAnalysis
            ]
        ]);
    }
}
