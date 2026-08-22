package handlers

import (
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"journalflow/internal/ai"
	"journalflow/internal/session"
)

type App struct {
	DB       *sql.DB
	Sessions *session.Manager
	AI       *ai.Client
	Tmpl     *template.Template
}

var idMonths = [13]string{"", "Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agt", "Sep", "Okt", "Nov", "Des"}
var idMonthsFull = [13]string{"", "Januari", "Februari", "Maret", "April", "Mei", "Juni", "Juli", "Agustus", "September", "Oktober", "November", "Desember"}
var idDays = map[time.Weekday]string{
	time.Sunday:    "Minggu",
	time.Monday:    "Senin",
	time.Tuesday:   "Selasa",
	time.Wednesday: "Rabu",
	time.Thursday:  "Kamis",
	time.Friday:    "Jumat",
	time.Saturday:  "Sabtu",
}

func LoadTemplates(dir string) *template.Template {
	funcs := template.FuncMap{
		// "2 Agustus 2006" → formatted in Indonesian
		"idDate": func(t time.Time) string {
			return fmt.Sprintf("%d %s %d", t.Day(), idMonthsFull[t.Month()], t.Year())
		},
		// short month + year: "Agt 2026"
		"idMonthYear": func(t time.Time) string {
			return fmt.Sprintf("%s %d", idMonths[t.Month()], t.Year())
		},
		// day name: "Sabtu"
		"idDay": func(t time.Time) string {
			return idDays[t.Weekday()]
		},
	}

	pattern := filepath.Join(dir, "*.html")
	t, err := template.New("").Funcs(funcs).ParseGlob(pattern)
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
