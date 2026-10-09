package handlers

import (
	"context"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"journalflow/internal/middleware"
)

// Batas "Coba dulu": cukup untuk merasakan inti app (1 jurnal + diskusi),
// sambil menjaga biaya AI dan penyalahgunaan tetap terkendali.
const (
	guestMaxJournals    = 1
	guestMaxChats       = 6
	guestPerIPPerDay    = 3
	guestGlobalPerDay   = 50
	guestRetention      = 7 * 24 * time.Hour
	guestCookieName     = "jf_guest"
	guestCleanupEvery   = time.Hour
	guestUnusablePwHash = "!" // bukan hash bcrypt valid, jadi tidak pernah cocok saat login
)

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
	Error string
}

func (a *App) GuestPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.Sessions.UserID(r); ok {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	a.render(w, "coba.html", guestPageData{})
}

func (a *App) GuestStart(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.Sessions.UserID(r); ok {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	ip := clientIP(r)

	var perIP, global int
	_ = a.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM users WHERE is_guest AND guest_ip = $1 AND created_at > now() - interval '24 hours'`, ip).Scan(&perIP)
	_ = a.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM users WHERE is_guest AND created_at > now() - interval '24 hours'`).Scan(&global)
	if perIP >= guestPerIPPerDay {
		a.render(w, "coba.html", guestPageData{Error: "Kamu sudah mencoba beberapa kali hari ini. Daftar akun gratis untuk melanjutkan."})
		return
	}
	if global >= guestGlobalPerDay {
		a.render(w, "coba.html", guestPageData{Error: "Fitur coba sedang penuh hari ini. Silakan daftar akun gratis atau coba lagi besok."})
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
		res, err := a.DB.Exec(`DELETE FROM users WHERE is_guest AND created_at < now() - make_interval(secs => $1)`,
			guestRetention.Seconds())
		if err != nil {
			log.Printf("[guest] cleanup: %v", err)
		} else if n, _ := res.RowsAffected(); n > 0 {
			log.Printf("[guest] cleanup removed %d expired trial accounts", n)
		}
		time.Sleep(guestCleanupEvery)
	}
}
