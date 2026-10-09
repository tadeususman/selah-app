package handlers

import (
	"strings"
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"journalflow/internal/ai"
	"journalflow/internal/notify"
	"journalflow/internal/session"
)

type App struct {
	DB       *sql.DB
	Sessions *session.Manager
	AI       *ai.Client
	Tmpl     *template.Template
	Notify   *notify.Telegram
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
		// verse number from a ref: "Yohanes 3:16" -> "16"
		"verseNum": func(ref string) string {
			if i := strings.LastIndex(ref, ":"); i >= 0 {
				return ref[i+1:]
			}
			return ref
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
		// "2 Oktober 2026" for nullable time
		"idDatePtr": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return fmt.Sprintf("%d %s %d", t.Day(), idMonthsFull[t.Month()], t.Year())
		},
		// human-readable byte size: 1048576 → "1.0 MB"
		"fmtBytes": func(b uint64) string { return fmtBytes(b) },
		// integer addition — useful for 1-based indexing in templates
		"add": func(a, b int) int { return a + b },
		// percentage of a over b, integer 0-100 (0 if b==0)
		"percent": func(a, b int) int {
			if b == 0 {
				return 0
			}
			p := a * 100 / b
			if p < 0 {
				return 0
			}
			if p > 100 {
				return 100
			}
			return p
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
