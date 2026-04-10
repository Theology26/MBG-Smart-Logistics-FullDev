<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Model;

class Pengguna extends Model
{

    protected $table = 'pengguna';

    protected $fillable = [
        'nama',
        'email',
        'kata_sandi',
        'peran',
        'tipe_kendaraan',
        'kapasitas_kendaraan'
    ];


    public function rute()
    {
        return $this->hasMany(Rute::class, 'kurir_id');
    }
}