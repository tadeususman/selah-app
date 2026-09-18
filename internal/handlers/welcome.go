package handlers

import (
	"net/http"

	"journalflow/internal/middleware"
)

type welcomePageData struct {
	UserName string
}

func (a *App) WelcomePage(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	var name string
	_ = a.DB.QueryRowContext(r.Context(),
		`SELECT name FROM users WHERE id = $1`, userID).Scan(&name)
	if name == "" {
		name = "teman"
	}
	a.render(w, "welcome.html", welcomePageData{UserName: name})
}

func (a *App) WelcomeSubmit(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	langStyle := r.FormValue("language_style")
	if langStyle != "casual" && langStyle != "formal" {
		langStyle = "casual"
	}
	theme := r.FormValue("theme")
	validThemes := map[string]bool{"default": true, "mawar": true, "lavender": true, "sage": true}
	if !validThemes[theme] {
		theme = "default"
	}
	_, err := a.DB.ExecContext(r.Context(),
		`UPDATE users SET language_style = $1, theme = $2, onboarded = true WHERE id = $3`,
		langStyle, theme, userID)
	if err != nil {
		http.Error(w, "could not save preferences", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}
