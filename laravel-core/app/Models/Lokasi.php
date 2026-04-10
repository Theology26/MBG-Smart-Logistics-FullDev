<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Model;

class Lokasi extends Model
{
    protected $table = 'lokasi';


    protected $fillable = [
        'nama_lokasi',
        'tipe_lokasi',
        'latitude',
        'longitude',
        'kebutuhan_porsi',
        'kontak_pic',
        'batas_waktu',
        'waktu_layanan_menit'
    ];

    public function pemberhentianRute()
    {
        return $this->hasMany(PemberhentianRute::class, 'lokasi_id');
    }
}