package handlers

import (
	"database/sql"
	"html/template"
	"log"
	"net/http"
	"path/filepath"

	"journalflow/internal/ai"
	"journalflow/internal/session"
)

// App bundles the dependencies every handler needs. Passing it as a
// receiver keeps handler signatures as plain http.HandlerFunc, which
// plugs straight into chi without extra adapters.
type App struct {
	DB       *sql.DB
	Sessions *session.Manager
	AI       *ai.Client
	Tmpl     *template.Template
}

// LoadTemplates parses every template in web/templates so {{template
// "layout" .}} composition works across files. Called once at startup;
// re-run (and restart the process) after editing templates.
func LoadTemplates(dir string) *template.Template {
	pattern := filepath.Join(dir, "*.html")
	t, err := template.ParseGlob(pattern)
	if err != nil {
		log.Fatalf("failed loading templates from %s: %v", pattern, err)
	}
	return t
}

func (a *App) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.Tmpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("template render error (%s): %v", name, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
