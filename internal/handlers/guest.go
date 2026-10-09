package handlers

import (
	"context"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"journalflow/internal/middleware"
)

// Batas "Coba dulu": cukup untuk merasakan inti app (1 jurnal + diskusi),
// sambil menjaga biaya AI dan penyalahgunaan tetap terkendali. Nilai bisa
// diubah admin (Admin → Pengaturan Server); konstanta di bawah hanyalah default.
const (
	guestRetention      = 7 * 24 * time.Hour
	guestCookieName     = "jf_guest"
	guestCreatePerIPDay = 30             // pagar teknis anti-spam baris tamu kosong; bukan kuota (kuota dihitung saat jurnal pertama)
	guestIdleRetention  = 24 * time.Hour // tamu yang hanya melihat-lihat (tanpa jurnal) dibersihkan lebih cepat
	guestCleanupEvery   = time.Hour
	guestUnusablePwHash = "!" // bukan hash bcrypt valid, jadi tidak pernah cocok saat login
)

type guestSettings struct {
	Enabled     bool
	MaxJournals int // per tamu
	MaxChats    int // pesan diskusi per tamu
	PerIPPerDay int // 0 = tanpa batas
	GlobalDay   int // 0 = tanpa batas
}

func defaultGuestSettings() guestSettings {
	return guestSettings{Enabled: true, MaxJournals: 1, MaxChats: 6, PerIPPerDay: 5, GlobalDay: 100}
}

func (a *App) loadGuestSettings(ctx context.Context) guestSettings {
	gs := defaultGuestSettings()
	rows, err := a.DB.QueryContext(ctx, `SELECT key, value FROM app_settings WHERE key LIKE 'guest_%'`)
	if err != nil {
		return gs
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) != nil {
			continue
		}
		n, convErr := strconv.Atoi(v)
		switch k {
		case "guest_enabled":
			gs.Enabled = v == "1"
		case "guest_max_journals":
			if convErr == nil && n >= 1 {
				gs.MaxJournals = n
			}
		case "guest_max_chats":
			if convErr == nil && n >= 1 {
				gs.MaxChats = n
			}
		case "guest_per_ip_day":
			if convErr == nil && n >= 0 {
				gs.PerIPPerDay = n
			}
		case "guest_global_day":
			if convErr == nil && n >= 0 {
				gs.GlobalDay = n
			}
		}
	}
	return gs
}

func (a *App) saveGuestSettings(ctx context.Context, gs guestSettings) error {
	enabled := "0"
	if gs.Enabled {
		enabled = "1"
	}
	for k, v := range map[string]string{
		"guest_enabled":      enabled,
		"guest_max_journals": strconv.Itoa(gs.MaxJournals),
		"guest_max_chats":    strconv.Itoa(gs.MaxChats),
		"guest_per_ip_day":   strconv.Itoa(gs.PerIPPerDay),
		"guest_global_day":   strconv.Itoa(gs.GlobalDay),
	} {
		if _, err := a.DB.ExecContext(ctx, `
			INSERT INTO app_settings (key, value) VALUES ($1, $2)
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, k, v); err != nil {
			return err
		}
	}
	return nil
}

// guestQuotaFull reports whether today's "Coba dulu" quota is used up. Only
// guests who have actually written a journal count (that is when AI cost
// starts), so people just browsing never consume quota.
func (a *App) guestQuotaFull(ctx context.Context, userID int64, gs guestSettings) bool {
	var ip string
	_ = a.DB.QueryRowContext(ctx, `SELECT COALESCE(guest_ip, '') FROM users WHERE id = $1`, userID).Scan(&ip)
	const active = `SELECT COUNT(DISTINCT u.id) FROM users u JOIN journal_entries j ON j.user_id = u.id
		WHERE u.is_guest AND u.id <> $1 AND j.created_at > now() - interval '24 hours'`
	var perIP, global int
	if gs.PerIPPerDay > 0 && ip != "" {
		_ = a.DB.QueryRowContext(ctx, active+` AND u.guest_ip = $2`, userID, ip).Scan(&perIP)
		if perIP >= gs.PerIPPerDay {
			return true
		}
	}
	if gs.GlobalDay > 0 {
		_ = a.DB.QueryRowContext(ctx, active, userID).Scan(&global)
		if global >= gs.GlobalDay {
			return true
		}
	}
	return false
}

// clientIP prefers the proxy-provided address (Cloudflare, then nginx).
func clientIP(r *http.Request) string {
	for _, h := range []string{"CF-Connecting-IP", "X-Real-IP"} {
		if v := strings.TrimSpace(r.Header.Get(h)); v != "" {
			return v
		}
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		return strings.TrimSpace(strings.Split(v, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (a *App) isGuest(ctx context.Context, userID int64) bool {
	var g bool
	_ = a.DB.QueryRowContext(ctx, `SELECT is_guest FROM users WHERE id = $1`, userID).Scan(&g)
	return g
}

// setGuestCookie is a readable (non-HttpOnly) hint so pages can show the
// "kamu sedang mencoba" banner. It is cosmetic only; the real check is is_guest in the DB.
func setGuestCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: guestCookieName, Value: "1", Path: "/",
		Expires:  time.Now().Add(guestRetention),
		SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
}

func clearGuestCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: guestCookieName, Value: "", Path: "/", Expires: time.Unix(0, 0), MaxAge: -1})
}

type guestPageData struct {
	Error  string
	Closed bool
}

func (a *App) GuestPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.Sessions.UserID(r); ok {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	if !a.loadGuestSettings(r.Context()).Enabled {
		a.render(w, "coba.html", guestPageData{Error: "Fitur coba sedang ditutup sementara. Silakan daftar akun gratis.", Closed: true})
		return
	}
	a.render(w, "coba.html", guestPageData{})
}

func (a *App) GuestStart(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.Sessions.UserID(r); ok {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	gs := a.loadGuestSettings(r.Context())
	if !gs.Enabled {
		a.render(w, "coba.html", guestPageData{Error: "Fitur coba sedang ditutup sementara. Silakan daftar akun gratis.", Closed: true})
		return
	}
	ip := clientIP(r)

	var created int
	_ = a.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM users WHERE is_guest AND guest_ip = $1 AND created_at > now() - interval '24 hours'`, ip).Scan(&created)
	if created >= guestCreatePerIPDay {
		a.render(w, "coba.html", guestPageData{Error: "Terlalu banyak percobaan dari jaringan ini hari ini. Daftar akun gratis untuk melanjutkan."})
		return
	}

	var userID int64
	err := a.DB.QueryRowContext(r.Context(), `
		INSERT INTO users (password_hash, name, onboarded, generate_share_card, is_guest, guest_ip)
		VALUES ($1, '', false, false, true, $2) RETURNING id`, guestUnusablePwHash, ip).Scan(&userID)
	if err != nil {
		log.Printf("[guest] create: %v", err)
		http.Error(w, "could not start trial", http.StatusInternalServerError)
		return
	}
	if err := a.Sessions.Create(w, r, userID); err != nil {
		http.Error(w, "could not start session", http.StatusInternalServerError)
		return
	}
	setGuestCookie(w, r)
	http.Redirect(w, r, "/welcome", http.StatusSeeOther)
}

type upgradePageData struct {
	Error string
	Info  string
	Name  string
}

func (a *App) GuestUpgradePage(w http.ResponseWriter, r *http.Request) {
	if !a.isGuest(r.Context(), middleware.UserID(r)) {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	data := upgradePageData{}
	switch r.URL.Query().Get("from") {
	case "journal":
		data.Info = "Kamu sudah mencoba satu jurnal. Daftar gratis supaya bisa terus menulis, dan jurnalmu ikut tersimpan."
	case "quota":
		data.Info = "Kuota mencoba hari ini sudah penuh. Daftar gratis supaya bisa langsung menulis jurnal."
	case "chat":
		data.Info = "Batas percakapan untuk mencoba sudah tercapai. Daftar gratis untuk melanjutkan, dan jurnalmu ikut tersimpan."
	case "feature":
		data.Info = "Fitur ini tersedia setelah kamu mendaftar. Jurnal yang sudah kamu tulis ikut tersimpan."
	}
	a.render(w, "simpan.html", data)
}

// GuestUpgradeSubmit turns the guest account into a real one in place, so the
// journal written during the trial is kept.
func (a *App) GuestUpgradeSubmit(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	if !a.isGuest(r.Context(), userID) {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	identifier := strings.TrimSpace(r.FormValue("identifier"))
	password := r.FormValue("password")
	name := strings.TrimSpace(r.FormValue("name"))
	fail := func(msg string) {
		a.render(w, "simpan.html", upgradePageData{Error: msg, Name: name})
	}

	if identifier == "" {
		fail("Email atau nomor HP wajib diisi.")
		return
	}
	if len(password) < 8 {
		fail("Password minimal 8 karakter.")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "could not hash password", http.StatusInternalServerError)
		return
	}

	col, value, taken := "email", identifier, "Email sudah terdaftar."
	if !strings.Contains(identifier, "@") {
		col, value, taken = "phone", normalizePhone(identifier), "Nomor HP sudah terdaftar."
	}
	// col is one of two fixed literals above, never user input.
	_, err = a.DB.ExecContext(r.Context(),
		`UPDATE users SET `+col+` = $1, password_hash = $2, name = $3,
		        is_guest = false, guest_ip = NULL, generate_share_card = true
		 WHERE id = $4 AND is_guest`,
		value, string(hash), name, userID)
	if err != nil {
		fail(taken)
		return
	}

	clearGuestCookie(w)
	var onboarded bool
	_ = a.DB.QueryRowContext(r.Context(), `SELECT onboarded FROM users WHERE id = $1`, userID).Scan(&onboarded)
	if !onboarded {
		http.Redirect(w, r, "/welcome", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// BlockGuests keeps trial accounts out of features that cost AI budget or
// manage a real account (Plan, settings, AI verse search).
func (a *App) BlockGuests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.isGuest(r.Context(), middleware.UserID(r)) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"Pencarian dengan Teman Selah tersedia setelah kamu mendaftar. Coba tombol Pilihkan Ayat."}`))
				return
			}
			dest := "/simpan?from=feature"
			if r.URL.Path == "/user" {
				dest = "/simpan"
			}
			http.Redirect(w, r, dest, http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// GuestCleanupLoop deletes trial accounts (and, via cascade, their journals)
// that were never turned into real accounts.
func (a *App) GuestCleanupLoop() {
	for {
		res, err := a.DB.Exec(`
			DELETE FROM users WHERE is_guest AND (
				created_at < now() - make_interval(secs => $1)
				OR (created_at < now() - make_interval(secs => $2)
				    AND NOT EXISTS (SELECT 1 FROM journal_entries WHERE user_id = users.id)))`,
			guestRetention.Seconds(), guestIdleRetention.Seconds())
		if err != nil {
			log.Printf("[guest] cleanup: %v", err)
		} else if n, _ := res.RowsAffected(); n > 0 {
			log.Printf("[guest] cleanup removed %d expired trial accounts", n)
		}
		time.Sleep(guestCleanupEvery)
	}
}
