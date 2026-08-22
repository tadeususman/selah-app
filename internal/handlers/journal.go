package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"journalflow/internal/ai"
	"journalflow/internal/middleware"
	"journalflow/internal/models"
)

// ---- GET /journal (all entries, for the bottom-nav "Journal" tab) ----

type journalListData struct {
	Entries []models.Preview
}

func (a *App) JournalList(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	rows, err := a.DB.QueryContext(r.Context(), `
		SELECT id, day_number, entry_date,
		       COALESCE(NULLIF(reflection, ''), NULLIF(verse_text, ''), 'Belum ada isi') AS snippet
		FROM journal_entries
		WHERE user_id = $1
		ORDER BY entry_date DESC, day_number DESC`,
		userID)
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
	a.render(w, "journal_list.html", journalListData{Entries: entries})
}

// ---- GET /journal/new (step 1: ask which verse to reflect on) ----

func (a *App) JournalNewPage(w http.ResponseWriter, r *http.Request) {
	a.render(w, "journal_new.html", nil)
}

// ---- POST /journal (create entry from verse, then fetch AI background) ----

func (a *App) JournalCreate(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	verseRef := r.FormValue("verse_ref")
	verseText := r.FormValue("verse_text")
	location := r.FormValue("location")

	if verseText == "" {
		http.Error(w, "verse text is required", http.StatusBadRequest)
		return
	}

	var nextDay int
	err := a.DB.QueryRowContext(r.Context(),
		`SELECT COALESCE(MAX(day_number), 0) + 1 FROM journal_entries WHERE user_id = $1`,
		userID).Scan(&nextDay)
	if err != nil {
		http.Error(w, "could not compute day number", http.StatusInternalServerError)
		return
	}

	// Ask Claude for historical/original-language background before
	// saving, so the entry is created already populated (matches the
	// "AI: Search Background Peristiwa" step in the flow).
	background, aiErr := a.AI.VerseBackground(r.Context(), verseRef, verseText)
	if aiErr != nil {
		// Don't block journaling if the AI call fails (e.g. key not
		// configured yet) — just leave it blank and let the user
		// retry the discussion later.
		background = ""
	}

	var entryID int64
	err = a.DB.QueryRowContext(r.Context(), `
		INSERT INTO journal_entries
			(user_id, day_number, entry_date, entry_time, location, verse_ref, verse_text, ai_background)
		VALUES ($1, $2, CURRENT_DATE, CURRENT_TIME, $3, $4, $5, $6)
		RETURNING id`,
		userID, nextDay, location, verseRef, verseText, background,
	).Scan(&entryID)
	if err != nil {
		http.Error(w, "could not create journal entry", http.StatusInternalServerError)
		return
	}

	if background != "" {
		_, _ = a.DB.ExecContext(r.Context(),
			`INSERT INTO journal_messages (entry_id, role, content) VALUES ($1, 'ai', $2)`,
			entryID, background)
	}

	http.Redirect(w, r, "/journal/"+strconv.FormatInt(entryID, 10), http.StatusSeeOther)
}

// ---- GET /journal/{id} (view/continue a devotion session) ----

type journalViewData struct {
	Entry    models.JournalEntry
	Messages []models.JournalMessage
}

func (a *App) JournalView(w http.ResponseWriter, r *http.Request) {
	entry, ok := a.loadOwnedEntry(w, r)
	if !ok {
		return
	}

	rows, err := a.DB.QueryContext(r.Context(),
		`SELECT id, entry_id, role, content, created_at FROM journal_messages WHERE entry_id = $1 ORDER BY created_at ASC`,
		entry.ID)
	if err != nil {
		http.Error(w, "could not load discussion", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var messages []models.JournalMessage
	for rows.Next() {
		var m models.JournalMessage
		if err := rows.Scan(&m.ID, &m.EntryID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
			http.Error(w, "could not read discussion", http.StatusInternalServerError)
			return
		}
		messages = append(messages, m)
	}

	a.render(w, "journal_view.html", journalViewData{Entry: entry, Messages: messages})
}

// ---- POST /journal/{id}/reflect (save "apa yang didapat setelah membaca") ----

func (a *App) JournalReflect(w http.ResponseWriter, r *http.Request) {
	entry, ok := a.loadOwnedEntry(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	reflection := r.FormValue("reflection")

	_, err := a.DB.ExecContext(r.Context(),
		`UPDATE journal_entries SET reflection = $1, updated_at = now() WHERE id = $2`,
		reflection, entry.ID)
	if err != nil {
		http.Error(w, "could not save reflection", http.StatusInternalServerError)
		return
	}
	if reflection != "" {
		_, _ = a.DB.ExecContext(r.Context(),
			`INSERT INTO journal_messages (entry_id, role, content) VALUES ($1, 'user', $2)`,
			entry.ID, reflection)
	}

	http.Redirect(w, r, "/journal/"+strconv.FormatInt(entry.ID, 10), http.StatusSeeOther)
}

// ---- POST /journal/{id}/discuss (the "Ask AI" loop) ----

func (a *App) JournalDiscuss(w http.ResponseWriter, r *http.Request) {
	entry, ok := a.loadOwnedEntry(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	question := r.FormValue("message")
	if question == "" {
		http.Redirect(w, r, "/journal/"+strconv.FormatInt(entry.ID, 10), http.StatusSeeOther)
		return
	}

	// Rebuild the full conversation so far so Claude has context of
	// the verse, background, and everything discussed previously.
	rows, err := a.DB.QueryContext(r.Context(),
		`SELECT role, content FROM journal_messages WHERE entry_id = $1 ORDER BY created_at ASC`,
		entry.ID)
	if err != nil {
		http.Error(w, "could not load discussion history", http.StatusInternalServerError)
		return
	}
	history := []struct{ Role, Content string }{
		{"user", "Ayat yang sedang saya renungkan (" + entry.VerseRef + "): " + entry.VerseText},
	}
	for rows.Next() {
		var role, content string
		if err := rows.Scan(&role, &content); err == nil {
			history = append(history, struct{ Role, Content string }{role, content})
		}
	}
	rows.Close()
	history = append(history, struct{ Role, Content string }{"user", question})

	msgs := toChatMessages(history)
	answer, err := a.AI.Discuss(r.Context(), msgs)
	if err != nil {
		answer = "Maaf, AI sedang tidak bisa dihubungi. Coba lagi sebentar. (" + err.Error() + ")"
	}

	_, _ = a.DB.ExecContext(r.Context(),
		`INSERT INTO journal_messages (entry_id, role, content) VALUES ($1, 'user', $2)`,
		entry.ID, question)
	_, _ = a.DB.ExecContext(r.Context(),
		`INSERT INTO journal_messages (entry_id, role, content) VALUES ($1, 'ai', $2)`,
		entry.ID, answer)

	http.Redirect(w, r, "/journal/"+strconv.FormatInt(entry.ID, 10), http.StatusSeeOther)
}

// ---- POST /journal/{id}/complete (save practical step, mark done) ----

func (a *App) JournalComplete(w http.ResponseWriter, r *http.Request) {
	entry, ok := a.loadOwnedEntry(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	step := r.FormValue("practical_step")

	_, err := a.DB.ExecContext(r.Context(), `
		UPDATE journal_entries
		SET practical_step = $1, status = 'completed', updated_at = now()
		WHERE id = $2`,
		step, entry.ID)
	if err != nil {
		http.Error(w, "could not complete journal", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// ---- helpers ----

func (a *App) loadOwnedEntry(w http.ResponseWriter, r *http.Request) (models.JournalEntry, bool) {
	userID := middleware.UserID(r)
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return models.JournalEntry{}, false
	}

	var e models.JournalEntry
	var entryDate, entryTime time.Time
	err = a.DB.QueryRowContext(r.Context(), `
		SELECT id, user_id, day_number, entry_date, entry_time, location,
		       verse_ref, verse_text, ai_background, reflection, practical_step,
		       status, created_at, updated_at
		FROM journal_entries WHERE id = $1 AND user_id = $2`,
		id, userID,
	).Scan(&e.ID, &e.UserID, &e.DayNumber, &entryDate, &entryTime, &e.Location,
		&e.VerseRef, &e.VerseText, &e.AIBackground, &e.Reflection, &e.PracticalStep,
		&e.Status, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		http.NotFound(w, r)
		return models.JournalEntry{}, false
	}
	e.EntryDate, e.EntryTime = entryDate, entryTime
	return e, true
}

// toChatMessages converts our stored history into the shape Claude's
// API expects. Internally we label AI turns "ai" (see journal_messages
// schema); the API wants "assistant".
func toChatMessages(history []struct{ Role, Content string }) []ai.ChatMessage {
	out := make([]ai.ChatMessage, 0, len(history))
	for _, h := range history {
		role := h.Role
		if role == "ai" {
			role = "assistant"
		}
		out = append(out, ai.ChatMessage{Role: role, Content: h.Content})
	}
	return out
}
