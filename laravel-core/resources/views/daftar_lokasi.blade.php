<!DOCTYPE html>
<html lang="id">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>MBG Smart Logistics - Lokasi</title>
    <style>
        body { font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; padding: 40px; background: #f4f7f6; }
        .card { background: white; padding: 20px; border-radius: 10px; box-shadow: 0 4px 6px rgba(0,0,0,0.1); }
        table { width: 100%; border-collapse: collapse; margin-top: 20px; }
        th, td { text-align: left; padding: 12px; border-bottom: 1px solid #ddd; }
        th { background-color: #4A90E2; color: white; }
        tr:hover { background-color: #f1f1f1; }
        .badge { padding: 5px 10px; border-radius: 5px; font-size: 12px; font-weight: bold; }
        .sppg { background: #e3f2fd; color: #1976d2; }
        .sekolah { background: #f1f8e9; color: #388e3c; }
    </style>
</head>
<body>

    <div class="card">
        <h2>📍 Daftar Lokasi MBG Smart Logistics</h2>
        <p>Data ini ditarik langsung dari database yang dikelola oleh <strong>Golang Engine</strong>.</p>

        <table>
            <thead>
                <tr>
                    <th>Nama Lokasi</th>
                    <th>Tipe</th>
                    <th>Kebutuhan Porsi</th>
                    <th>PIC</th>
                </tr>
            </thead>
            <tbody>
                @forelse($semua_lokasi as $lokasi)
                    <tr>
                        <td>{{ $lokasi->nama_lokasi }}</td>
                        <td>
                            <span class="badge {{ strtolower($lokasi->tipe_lokasi) }}">
                                {{ $lokasi->tipe_lokasi }}
                            </span>
                        </td>
                        <td>{{ $lokasi->kebutuhan_porsi ?? 0 }} porsi</td>
                        <td>{{ $lokasi->kontak_pic ?? '-' }}</td>
                    </tr>
                @empty
                    <tr>
                        <td colspan="4" style="text-align: center;">Belum ada data. Coba input lewat Go API ya!</td>
                    </tr>
                @endforelse
            </tbody>
        </table>
    </div>

</body>
</html>