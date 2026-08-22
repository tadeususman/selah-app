package handlers

import (
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

type loginPageData struct {
	Error string
}

func (a *App) LoginPage(w http.ResponseWriter, r *http.Request) {
	// Already logged in? skip straight to dashboard.
	if _, ok := a.Sessions.UserID(r); ok {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	a.render(w, "login.html", loginPageData{})
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

// Register is a minimal, unlinked (no nav link) sign-up endpoint —
// since this is a personal single-user app, the expectation is you
// create your one account once (e.g. via `POST /register`) and then
// only ever use /login afterwards. Feel free to remove this route
// entirely after creating your account.
func (a *App) RegisterSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	email := r.FormValue("email")
	password := r.FormValue("password")
	name := r.FormValue("name")

	if email == "" || password == "" {
		http.Error(w, "email and password required", http.StatusBadRequest)
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
		http.Error(w, "could not create user (maybe email already exists)", http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
