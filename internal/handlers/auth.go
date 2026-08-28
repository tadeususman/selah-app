package handlers

import (
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

type loginPageData struct {
	Error string
	Info  string
}

func (a *App) LoginPage(w http.ResponseWriter, r *http.Request) {
	// Already logged in? skip straight to dashboard.
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
	email := r.FormValue("email")
	password := r.FormValue("password")

	var userID int64
	var hash string
	err := a.DB.QueryRowContext(r.Context(),
		`SELECT id, password_hash FROM users WHERE email = $1`, email).
		Scan(&userID, &hash)

	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		a.render(w, "login.html", loginPageData{Error: "Email atau password salah."})
		return
	}

	if err := a.Sessions.Create(w, r, userID); err != nil {
		http.Error(w, "could not start session", http.StatusInternalServerError)
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
	email := r.FormValue("email")
	password := r.FormValue("password")
	name := r.FormValue("name")

	if email == "" || password == "" {
		a.render(w, "register.html", registerPageData{Error: "Email dan password wajib diisi."})
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

	_, err = a.DB.ExecContext(r.Context(),
		`INSERT INTO users (email, password_hash, name) VALUES ($1, $2, $3)`,
		email, string(hash), name)
	if err != nil {
		a.render(w, "register.html", registerPageData{Error: "Email sudah terdaftar."})
		return
	}

	http.Redirect(w, r, "/login?registered=1", http.StatusSeeOther)
}
