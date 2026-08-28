package handlers

import (
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
	appconfig "journalflow/internal/config"
	"journalflow/internal/middleware"
)

type userPageData struct {
	Email               string
	Name                string
	IsAdmin             bool
	DiscussOriginalLang bool
	Version             string
	Year                int
	FlashOK             string
	FlashErr            string
}

func (a *App) UserPage(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	var name, email string
	var isAdmin, discussOriginalLang bool
	err := a.DB.QueryRowContext(r.Context(),
		`SELECT name, email, is_admin, discuss_original_lang FROM users WHERE id = $1`, userID).Scan(&name, &email, &isAdmin, &discussOriginalLang)
	if err != nil {
		http.Error(w, "could not load user", http.StatusInternalServerError)
		return
	}
	data := userPageData{Name: name, Email: email, IsAdmin: isAdmin, DiscussOriginalLang: discussOriginalLang, Version: appconfig.Version, Year: time.Now().Year()}
	switch r.URL.Query().Get("ok") {
	case "email":
		data.FlashOK = "Email berhasil diperbarui."
	case "name":
		data.FlashOK = "Nama berhasil diperbarui."
	case "password":
		data.FlashOK = "Password berhasil diganti."
	case "prefs":
		data.FlashOK = "Preferensi berhasil disimpan."
	}
	switch r.URL.Query().Get("err") {
	case "wrong_password":
		data.FlashErr = "Password sekarang salah."
	case "mismatch":
		data.FlashErr = "Konfirmasi password baru tidak cocok."
	case "short":
		data.FlashErr = "Password baru minimal 8 karakter."
	}
	a.render(w, "user.html", data)
}

func (a *App) UserUpdateEmail(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	email := r.FormValue("email")
	if email == "" {
		http.Redirect(w, r, "/user?err=empty", http.StatusSeeOther)
		return
	}
	_, err := a.DB.ExecContext(r.Context(),
		`UPDATE users SET email = $1 WHERE id = $2`, email, userID)
	if err != nil {
		http.Error(w, "could not update email", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/user?ok=email", http.StatusSeeOther)
}

func (a *App) UserUpdateName(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := r.FormValue("name")
	if name == "" {
		http.Redirect(w, r, "/user?err=empty", http.StatusSeeOther)
		return
	}
	_, err := a.DB.ExecContext(r.Context(),
		`UPDATE users SET name = $1 WHERE id = $2`, name, userID)
	if err != nil {
		http.Error(w, "could not update name", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/user?ok=name", http.StatusSeeOther)
}

func (a *App) UserUpdatePrefs(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	originalLang := r.FormValue("discuss_original_lang") == "on"
	_, err := a.DB.ExecContext(r.Context(),
		`UPDATE users SET discuss_original_lang = $1 WHERE id = $2`, originalLang, userID)
	if err != nil {
		http.Error(w, "could not update preferences", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/user?ok=prefs", http.StatusSeeOther)
}

func (a *App) UserUpdatePassword(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	oldPw := r.FormValue("old_password")
	newPw := r.FormValue("new_password")
	newPw2 := r.FormValue("new_password2")

	if len(newPw) < 8 {
		http.Redirect(w, r, "/user?err=short", http.StatusSeeOther)
		return
	}
	if newPw != newPw2 {
		http.Redirect(w, r, "/user?err=mismatch", http.StatusSeeOther)
		return
	}

	var hash string
	err := a.DB.QueryRowContext(r.Context(),
		`SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&hash)
	if err != nil {
		http.Error(w, "could not load user", http.StatusInternalServerError)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(oldPw)) != nil {
		http.Redirect(w, r, "/user?err=wrong_password", http.StatusSeeOther)
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(newPw), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "could not hash password", http.StatusInternalServerError)
		return
	}
	_, err = a.DB.ExecContext(r.Context(),
		`UPDATE users SET password_hash = $1 WHERE id = $2`, string(newHash), userID)
	if err != nil {
		http.Error(w, "could not update password", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/user?ok=password", http.StatusSeeOther)
}
