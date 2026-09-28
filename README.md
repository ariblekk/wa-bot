# GoWA OpenAI Bridge

Backend Go + Gin yang menerima webhook GoWA, mengirim pesan teks ke OpenAI-compatible Chat Completions API, lalu mengirim jawabannya kembali ke WhatsApp melalui GoWA.

## Alur

```text
GoWA webhook -> Gin -> OpenAI-compatible API -> GoWA /send/message
```

## Menjalankan

1. Salin `.env.example` menjadi `.env` dan isi nilainya.
2. Pastikan GoWA mengirim webhook event `message` ke:

```text
http://HOST:8080/webhook/gowa
```

3. Atur secret yang sama pada GoWA (`WHATSAPP_WEBHOOK_SECRET`) dan aplikasi (`WEBHOOK_SECRET`).
4. Jalankan:

```bash
go mod tidy
go run ./cmd/server
```

Health check tersedia di `GET /health`.

## System prompt

Prompt panjang diletakkan di file terpisah agar mudah diedit:

```env
OPENAI_SYSTEM_PROMPT_FILE=./prompts/system.txt
```

File dibaca **sekali saat startup**, sehingga tidak ada dampak ke kecepatan balasan. `OPENAI_SYSTEM_PROMPT` menjadi fallback kalau variabel file tidak diisi.

`prompts/system.txt` berisi aturan agar output tidak memakai Markdown yang tidak didukung WhatsApp (code block, `**tebal**`, tabel, heading, link `[teks](url)`).

Backend juga menyisipkan waktu server aktual ke setiap request AI. Default timezone adalah `Asia/Jakarta` dan bisa diubah melalui `APP_TIMEZONE`. Jika user bertanya "sekarang jam berapa", AI menjawab berdasarkan waktu server tersebut, bukan mengatakan tidak bisa melihat waktu realtime.

## Konteks percakapan

Memory percakapan disimpan persisten di PostgreSQL dan dipisahkan berdasarkan `chat_id`, sehingga bot bisa memahami pesan lanjutan seperti "yang tadi", "buat lebih singkat", atau "kirim ulang" tanpa mencampur percakapan pengguna lain.

```env
CHAT_MEMORY_ENABLED=true
CHAT_MEMORY_MAX_MESSAGES=20
CHAT_MEMORY_TTL_HOURS=24
POSTGRES_DSN=postgres://user:password@127.0.0.1:5432/wa_chatbot?sslmode=disable
POSTGRES_MAX_CONNS=10
POSTGRES_MIN_CONNS=2
```

Saat startup aplikasi melakukan ping PostgreSQL dan membuat tabel/index `conversation_messages` bila belum ada. Pesan downloader MeTube tidak dimasukkan ke memory AI; hanya percakapan yang benar-benar dikirim ke model yang disimpan. History dibatasi agar request tidak terus membesar dan menghabiskan token. Pesan dari `chat_id` yang sama diproses berurutan agar urutan konteks tetap konsisten, sementara chat berbeda tetap bisa diproses paralel. Jika `CHAT_MEMORY_ENABLED=false`, PostgreSQL tidak diperlukan dan memory percakapan dinonaktifkan.

## Konfigurasi OpenAI custom

Aplikasi menggunakan format OpenAI-compatible `POST /chat/completions`. Untuk proxy/provider lain:

```env
OPENAI_BASE_URL=https://provider.example/v1
OPENAI_CHAT_COMPLETIONS_PATH=/chat/completions
```

Jika URL lengkap diperlukan:

```env
OPENAI_CHAT_COMPLETIONS_URL=https://provider.example/custom/generate
```

Endpoint custom tetap harus menerima request Chat Completions dan mengembalikan response dengan bentuk `choices[0].message.content`.

## Konfigurasi GoWA

Untuk GoWA tanpa auth:

```env
GOWA_BASE_URL=http://localhost:3000
GOWA_DEVICE_ID=628123456789@s.whatsapp.net
```

Untuk GoWA Basic Auth:

```env
GOWA_BASIC_AUTH=admin:password
```

GoWA versi multi-device menggunakan `X-Device-Id`. Jika `GOWA_DEVICE_ID` kosong, aplikasi memakai `device_id` dari webhook.

## Signature webhook

GoWA menandatangani raw request body menggunakan HMAC-SHA256 pada header:

```text
X-Hub-Signature-256: sha256=<hex digest>
```

Jika `WEBHOOK_SECRET` kosong, verifikasi signature dilewati. Jangan mengosongkannya di production.

## Keamanan chat grup

Secara default, AI **tidak memproses chat grup**. Pesan grup ditolak sebelum read receipt, typing indicator, panggilan OpenAI, atau pengiriman balasan.

```env
ALLOW_GROUP_MESSAGES=false
```

Deteksi grup menggunakan `payload.is_group` dan suffix JID `@g.us`. Jangan ubah ke `true` kecuali memang ingin AI aktif di grup.

Di GoWA, batasi webhook ke event pesan saja:

```env
WHATSAPP_WEBHOOK_EVENTS=message
WHATSAPP_WEBHOOK_IGNORE_JIDS=@g.us
```

`WHATSAPP_WEBHOOK_IGNORE_JIDS=@g.us` menambah lapisan keamanan agar GoWA tidak meneruskan event dari grup.

## Integrasi MeTube (kirim video/lagu dari link)

Kalau ada instance MeTube ([alexta69/metube](https://github.com/alexta69/metube)), bot bisa mengirim file video atau audio ke WhatsApp saat pengirim mengirim link.

```env
METUBE_ENABLED=true
METUBE_BASE_URL=https://metube.example.com
METUBE_VIDEO_QUALITY=720
METUBE_AUDIO_FORMAT=mp3
```

Alurnya:

```text
Link di pesan -> POST MeTube /add -> poll /history sampai finished
              -> GoWA /send/video atau /send/audio dengan file_url
```

File **tidak disimpan** di project ini. GoWA mengunduh langsung dari URL MeTube, jadi tidak ada transfer file besar lewat bot maupun pemakaian disk. Syaratnya: URL file MeTube harus bisa dijangkau dari host GoWA, karena pengunduhan dilakukan di sisi GoWA.

Cara pakai dari WhatsApp:

```text
https://youtu.be/xxxx                         video 720p
https://www.tiktok.com/@user/video/xxxx      video TikTok
https://www.instagram.com/reel/xxxx/         video/reel Instagram
tolong download musiknya https://...         audio mp3
https://soundcloud.com/artist/track           audio mp3 otomatis
kirim vn https://...                          voice note
```

Kalau pengirim hanya mengirim link, atau hanya menambah kata-kata pendek seperti "tolong download", bot tidak mengirim balasan teks tambahan karena permintaannya sudah jelas. Kalau link disertai pertanyaan sungguhan, file dan jawaban teks keduanya dikirim.

Host yang diterima diatur lewat `METUBE_ALLOWED_HOSTS` (pisahkan dengan koma). Konfigurasi aktif memakai `*`, sehingga semua situs yang didukung MeTube/yt-dlp dapat dicoba. Host privat, localhost, dan alamat loopback tetap selalu ditolak. Kalau ingin membatasi situs, isi variabel ini dengan daftar domain tertentu.

Situs audio-only seperti SoundCloud, Bandcamp, dan Mixcloud otomatis masuk jalur audio/MP3. Kalau user menulis `video`, aplikasi tetap mencoba jalur video dan MeTube akan menentukan apakah format video tersedia.

### Bot tidak mengiklankan fitur ini

`prompts/system.txt` berisi aturan bahwa model tidak boleh menyebutkan atau menjanjikan kemampuan mengunduh file mana pun. Kalau pengguna bertanya "kamu bisa apa", jawabannya hanya apa yang bisa dibantu sebagai asisten. Ini memisahkan perilaku nyata dari yang diklaim ke pengguna.

### Catatan penting soal link YouTube

MeTube memakai id entri yt-dlp sebagai `id`, dan id itu sama untuk video dan audio dari satu video. Pencocokan entri antrean karena itu memakai kombinasi `id` + `timestamp` + `download_type`, bukan `id` saja. Tanpa ini, permintaan audio untuk link yang videonya sedang diunduh bisa mengembalikan file video.

## Konfigurasi lengkap MeTube

| Variabel | Bawaan | Keterangan |
| --- | --- | --- |
| `METUBE_ENABLED` | `true` | Fitur hanya aktif bila `METUBE_BASE_URL` diisi |
| `METUBE_BASE_URL` | kosong | URL instance MeTube, wajib |
| `METUBE_API_KEY` | kosong | Dikirim sebagai header `X-API-Key` bila instance dilindungi |
| `METUBE_ADD_PATH` | `/add` | Endpoint enqueue |
| `METUBE_HISTORY_PATH` | `/history` | Endpoint status antrean |
| `METUBE_VIDEO_FILE_PATH` | `/download` | Static path file video |
| `METUBE_AUDIO_FILE_PATH` | `/audio_download` | Static path file audio |
| `METUBE_VIDEO_QUALITY` | `720` | `best`, `worst`, `2160`, `1440`, `1080`, `720`, `480`, `360`, `240` |
| `METUBE_VIDEO_FORMAT` | `mp4` | `any`, `mp4`, `ios` |
| `METUBE_VIDEO_CODEC` | `h264` | `auto`, `h264`, `h265`, `av1`, `vp9` |
| `METUBE_AUDIO_FORMAT` | `mp3` | `auto`, `m4a`, `mp3`, `opus`, `wav`, `flac` |
| `METUBE_ALLOWED_HOSTS` | daftar situs umum | Host yang boleh diproses, `*` untuk semua situs yt-dlp |
| `METUBE_AUDIO_ONLY_HOSTS` | daftar situs audio | Host yang default ke MP3 saat link dikirim tanpa kata `video` |
| `METUBE_POLL_SECONDS` | `3` | Jeda antar poll `/history` |
| `METUBE_WAIT_TIMEOUT_SECONDS` | `600` | Batas menunggu satu download selesai |
| `METUBE_REQUEST_TIMEOUT_SECONDS` | `20` | Timeout untuk `/history` |
| `METUBE_ADD_TIMEOUT_SECONDS` | `120` | Timeout untuk `/add`, lebih lama karena menunggu ekstraksi metadata yt-dlp |

Nilai yang tidak dikenal divalidasi saat startup, jadi salah konfigurasi ketahuan sebelum ada pesan masuk, bukan saat pengguna mengirim link.

## Test

```bash
go test ./...
```

Test integrasi MeTube memakai server tiruan. Untuk mencoba instance sungguhan:

```bash
METUBE_LIVE_TEST=1 METUBE_BASE_URL=https://metube.example.com go test ./internal/service/ -run TestLiveMeTube -v
```

## Catatan perilaku

- Hanya event `message` yang diproses.
- Pesan dari bot sendiri (`is_from_me=true`) diabaikan untuk mencegah loop.
- Pesan grup diabaikan sebelum ada efek samping ke WhatsApp atau OpenAI.
- Pesan kosong/media tanpa caption diabaikan.
- Pesan duplikat berdasarkan `device_id + message id` diabaikan selama proses hidup aplikasi.
- Webhook langsung dibalas `202`; pemanggilan OpenAI dan pengiriman balasan berjalan asynchronous.
- Storage deduplikasi masih in-memory. Untuk multi-instance/production berskala besar, ganti dengan Redis atau database.
