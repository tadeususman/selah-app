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
		// message time: "14:05" in WIB
		"msgTime": func(t time.Time) string {
			loc, _ := time.LoadLocation("Asia/Jakarta")
			return t.In(loc).Format("15:04")
		},
		// date+time in WIB for nullable time
		"wibDateTime": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			loc, _ := time.LoadLocation("Asia/Jakarta")
			return t.In(loc).Format("2 Jan 2006, 15:04")
		},
		// short month + 2-digit year: "Agt 26"
		"idMonthShort": func(t time.Time) string {
			return fmt.Sprintf("%s %02d", idMonths[t.Month()], t.Year()%100)
		},
		// format integer with dot thousand separator: 36389 → "36.389"
		"fmtInt": func(n int) string {
			s := fmt.Sprintf("%d", n)
			out := []byte{}
			for i, c := range s {
				if i > 0 && (len(s)-i)%3 == 0 {
					out = append(out, '.')
				}
				out = append(out, byte(c))
			}
			return string(out)
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
