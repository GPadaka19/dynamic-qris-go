# Dynamic QRIS Converter

Mengubah QRIS statis (`data/qris.jpg`) menjadi QRIS dinamis dengan nominal tertentu, lalu
(opsional) mengirimkannya lewat WhatsApp melalui
[go-whatsapp-web-multidevice](https://github.com/aldinokemal/go-whatsapp-web-multidevice) (GOWA).

## Menjalankan

```sh
cp .env.example .env      # isi WA_API_URL, dst.
cp /path/ke/qris.jpg data/qris.jpg
make run                  # atau: go run .
```

Buka <http://localhost:4009>. Dengan Docker: `docker compose up -d --build`
(butuh network eksternal `qris-net`).

## Konfigurasi (env)

| Variabel          | Default         | Keterangan                                          |
|-------------------|-----------------|-----------------------------------------------------|
| `PORT`            | `4009`          | Port server                                         |
| `WA_API_URL`      | –               | Base URL GOWA tanpa trailing slash                  |
| `WA_USER`/`WA_PASSWORD` | –         | Basic Auth GOWA (kosongkan jika tidak dipakai)      |
| `WA_DEVICE_ID`    | –               | Dikirim sebagai header `X-Device-Id` (multi-device) |
| `APP_PASSWORD`    | –               | Password halaman login. Kosong = login dimatikan    |
| `SESSION_SECRET`  | acak per proses | Kunci penanda cookie sesi; isi agar sesi bertahan setelah restart |
| `QRIS_IMAGE_PATH` | `data/qris.jpg` | Lokasi gambar QRIS statis                           |
| `WEB_DIR`         | `web`           | Lokasi aset frontend                                |

## API

| Method & path        | Fungsi                                                    |
|----------------------|-----------------------------------------------------------|
| `GET /api/qris`      | Teks QRIS statis hasil decode `data/qris.jpg`             |
| `POST /api/convert`  | `{qris, amount, remarks}` → QRIS dinamis + gambar QR      |
| `GET /api/contacts`  | Proxy kontak GOWA (di-cache 5 menit)                      |
| `POST /api/send`     | `{phone, caption, image}` → kirim gambar via GOWA         |
| `GET /healthz`       | Health check                                              |

Error selalu berbentuk `{"success": false, "error": "...", "message": "..."}`.

## Keamanan

Set `APP_PASSWORD` untuk mengaktifkan halaman login (`/login`). Semua halaman dan API
(kecuali `/login`, `/healthz`, dan aset CSS) memerlukan sesi. Sesi berupa cookie
`HttpOnly`, `SameSite=Strict` (dan `Secure` di belakang HTTPS) yang berlaku 24 jam; mengganti
password membatalkan semua sesi. Percobaan login gagal dibatasi 5 kali per menit per alamat IP.

Jika `APP_PASSWORD` kosong, aplikasi terbuka tanpa login dan `/api/send` dapat mengirim
WhatsApp dari akun yang tertaut ke siapa pun yang bisa mengaksesnya. Jangan ekspos langsung
ke internet tanpa password atau reverse proxy ber-auth (di compose ini: Traefik + Authelia).
Gunakan HTTPS, karena password dikirim lewat form.

## Pengembangan

```sh
make test   # go test -race ./...
make vet
make fmt
```
