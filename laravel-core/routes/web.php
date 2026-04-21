<?php

use Illuminate\Support\Facades\Route;
use App\Http\Controllers\LogisticsAiController;

Route::get('/', [LogisticsAiController::class, 'index']);
Route::post('/api/analyze-route', [LogisticsAiController::class, 'analyze']);