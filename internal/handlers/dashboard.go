package handlers

import (
	"net/http"
	"time"

	"journalflow/internal/middleware"
	"journalflow/internal/models"
)

type recentEntry struct {
	ID        int64
	EntryDate time.Time
	VerseRef  string
	VerseText string
}

type dashboardData struct {
	UserName   string
	MonthLabel string
	Recent     []recentEntry
	Entries    []models.Preview
}

func (a *App) Dashboard(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	now := time.Now()

	// Fetch user name
	var userName string
	_ = a.DB.QueryRowContext(r.Context(),
		`SELECT name FROM users WHERE id = $1`, userID).Scan(&userName)

	// Last 3 journals (any month) with verse
	recentRows, err := a.DB.QueryContext(r.Context(), `
		SELECT id, entry_date, COALESCE(verse_ref,''), verse_text
		FROM journal_entries
		WHERE user_id = $1
		ORDER BY entry_date DESC, id DESC
		LIMIT 3`, userID)
	if err != nil {
		http.Error(w, "could not load recent journals", http.StatusInternalServerError)
		return
	}
	defer recentRows.Close()
	var recent []recentEntry
	for recentRows.Next() {
		var e recentEntry
		if err := recentRows.Scan(&e.ID, &e.EntryDate, &e.VerseRef, &e.VerseText); err != nil {
			http.Error(w, "could not read recent journals", http.StatusInternalServerError)
			return
		}
		if len(e.VerseText) > 80 {
			e.VerseText = e.VerseText[:80] + "…"
		}
		recent = append(recent, e)
	}

	// Current month entries for the monthly list
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	monthEnd := monthStart.AddDate(0, 1, 0)
	rows, err := a.DB.QueryContext(r.Context(), `
		SELECT id, day_number, entry_date,
		       COALESCE(NULLIF(reflection, ''), NULLIF(verse_text, ''), 'Belum ada isi') AS snippet
		FROM journal_entries
		WHERE user_id = $1 AND entry_date >= $2 AND entry_date < $3
		ORDER BY entry_date DESC, day_number DESC`,
		userID, monthStart, monthEnd)
	if err != nil {
		http.Error(w, "could not load journals", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var entries []models.Preview
	for rows.Next() {
		var p models.Preview
		if err := rows.Scan(&p.ID, &p.DayNumber, &p.EntryDate, &p.Snippet); err != nil {
			http.Error(w, "could not read journals", http.StatusInternalServerError)
			return
		}
		entries = append(entries, p)
	}

	idMonths := [13]string{"", "Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agt", "Sep", "Okt", "Nov", "Des"}
	monthLabel := idMonths[now.Month()] + " " + now.Format("2006")

	a.render(w, "dashboard.html", dashboardData{
		UserName:   userName,
		MonthLabel: monthLabel,
		Recent:     recent,
		Entries:    entries,
	})
}
