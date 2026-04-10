<?php

use Illuminate\Support\Facades\Route;
use App\Models\Lokasi;

Route::get('/', function () {
    
    $semua_lokasi = Lokasi::all();

    return view('daftar_lokasi', compact('semua_lokasi'));
});