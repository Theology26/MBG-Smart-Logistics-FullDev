<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Model;

class Rute extends Model
{
    protected $table = 'rute';

    protected $fillable = [
        'kurir_id',
        'tanggal',
        'total_jarak_km',
        'waktu_mulai_aktual',
        'waktu_selesai_aktual',
        'status_rute'
    ];

    public function kurir()
    {
        return $this->belongsTo(Pengguna::class, 'kurir_id');
    }

    public function pemberhentianRute()
    {
        return $this->hasMany(PemberhentianRute::class, 'rute_id');
    }
}