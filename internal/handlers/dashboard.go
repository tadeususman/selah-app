package handlers

import (
	"net/http"
	"time"

	"journalflow/internal/middleware"
	"journalflow/internal/models"
)

type shareCardPreview struct {
	ID           int64
	VerseRef     string
	ShareSummary string
	EntryDate    time.Time
}

type dashboardData struct {
	UserID        int64
	UserName      string
	Greeting      string
	Entries       []models.Preview
	ShareCards    []shareCardPreview
	LanguageStyle string
}

func (a *App) Dashboard(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	now := time.Now()

	var userName, langStyle string
	_ = a.DB.QueryRowContext(r.Context(),
		`SELECT name, language_style FROM users WHERE id = $1`, userID).Scan(&userName, &langStyle)
	if langStyle == "" {
		langStyle = "casual"
	}

	// Last 3 journal entries regardless of month.
	rows, err := a.DB.QueryContext(r.Context(), `
		SELECT id, day_number, entry_date,
		       COALESCE(NULLIF(reflection, ''), NULLIF(verse_text, ''), 'Belum ada isi') AS snippet,
		       COALESCE(verse_ref, ''), verse_text
		FROM journal_entries
		WHERE user_id = $1
		ORDER BY entry_date DESC, day_number DESC
		LIMIT 3`, userID)
	if err != nil {
		http.Error(w, "could not load journals", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var entries []models.Preview
	for rows.Next() {
		var p models.Preview
		if err := rows.Scan(&p.ID, &p.DayNumber, &p.EntryDate, &p.Snippet, &p.VerseRef, &p.VerseText); err != nil {
			http.Error(w, "could not read journals", http.StatusInternalServerError)
			return
		}
		if len(p.VerseText) > 100 {
			p.VerseText = p.VerseText[:100] + "…"
		}
		entries = append(entries, p)
	}

	// Last 4 completed entries with a share summary for the card carousel.
	cardRows, err := a.DB.QueryContext(r.Context(), `
		SELECT id, verse_ref, share_summary, entry_date
		FROM journal_entries
		WHERE user_id = $1 AND status = 'completed' AND share_summary != ''
		ORDER BY entry_date DESC, day_number DESC
		LIMIT 4`, userID)
	var shareCards []shareCardPreview
	if err == nil {
		defer cardRows.Close()
		for cardRows.Next() {
			var c shareCardPreview
			if cardRows.Scan(&c.ID, &c.VerseRef, &c.ShareSummary, &c.EntryDate) == nil {
				shareCards = append(shareCards, c)
			}
		}
	}

	wib, _ := time.LoadLocation("Asia/Jakarta")
	hour := now.In(wib).Hour()
	greeting := "Selamat malam"
	switch {
	case hour < 11:
		greeting = "Selamat pagi"
	case hour < 15:
		greeting = "Selamat siang"
	case hour < 18:
		greeting = "Selamat sore"
	}

	a.render(w, "dashboard.html", dashboardData{
		UserID:        userID,
		UserName:      userName,
		Greeting:      greeting,
		Entries:       entries,
		ShareCards:    shareCards,
		LanguageStyle: langStyle,
	})
}
