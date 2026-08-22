# HANDOFF — JournalFlow, lanjutan untuk Claude Code

> **Nama app: "Selah"** (wordmark: `selah.` — lowercase, titik aksen warna gold).
> Font brand: **Plus Jakarta Sans** (weight 800 untuk wordmark), sudah di-import
> lewat Google Fonts di `web/static/css/style.css`. Icon mark: monogram huruf
> "s" gold di atas squircle navy (`rx: 18px`), lihat `.brand-mark-icon` di CSS
> dan pemakaiannya di `login.html`. Kalau mau bikin app icon PNG asli untuk
> PWA/homescreen (bukan cuma di dalam halaman web), re-create monogram ini di
> Figma/design tool dengan warna navy `#2C4468` + gold `#F0A93B`, lalu export
> ke ukuran-ukuran standar (192x192, 512x512, dst) dan tambahkan manifest.json
> untuk PWA kalau app ini mau bisa di-"Add to Home Screen".

Project ini sudah di-scaffold dan **berhasil di-build** (`go build ./...` clean,
`go vet` clean) di sandbox terpisah. Semua kode di bawah ini sudah ada dan
jalan secara fungsional — tugas Claude Code adalah **melengkapi, mengetes di
lingkungan nyata (docker + postgres beneran), dan menghaluskan**, bukan
membangun dari nol.

## Yang SUDAH ada dan berfungsi

- **Struktur Go project** lengkap: `cmd/server`, `internal/{config,db,session,
  models,handlers,middleware,ai}`
- **Schema Postgres** (`migrations/0001_init.sql`, di-embed ke binary lewat
  `internal/db/schema_embed.sql` — dua file ini HARUS tetap sinkron kalau
  nambah tabel/kolom): tabel `users`, `journal_entries`, `journal_messages`,
  `sessions`
- **Auth**: login/logout dengan bcrypt + session cookie yang disimpan di
  tabel `sessions` (bukan JWT — sengaja simpel karena ini app single-user).
  Ada endpoint `/register` tanpa link di nav untuk bikin akun pertama kali;
  hapus/nonaktifkan setelah akun dibuat.
- **Flow journal lengkap** sesuai diagram yang saya kasih ke Claude
  sebelumnya:
  1. `GET /journal/new` — form input ayat
  2. `POST /journal` — simpan entry baru + panggil `AI.VerseBackground()`
     untuk cari konteks historis/bahasa asli ayat
  3. `GET /journal/{id}` — halaman utama: tampilkan ayat, background AI,
     thread diskusi, dan form-form berikutnya
  4. `POST /journal/{id}/reflect` — simpan refleksi user
  5. `POST /journal/{id}/discuss` — loop tanya-jawab dengan AI (kirim seluruh
     history percakapan supaya AI punya konteks penuh)
  6. `POST /journal/{id}/complete` — simpan langkah praktis, tandai selesai
- **Dashboard** — list journal bulan berjalan + FAB tombol tambah
- **`/journal`** — list semua journal (untuk bottom nav)
- **Bottom nav**: Home / Journal / Logout, sesuai referensi desain
- **Klien AI** (`internal/ai/claude.go`) — HTTP client tipis ke endpoint
  `/v1/messages` gaya Anthropic. `ANTHROPIC_BASE_URL` bisa diarahkan ke server
  Claude pribadi Anda, bukan cuma `api.anthropic.com`.
- **Docker**: `Dockerfile` (multi-stage, alpine) + `docker-compose.yml`
  (app + postgres, dengan healthcheck) + `.env.example`
- **CSS** mobile-first bertema cream/navy/gold meniru referensi desain yang
  dikasih ke Claude (kartu putih, FAB kuning, bottom nav)

## Yang BELUM dikerjakan / perlu perhatian Claude Code

1. **Belum pernah dites terhadap Postgres beneran + Claude API beneran.**
   Sandbox tempat saya scaffold tidak punya akses jaringan ke Postgres atau
   Anthropic API, jadi build cuma diverifikasi secara statis (compile +
   vet). **Prioritas #1**: `docker compose up --build`, bikin akun via
   `/register`, coba seluruh flow end-to-end, perbaiki apa pun yang meleset.

2. **`go.sum` dibuat di sandbox dengan `GOPROXY=direct` dan sedikit trik**
   (ada `replace golang.org/x/crypto => github.com/golang/crypto` di
   `go.mod` karena sandbox saya tidak bisa akses `proxy.golang.org` /
   `golang.org`). Di server Anda yang aksesnya penuh, jalankan `go mod tidy`
   ulang — kemungkinan besar replace directive itu bisa dihapus dan versi
   dependency bisa di-bump ke yang terbaru (`go-chi/chi`, `lib/pq`,
   `joho/godotenv`, `golang.org/x/crypto`).

3. **Driver DB pakai `lib/pq`, bukan `pgx`.** Saya switch dari pgx ke lib/pq
   di tengah jalan karena masalah resolusi dependency di sandbox (bukan
   masalah teknis pgx itu sendiri). `lib/pq` sudah cukup untuk kebutuhan
   app ini, tapi kalau Anda mau performa/fitur lebih (native pgx types,
   connection pooling lebih baik), pertimbangkan migrasi ke `pgx/v5` +
   `pgxpool` — semua query pakai `database/sql`-style jadi migrasinya
   lumayan mekanis (ganti `QueryContext`/`ExecContext`/`QueryRowContext`
   jadi versi pgx, dan `*sql.DB` jadi `*pgxpool.Pool` di semua file
   `internal/handlers/*.go` dan `internal/session/session.go`).

4. **`SESSION_SECRET` saat ini tidak benar-benar dipakai** untuk apa pun
   (session disimpan di DB, cookie cuma bawa random ID, bukan token yang
   di-sign). Env var-nya ada untuk kebutuhan masa depan. Boleh dihapus dari
   `.env` requirement kalau tidak mau dipertahankan, atau dipakai betulan
   untuk signed cookie via `gorilla/securecookie` kalau mau session tanpa
   query DB tiap request.

5. **Belum ada halaman/handler untuk:**
   - Edit ulang entry yang sudah `completed` (saat ini read-only lewat UI,
     tapi tidak ada tombol edit)
   - Hapus journal entry
   - Pagination di `/journal` (list semua journal) — saat ini query semua
     tanpa limit, oke untuk personal use tapi akan lambat setelah ratusan
     entry
   - Validasi/rate-limit ke endpoint `/journal/{id}/discuss` (user bisa spam
     panggil AI berkali-kali tanpa batas — untuk personal app mungkin tidak
     masalah, tapi worth dipikirkan kalau biaya API jadi perhatian)
   - HTTPS/reverse proxy — README menyebutkan taruh Caddy/nginx di depan,
     tapi belum ada contoh konfigurasinya

6. **Icon di bottom nav & FAB masih pakai karakter Unicode polos** (⌂ ▤ ⏻ +)
   sebagai placeholder, bukan SVG/icon font. Ganti dengan icon set yang lebih
   matching kalau mau lebih rapi secara visual.

7. **Error handling AI call** di `JournalCreate` (`internal/handlers/
   journal.go`) saat ini "fail silently" — kalau panggilan ke Claude gagal
   (misal API key kosong/salah), background langsung dikosongkan tanpa
   pemberitahuan ke user. Pertimbangkan tampilkan pesan error yang jelas di
   UI supaya user tahu perlu cek konfigurasi `ANTHROPIC_API_KEY`.

8. **Belum ada test** sama sekali (unit maupun integration). Untuk app
   personal ini opsional, tapi kalau mau nambah fitur dengan aman ke depannya
   worth mulai nulis test untuk `internal/handlers` minimal.

## File-file kunci untuk dibaca duluan

- `cmd/server/main.go` — routing & wiring semua dependency
- `internal/handlers/journal.go` — inti flow journaling + AI
- `internal/ai/claude.go` — integrasi Claude, termasuk system prompt yang
  dipakai (`backgroundSystemPrompt`, `discussSystemPrompt`) — kemungkinan
  perlu di-tuning sesuai gaya devotion yang Anda inginkan
- `migrations/0001_init.sql` — schema, source of truth
- `web/templates/journal_view.html` — halaman paling kompleks, tempat semua
  step (background → refleksi → diskusi → langkah praktis) dirender

## Cara jalan lokal tanpa Docker (untuk debugging cepat)

```bash
# perlu Postgres jalan di localhost:5432 dengan db/user "journalflow"
cp .env.example .env   # sesuaikan DATABASE_URL
go run ./cmd/server
```
