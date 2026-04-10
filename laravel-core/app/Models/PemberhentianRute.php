<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Model;

class PemberhentianRute extends Model
{
    protected $table = 'pemberhentian_rute';


    protected $fillable = [
        'rute_id',
        'lokasi_id',
        'urutan_berhenti',
        'waktu_tiba_aktual',
        'porsi_turun',
        'porsi_naik',
        'bukti_foto',
        'catatan',
        'status_perhentian'
    ];

    public function rute()
    {
        return $this->belongsTo(Rute::class, 'rute_id');
    }

    public function lokasi()
    {
        return $this->belongsTo(Lokasi::class, 'lokasi_id');
    }
}