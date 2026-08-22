package handlers

import (
	"net/http"

	"golang.org/x/crypto/bcrypt"
	"journalflow/internal/middleware"
)

type adminPageData struct {
	Email    string
	Name     string
	FlashOK  string
	FlashErr string
}

func (a *App) AdminPage(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	var name, email string
	err := a.DB.QueryRowContext(r.Context(),
		`SELECT name, email FROM users WHERE id = $1`, userID).Scan(&name, &email)
	if err != nil {
		http.Error(w, "could not load user", http.StatusInternalServerError)
		return
	}
	data := adminPageData{Name: name, Email: email}
	switch r.URL.Query().Get("ok") {
	case "name":
		data.FlashOK = "Nama berhasil diperbarui."
	case "password":
		data.FlashOK = "Password berhasil diganti."
	}
	switch r.URL.Query().Get("err") {
	case "wrong_password":
		data.FlashErr = "Password sekarang salah."
	case "mismatch":
		data.FlashErr = "Konfirmasi password baru tidak cocok."
	case "short":
		data.FlashErr = "Password baru minimal 8 karakter."
	}
	a.render(w, "admin.html", data)
}

func (a *App) AdminUpdateName(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := r.FormValue("name")
	if name == "" {
		http.Redirect(w, r, "/admin?err=empty", http.StatusSeeOther)
		return
	}
	_, err := a.DB.ExecContext(r.Context(),
		`UPDATE users SET name = $1 WHERE id = $2`, name, userID)
	if err != nil {
		http.Error(w, "could not update name", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin?ok=name", http.StatusSeeOther)
}

func (a *App) AdminUpdatePassword(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	oldPw := r.FormValue("old_password")
	newPw := r.FormValue("new_password")
	newPw2 := r.FormValue("new_password2")

	if len(newPw) < 8 {
		http.Redirect(w, r, "/admin?err=short", http.StatusSeeOther)
		return
	}
	if newPw != newPw2 {
		http.Redirect(w, r, "/admin?err=mismatch", http.StatusSeeOther)
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
		http.Redirect(w, r, "/admin?err=wrong_password", http.StatusSeeOther)
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
	http.Redirect(w, r, "/admin?ok=password", http.StatusSeeOther)
}
