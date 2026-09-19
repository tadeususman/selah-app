package handlers

import (
	"net/http"
	"strings"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

type loginPageData struct {
	Error string
	Info  string
}

func (a *App) LoginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.Sessions.UserID(r); ok {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	data := loginPageData{}
	if r.URL.Query().Get("deleted") == "1" {
		data.Info = "Akun kamu berhasil dihapus. Sampai jumpa."
	}
	a.render(w, "login.html", data)
}

func (a *App) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	identifier := strings.TrimSpace(r.FormValue("identifier"))
	password := r.FormValue("password")

	var userID int64
	var hash string
	var onboarded bool
	var err error

	if strings.Contains(identifier, "@") {
		err = a.DB.QueryRowContext(r.Context(),
			`SELECT id, password_hash, onboarded FROM users WHERE email = $1`, identifier).
			Scan(&userID, &hash, &onboarded)
	} else {
		phone := normalizePhone(identifier)
		err = a.DB.QueryRowContext(r.Context(),
			`SELECT id, password_hash, onboarded FROM users WHERE phone = $1`, phone).
			Scan(&userID, &hash, &onboarded)
	}

	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		a.render(w, "login.html", loginPageData{Error: "Email/nomor HP atau password salah."})
		return
	}

	if err := a.Sessions.Create(w, r, userID); err != nil {
		http.Error(w, "could not start session", http.StatusInternalServerError)
		return
	}
	if !onboarded {
		http.Redirect(w, r, "/welcome", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (a *App) Logout(w http.ResponseWriter, r *http.Request) {
	a.Sessions.Destroy(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

type registerPageData struct {
	Error string
}

func (a *App) RegisterPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.Sessions.UserID(r); ok {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	a.render(w, "register.html", registerPageData{})
}

func (a *App) RegisterSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	identifier := strings.TrimSpace(r.FormValue("identifier"))
	password := r.FormValue("password")
	name := r.FormValue("name")

	if identifier == "" {
		a.render(w, "register.html", registerPageData{Error: "Email atau nomor HP wajib diisi."})
		return
	}
	if len(password) < 8 {
		a.render(w, "register.html", registerPageData{Error: "Password minimal 8 karakter."})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "could not hash password", http.StatusInternalServerError)
		return
	}

	if strings.Contains(identifier, "@") {
		_, err = a.DB.ExecContext(r.Context(),
			`INSERT INTO users (email, password_hash, name, onboarded) VALUES ($1, $2, $3, false)`,
			identifier, string(hash), name)
		if err != nil {
			a.render(w, "register.html", registerPageData{Error: "Email sudah terdaftar."})
			return
		}
	} else {
		phone := normalizePhone(identifier)
		_, err = a.DB.ExecContext(r.Context(),
			`INSERT INTO users (phone, password_hash, name, onboarded) VALUES ($1, $2, $3, false)`,
			phone, string(hash), name)
		if err != nil {
			a.render(w, "register.html", registerPageData{Error: "Nomor HP sudah terdaftar."})
			return
		}
	}

	http.Redirect(w, r, "/login?registered=1", http.StatusSeeOther)
}

// normalizePhone converts Indonesian phone formats to +62xxxxxxxxxx.
func normalizePhone(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) || r == '+' {
			b.WriteRune(r)
		}
	}
	p := b.String()
	switch {
	case strings.HasPrefix(p, "+62"):
		return p
	case strings.HasPrefix(p, "62"):
		return "+" + p
	case strings.HasPrefix(p, "0"):
		return "+62" + p[1:]
	case strings.HasPrefix(p, "8"):
		return "+62" + p
	}
	return p
}
