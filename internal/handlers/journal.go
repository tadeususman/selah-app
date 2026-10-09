package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"journalflow/internal/ai"
	"journalflow/internal/middleware"
	"journalflow/internal/models"
)

const dailyJournalLimit = 10

// ---- GET /journal (all entries, for the bottom-nav "Journal" tab) ----

type journalListData struct {
	Entries []models.Preview
}

func (a *App) JournalList(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	rows, err := a.DB.QueryContext(r.Context(), `
		SELECT je.id, je.day_number, je.entry_date, je.entry_time,
		       COALESCE(je.location, ''),
		       COALESCE(NULLIF(je.reflection, ''), NULLIF(je.verse_text, ''), 'Belum ada isi') AS snippet,
		       COALESCE(je.verse_ref, ''), je.verse_text, je.status,
		       COALESCE(je.plan_id, 0), COALESCE(je.plan_day, 0),
		       COALESCE(p.name, ''), COALESCE(p.duration, 0)
		FROM journal_entries je
		LEFT JOIN plans p ON p.id = je.plan_id
		WHERE je.user_id = $1
		ORDER BY je.entry_date DESC, je.day_number DESC
		LIMIT 15`,
		userID)
	if err != nil {
		http.Error(w, "could not load journals", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var entries []models.Preview
	for rows.Next() {
		var p models.Preview
		if err := rows.Scan(&p.ID, &p.DayNumber, &p.EntryDate, &p.EntryTime, &p.Location, &p.Snippet, &p.VerseRef, &p.VerseText, &p.Status,
			&p.PlanID, &p.PlanDay, &p.PlanName, &p.PlanDuration); err != nil {
			http.Error(w, "could not read journals", http.StatusInternalServerError)
			return
		}
		if len(p.VerseText) > 100 {
			p.VerseText = p.VerseText[:100] + "…"
		}
		entries = append(entries, p)
	}
	a.render(w, "journal_list.html", journalListData{Entries: entries})
}

// ---- GET /journal/more?before=<id> (infinite scroll, returns JSON) ----

type journalPreviewJSON struct {
	ID        int64  `json:"id"`
	EntryDate string `json:"entry_date"`
	EntryTime string `json:"entry_time"`
	Location  string `json:"location"`
	VerseRef  string `json:"verse_ref"`
	VerseText string `json:"verse_text"`
	Status    string `json:"status"`
	Day       int    `json:"day"`
	Weekday   string `json:"weekday"`
	MonthShort string `json:"month_short"`
	PlanID       int64  `json:"plan_id,omitempty"`
	PlanDay      int    `json:"plan_day,omitempty"`
	PlanName     string `json:"plan_name,omitempty"`
	PlanDuration int    `json:"plan_duration,omitempty"`
}

func (a *App) JournalMore(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	beforeID, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	if beforeID <= 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"entries": []any{}, "has_more": false})
		return
	}

	rows, err := a.DB.QueryContext(r.Context(), `
		SELECT je.id, je.day_number, je.entry_date, je.entry_time,
		       COALESCE(je.location, ''),
		       COALESCE(NULLIF(je.verse_ref, ''), '') AS verse_ref,
		       COALESCE(NULLIF(je.verse_text, ''), '') AS verse_text,
		       je.status,
		       COALESCE(je.plan_id, 0), COALESCE(je.plan_day, 0),
		       COALESCE(p.name, ''), COALESCE(p.duration, 0)
		FROM journal_entries je
		LEFT JOIN plans p ON p.id = je.plan_id
		WHERE je.user_id = $1 AND je.id < $2
		ORDER BY je.entry_date DESC, je.day_number DESC
		LIMIT 16`, userID, beforeID)
	if err != nil {
		http.Error(w, "could not load journals", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	loc, _ := time.LoadLocation("Asia/Jakarta")
	idDaysMap := map[time.Weekday]string{0: "Minggu", 1: "Senin", 2: "Selasa", 3: "Rabu", 4: "Kamis", 5: "Jumat", 6: "Sabtu"}
	idMonthsMap := map[time.Month]string{1: "Jan", 2: "Feb", 3: "Mar", 4: "Apr", 5: "Mei", 6: "Jun", 7: "Jul", 8: "Agt", 9: "Sep", 10: "Okt", 11: "Nov", 12: "Des"}

	var entries []journalPreviewJSON
	for rows.Next() {
		var p models.Preview
		if err := rows.Scan(&p.ID, &p.DayNumber, &p.EntryDate, &p.EntryTime, &p.Location, &p.VerseRef, &p.VerseText, &p.Status,
			&p.PlanID, &p.PlanDay, &p.PlanName, &p.PlanDuration); err != nil {
			http.Error(w, "could not read journals", http.StatusInternalServerError)
			return
		}
		if len(p.VerseText) > 100 {
			p.VerseText = p.VerseText[:100] + "…"
		}
		entryTime := p.EntryTime.In(loc)
		entries = append(entries, journalPreviewJSON{
			ID:         p.ID,
			EntryDate:  p.EntryDate.Format("2006-01-02"),
			EntryTime:  entryTime.Format("15:04"),
			Location:   p.Location,
			VerseRef:   p.VerseRef,
			VerseText:  p.VerseText,
			Status:     p.Status,
			Day:        p.EntryDate.Day(),
			Weekday:    idDaysMap[p.EntryDate.Weekday()],
			MonthShort: idMonthsMap[p.EntryDate.Month()] + " " + strconv.Itoa(p.EntryDate.Year()%100),
			PlanID:       p.PlanID,
			PlanDay:      p.PlanDay,
			PlanName:     p.PlanName,
			PlanDuration: p.PlanDuration,
		})
	}

	hasMore := len(entries) == 16
	if hasMore {
		entries = entries[:15]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"entries": entries, "has_more": hasMore})
}

// ---- GET /journal/new (step 1: ask which verse to reflect on) ----

type planContextForJournal struct {
	PlanID    int64
	PlanName  string
	Day       int
	Duration  int
	VerseRef  string
	VerseText string
	VerseParts string // raw JSON, "" if single verse
	IntroText string
}

type journalNewData struct {
	UserID        int64
	Error         string
	LanguageStyle string
	PlanCtx       *planContextForJournal
}

func (a *App) JournalNewPage(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	var langStyle string
	_ = a.DB.QueryRowContext(r.Context(),
		`SELECT language_style FROM users WHERE id = $1`, userID).Scan(&langStyle)
	if langStyle == "" {
		langStyle = "casual"
	}
	data := journalNewData{UserID: userID, LanguageStyle: langStyle}

	// Plan context: /journal/new?plan_id=X&day=Y pre-fills verse and locks fields
	planID, _ := strconv.ParseInt(r.URL.Query().Get("plan_id"), 10, 64)
	day, _ := strconv.Atoi(r.URL.Query().Get("day"))
	if planID > 0 && day > 0 {
		// If an entry already exists for this plan+day, jump straight to it
		var existingID int64
		err := a.DB.QueryRowContext(r.Context(),
			`SELECT id FROM journal_entries WHERE plan_id = $1 AND plan_day = $2 AND user_id = $3`,
			planID, day, userID).Scan(&existingID)
		if err == nil && existingID > 0 {
			http.Redirect(w, r, "/journal/"+strconv.FormatInt(existingID, 10), http.StatusSeeOther)
			return
		}
		var ctx planContextForJournal
		err = a.DB.QueryRowContext(r.Context(), `
			SELECT p.id, p.name, pd.day_number, p.duration, pd.verse_ref, pd.verse_text, COALESCE(pd.verse_parts::text, ''), pd.intro_text
			FROM plan_days pd
			JOIN plans p ON p.id = pd.plan_id
			WHERE pd.plan_id = $1 AND pd.day_number = $2 AND p.user_id = $3`,
			planID, day, userID,
		).Scan(&ctx.PlanID, &ctx.PlanName, &ctx.Day, &ctx.Duration, &ctx.VerseRef, &ctx.VerseText, &ctx.VerseParts, &ctx.IntroText)
		if err == nil {
			data.PlanCtx = &ctx
		}
	}

	if r.URL.Query().Get("err") == "rate_limit" {
		if langStyle == "formal" {
			data.Error = "Anda sudah membuat 10 journal hari ini. Coba lagi besok. 🙏"
		} else {
			data.Error = "Kamu sudah membuat 10 journal hari ini. Coba lagi besok ya. 🙏"
		}
	}
	a.render(w, "journal_new.html", data)
}

// ---- POST /journal (create entry from verse, then fetch AI background) ----

func (a *App) JournalCreate(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	// Rate limit: max 10 journals per day per user
	var todayCount int
	_ = a.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM journal_entries WHERE user_id = $1 AND entry_date = CURRENT_DATE`,
		userID).Scan(&todayCount)
	if todayCount >= dailyJournalLimit {
		http.Redirect(w, r, "/journal/new?err=rate_limit", http.StatusSeeOther)
		return
	}

	if a.isGuest(r.Context(), userID) {
		var total int
		_ = a.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM journal_entries WHERE user_id = $1`, userID).Scan(&total)
		gs := a.loadGuestSettings(r.Context())
		if total >= gs.MaxJournals {
			http.Redirect(w, r, "/simpan?from=journal", http.StatusSeeOther)
			return
		}
		if total == 0 && a.guestQuotaFull(r.Context(), userID, gs) {
			http.Redirect(w, r, "/simpan?from=quota", http.StatusSeeOther)
			return
		}
	}

	verseRef := r.FormValue("verse_ref")
	verseText := r.FormValue("verse_text")
	location := r.FormValue("location")
	dateStr := r.FormValue("entry_date")
	timeStr := r.FormValue("entry_time")

	// Optional plan link — if present, this entry is tied to a specific day of a plan
	var planIDVal, planDayVal interface{}
	if pid, err := strconv.ParseInt(r.FormValue("plan_id"), 10, 64); err == nil && pid > 0 {
		if pd, err := strconv.Atoi(r.FormValue("plan_day")); err == nil && pd > 0 {
			// Verify the plan belongs to the user before trusting these values
			var owned int
			_ = a.DB.QueryRowContext(r.Context(),
				`SELECT 1 FROM plans WHERE id = $1 AND user_id = $2`, pid, userID).Scan(&owned)
			if owned == 1 {
				// Dedupe: if an entry already exists for this plan+day, redirect to it
				var existingID int64
				if a.DB.QueryRowContext(r.Context(),
					`SELECT id FROM journal_entries WHERE plan_id = $1 AND plan_day = $2 AND user_id = $3`,
					pid, pd, userID).Scan(&existingID) == nil && existingID > 0 {
					http.Redirect(w, r, "/journal/"+strconv.FormatInt(existingID, 10), http.StatusSeeOther)
					return
				}
				planIDVal = pid
				planDayVal = pd
			}
		}
	}

	// Coordinates — optional, only present if user allowed geolocation
	var lat, lon *float64
	if v, err := strconv.ParseFloat(r.FormValue("lat"), 64); err == nil {
		lat = &v
	}
	if v, err := strconv.ParseFloat(r.FormValue("lon"), 64); err == nil {
		lon = &v
	}

	if verseRef == "" {
		http.Error(w, "Referensi ayat wajib diisi", http.StatusBadRequest)
		return
	}
	if verseText == "" {
		http.Error(w, "Teks ayat wajib diisi", http.StatusBadRequest)
		return
	}
	passage := models.IsPassageRef(verseRef)
	maxVerse := 500
	if passage {
		maxVerse = 1500
	}
	if utf8.RuneCountInString(verseText) > maxVerse {
		http.Error(w, "Teks ayat terlalu panjang (maksimal "+strconv.Itoa(maxVerse)+" karakter)", http.StatusBadRequest)
		return
	}

	// Per-verse parts only matter when the text is too long for one card/pin
	// (> 500 chars). They are only trusted if they still match the submitted
	// text (the user may have edited it after fetching).
	var versePartsVal interface{}
	if passage && utf8.RuneCountInString(verseText) > 500 {
		var parts []VersePart
		if json.Unmarshal([]byte(r.FormValue("verse_parts")), &parts) == nil && len(parts) > 1 {
			var texts []string
			for _, p := range parts {
				texts = append(texts, p.Text)
			}
			if strings.Join(texts, " ") == verseText {
				b, _ := json.Marshal(parts)
				versePartsVal = string(b)
			}
		}
	}

	now := time.Now()
	if _, err := time.Parse("2006-01-02", dateStr); err != nil {
		dateStr = now.Format("2006-01-02")
	}
	if _, err := time.Parse("15:04:05", timeStr); err != nil {
		timeStr = now.Format("15:04:05")
	}

	var nextDay int
	err := a.DB.QueryRowContext(r.Context(),
		`SELECT COALESCE(MAX(day_number), 0) + 1 FROM journal_entries WHERE user_id = $1`,
		userID).Scan(&nextDay)
	if err != nil {
		http.Error(w, "could not compute day number", http.StatusInternalServerError)
		return
	}

	var langStyle string
	_ = a.DB.QueryRowContext(r.Context(),
		`SELECT language_style FROM users WHERE id = $1`, userID).Scan(&langStyle)
	if langStyle == "" {
		langStyle = "casual"
	}

	// Ask Claude for historical/original-language background before
	// saving, so the entry is created already populated (matches the
	// "AI: Search Background Peristiwa" step in the flow).
	background, aiErr := a.AI.VerseBackground(r.Context(), verseRef, verseText, langStyle)
	if aiErr != nil {
		// Don't block journaling if the AI call fails (e.g. key not
		// configured yet) — just leave it blank and let the user
		// retry the discussion later.
		background = ""
	}

	var entryID int64
	err = a.DB.QueryRowContext(r.Context(), `
		INSERT INTO journal_entries
			(user_id, day_number, entry_date, entry_time, location, verse_ref, verse_text, ai_background, latitude, longitude, plan_id, plan_day, verse_parts)
		VALUES ($1, $2, $3::date, $4::time, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb)
		RETURNING id`,
		userID, nextDay, dateStr, timeStr, location, verseRef, verseText, background, lat, lon, planIDVal, planDayVal, versePartsVal,
	).Scan(&entryID)
	if err != nil {
		log.Printf("[journal] create entry: %v", err)
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
	Entry            models.JournalEntry
	Messages         []models.JournalMessage
	CompletedCount   int
	LanguageStyle    string
	ShowShareCard    bool
	PendingShareCard bool
	UserName         string
	PlanName         string // "" if entry is not part of a plan
	PlanDuration     int
	VerseParts       []VersePart // nil unless entry is a fetched passage
	CardRef          string      // verse used on the Momen card
	CardText         string
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

	var completedCount int
	_ = a.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM journal_entries WHERE user_id = $1 AND status = 'completed'`,
		entry.UserID).Scan(&completedCount)

	var viewLangStyle, userName string
	var generateShareCard bool
	_ = a.DB.QueryRowContext(r.Context(),
		`SELECT language_style, generate_share_card, name FROM users WHERE id = $1`, entry.UserID).Scan(&viewLangStyle, &generateShareCard, &userName)
	if viewLangStyle == "" {
		viewLangStyle = "casual"
	}

	showShare := entry.Status == "completed" && entry.ShareSummary != ""
	// Only show pending notice if: card feature is on, completed recently (within 10 min), and no summary yet
	pendingShare := entry.Status == "completed" && entry.ShareSummary == "" &&
		generateShareCard && time.Since(entry.UpdatedAt) < 10*time.Minute

	var planName string
	var planDuration int
	if entry.PlanID > 0 {
		_ = a.DB.QueryRowContext(r.Context(),
			`SELECT name, duration FROM plans WHERE id = $1 AND user_id = $2`,
			entry.PlanID, entry.UserID).Scan(&planName, &planDuration)
	}

	var verseParts []VersePart
	if entry.VerseParts != "" {
		_ = json.Unmarshal([]byte(entry.VerseParts), &verseParts)
	}
	cardRef, cardText := entry.VerseRef, entry.VerseText
	if entry.CardVerseRef != "" {
		cardRef, cardText = entry.CardVerseRef, entry.CardVerseText
	} else if len(verseParts) > 1 {
		cardRef, cardText = verseParts[0].Ref, verseParts[0].Text
	}

	a.render(w, "journal_view.html", journalViewData{
		VerseParts: verseParts, CardRef: cardRef, CardText: cardText,
		Entry: entry, Messages: messages, CompletedCount: completedCount,
		LanguageStyle: viewLangStyle, ShowShareCard: showShare, PendingShareCard: pendingShare,
		UserName:  userName,
		PlanName:  planName,
		PlanDuration: planDuration,
	})
}

// passageHint tells the AI that the reference is a multi-verse passage.
func passageHint(ref string) string {
	if models.IsPassageRef(ref) {
		return " — perikop beberapa ayat, bahas sebagai satu kesatuan konteks"
	}
	return ""
}

// ---- POST /journal/{id}/card-verse (pick which verse of a passage goes on the Momen card) ----

func (a *App) JournalCardVerse(w http.ResponseWriter, r *http.Request) {
	entry, ok := a.loadOwnedEntry(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	var parts []VersePart
	if entry.VerseParts == "" || json.Unmarshal([]byte(entry.VerseParts), &parts) != nil {
		http.Error(w, "bukan perikop", http.StatusBadRequest)
		return
	}
	ref := r.FormValue("ref")
	for _, p := range parts {
		if p.Ref == ref {
			if _, err := a.DB.ExecContext(r.Context(),
				`UPDATE journal_entries SET card_verse_ref = $1, card_verse_text = $2 WHERE id = $3`,
				p.Ref, p.Text, entry.ID); err != nil {
				http.Error(w, "could not save", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	http.Error(w, "ayat tidak ditemukan", http.StatusBadRequest)
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
	if utf8.RuneCountInString(reflection) > 500 {
		http.Error(w, "Refleksi terlalu panjang (maksimal 500 karakter)", http.StatusBadRequest)
		return
	}

	_, err := a.DB.ExecContext(r.Context(),
		`UPDATE journal_entries SET reflection = $1, updated_at = now() WHERE id = $2`,
		reflection, entry.ID)
	if err != nil {
		http.Error(w, "could not save reflection", http.StatusInternalServerError)
		return
	}
	if reflection != "" {
		_, _ = a.DB.ExecContext(r.Context(),
			`INSERT INTO journal_messages (entry_id, role, content) VALUES ($1, 'reflection', $2)`,
			entry.ID, reflection)
	}

	http.Redirect(w, r, "/journal/"+strconv.FormatInt(entry.ID, 10), http.StatusSeeOther)
}

// ---- POST /journal/{id}/discuss (the "Ask AI" loop) ----

func (a *App) JournalDiscuss(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
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
	if a.isGuest(r.Context(), userID) {
		var chats int
		_ = a.DB.QueryRowContext(r.Context(), `
			SELECT COUNT(*) FROM journal_messages m JOIN journal_entries e ON e.id = m.entry_id
			WHERE e.user_id = $1 AND m.role = 'user'`, userID).Scan(&chats)
		if chats >= a.loadGuestSettings(r.Context()).MaxChats {
			http.Redirect(w, r, "/simpan?from=chat", http.StatusSeeOther)
			return
		}
	}
	if utf8.RuneCountInString(question) > 500 {
		http.Error(w, "Pesan terlalu panjang (maksimal 500 karakter)", http.StatusBadRequest)
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
		{"user", "Ayat yang sedang saya renungkan (" + entry.VerseRef + ")" + passageHint(entry.VerseRef) + ": " + entry.VerseText},
	}
	for rows.Next() {
		var role, content string
		if err := rows.Scan(&role, &content); err == nil {
			history = append(history, struct{ Role, Content string }{role, content})
		}
	}
	rows.Close()
	history = append(history, struct{ Role, Content string }{"user", question})

	var originalLang bool
	var discussLangStyle string
	_ = a.DB.QueryRowContext(r.Context(),
		`SELECT discuss_original_lang, language_style FROM users WHERE id = $1`, userID).Scan(&originalLang, &discussLangStyle)
	if discussLangStyle == "" {
		discussLangStyle = "casual"
	}

	msgs := toChatMessages(history)
	answer, err := a.AI.Discuss(r.Context(), msgs, originalLang, discussLangStyle)
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
	if utf8.RuneCountInString(step) > 500 {
		http.Error(w, "Langkah praktis terlalu panjang (maksimal 500 karakter)", http.StatusBadRequest)
		return
	}

	_, err := a.DB.ExecContext(r.Context(), `
		UPDATE journal_entries
		SET practical_step = $1, status = 'completed', updated_at = now()
		WHERE id = $2`,
		step, entry.ID)
	if err != nil {
		http.Error(w, "could not complete journal", http.StatusInternalServerError)
		return
	}

	// Build full session context for closing message
	var closing string
	rows, err := a.DB.QueryContext(r.Context(),
		`SELECT role, content FROM journal_messages WHERE entry_id = $1 ORDER BY created_at ASC`,
		entry.ID)
	if err == nil {
		history := []struct{ Role, Content string }{
			{"user", "Ayat yang aku renungkan (" + entry.VerseRef + ")" + passageHint(entry.VerseRef) + ": " + entry.VerseText},
		}
		for rows.Next() {
			var role, content string
			if err := rows.Scan(&role, &content); err == nil {
				history = append(history, struct{ Role, Content string }{role, content})
			}
		}
		rows.Close()
		if entry.Reflection != "" {
			history = append(history, struct{ Role, Content string }{"user", "Refleksiku: " + entry.Reflection})
		}
		if step != "" {
			history = append(history, struct{ Role, Content string }{"user", "Langkah praktis yang aku tulis: " + step})
		}

		var closingLangStyle string
		_ = a.DB.QueryRowContext(r.Context(),
			`SELECT language_style FROM users WHERE id = $1`, entry.UserID).Scan(&closingLangStyle)
		if closingLangStyle == "" {
			closingLangStyle = "casual"
		}

		// Closing message: synchronous (user waits for this)
		closing, _ = a.AI.ClosingMessage(r.Context(), toChatMessages(history), closingLangStyle, ai.TimeOfDay(time.Now()))
		if closing != "" {
			_, _ = a.DB.ExecContext(r.Context(),
				`INSERT INTO journal_messages (entry_id, role, content) VALUES ($1, 'ai', $2)`,
				entry.ID, closing)
		}

		// Share summary: only if user has card generation enabled
		var generateShareCard bool
		_ = a.DB.QueryRowContext(r.Context(),
			`SELECT generate_share_card FROM users WHERE id = $1`, entry.UserID).Scan(&generateShareCard)
		if generateShareCard {
			entryID := entry.ID
			vRef, vText, bg, refl := entry.VerseRef, entry.VerseText, entry.AIBackground, entry.Reflection
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				defer cancel()
				summary, err := a.AI.GenerateShareSummary(ctx, vRef, vText, bg, refl, step)
				if err == nil && summary != "" {
					_, _ = a.DB.ExecContext(ctx,
						`UPDATE journal_entries SET share_summary = $1 WHERE id = $2`,
						summary, entryID)
				}
			}()
		}
	}

	// Plan completion: if this journal belongs to a plan AND all N days are now completed,
	// flip plan.status to 'completed' synchronously (so UI badge updates immediately) and
	// kick off FinalReflection generation in a goroutine (polled via /plan/{id}/final-status).
	if entry.PlanID > 0 {
		a.maybeCompletePlan(r.Context(), entry.PlanID, entry.UserID)
	}

	http.Redirect(w, r, "/journal/"+strconv.FormatInt(entry.ID, 10), http.StatusSeeOther)
}

// maybeCompletePlan checks whether all days of a plan are now completed. If yes, it:
//   - synchronously flips plans.status = 'completed' and sets completed_at
//   - kicks off an async goroutine to generate the final reflection and persist it
//
// If the plan is already marked completed or still has pending days, this is a no-op.
func (a *App) maybeCompletePlan(ctx context.Context, planID, userID int64) {
	var duration int
	var status string
	err := a.DB.QueryRowContext(ctx,
		`SELECT duration, status FROM plans WHERE id = $1 AND user_id = $2`, planID, userID,
	).Scan(&duration, &status)
	if err != nil || status == "completed" {
		return
	}
	var doneCount int
	_ = a.DB.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT plan_day) FROM journal_entries
		WHERE plan_id = $1 AND user_id = $2 AND status = 'completed' AND plan_day IS NOT NULL`,
		planID, userID,
	).Scan(&doneCount)
	if doneCount < duration {
		return
	}
	// All days done — flip status now so the UI reflects completion on next load.
	if _, err := a.DB.ExecContext(ctx,
		`UPDATE plans SET status = 'completed', completed_at = now() WHERE id = $1 AND status = 'active'`,
		planID); err != nil {
		return
	}

	// Pull everything the AI needs, then generate in the background.
	go a.generatePlanFinalReflection(planID, userID)
}

func generatePlanFinalReflectionFor(a *App, planID, userID int64) {
	a.generatePlanFinalReflection(planID, userID)
}

func (a *App) generatePlanFinalReflection(planID, userID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	var planName, langStyle string
	err := a.DB.QueryRowContext(ctx, `
		SELECT p.name, COALESCE(u.language_style, 'casual')
		FROM plans p JOIN users u ON u.id = p.user_id
		WHERE p.id = $1 AND p.user_id = $2`, planID, userID,
	).Scan(&planName, &langStyle)
	if err != nil {
		return
	}

	rows, err := a.DB.QueryContext(ctx, `
		SELECT pd.day_number, pd.verse_ref, pd.verse_text,
		       COALESCE(je.reflection, ''), COALESCE(je.practical_step, '')
		FROM plan_days pd
		LEFT JOIN journal_entries je
		       ON je.plan_id = pd.plan_id
		      AND je.plan_day = pd.day_number
		      AND je.user_id = $2
		WHERE pd.plan_id = $1
		ORDER BY pd.day_number ASC`, planID, userID)
	if err != nil {
		return
	}
	defer rows.Close()
	var days []ai.FinalReflectionDay
	for rows.Next() {
		var d ai.FinalReflectionDay
		if err := rows.Scan(&d.DayNumber, &d.VerseRef, &d.VerseText, &d.Reflection, &d.PracticalStep); err == nil {
			days = append(days, d)
		}
	}

	reflection, err := a.AI.FinalReflection(ctx, planName, days, langStyle)
	if err != nil || reflection == "" {
		return
	}
	_, _ = a.DB.ExecContext(ctx,
		`UPDATE plans SET final_reflection = $1 WHERE id = $2`,
		reflection, planID)

	// Generate public-facing share summary (for gift card)
	summary, err := a.AI.PlanShareSummary(ctx, planName, days)
	if err == nil && summary != "" {
		_, _ = a.DB.ExecContext(ctx,
			`UPDATE plans SET share_summary = $1 WHERE id = $2`,
			summary, planID)
	}
}

// ---- GET /journal/{id}/share-status (polling endpoint, returns JSON) ----

func (a *App) JournalShareStatus(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var summary string
	err = a.DB.QueryRowContext(r.Context(),
		`SELECT share_summary FROM journal_entries WHERE id = $1 AND user_id = $2`,
		id, userID).Scan(&summary)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if summary == "" {
		w.Write([]byte(`{"summary":""}`))
	} else {
		// Minimal safe JSON encode — summary is plain prose, no quotes/backslashes expected,
		// but escape the few characters that would break JSON.
		safe := jsonEscapeString(summary)
		w.Write([]byte(`{"summary":"` + safe + `"}`))
	}
}

func jsonEscapeString(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b = append(b, '\\', '"')
		case '\\':
			b = append(b, '\\', '\\')
		case '\n':
			b = append(b, '\\', 'n')
		case '\r':
			b = append(b, '\\', 'r')
		case '\t':
			b = append(b, '\\', 't')
		default:
			b = append(b, c)
		}
	}
	return string(b)
}

// ---- POST /journal/{id}/delete ----

func (a *App) JournalDelete(w http.ResponseWriter, r *http.Request) {
	entry, ok := a.loadOwnedEntry(w, r)
	if !ok {
		return
	}
	_, err := a.DB.ExecContext(r.Context(),
		`DELETE FROM journal_messages WHERE entry_id = $1`, entry.ID)
	if err != nil {
		http.Error(w, "could not delete messages", http.StatusInternalServerError)
		return
	}
	_, err = a.DB.ExecContext(r.Context(),
		`DELETE FROM journal_entries WHERE id = $1`, entry.ID)
	if err != nil {
		http.Error(w, "could not delete journal", http.StatusInternalServerError)
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
		       verse_ref, verse_text, COALESCE(verse_parts::text, ''), card_verse_ref, card_verse_text,
		       ai_background, reflection, practical_step,
		       status, share_summary, COALESCE(plan_id, 0), COALESCE(plan_day, 0),
		       created_at, updated_at
		FROM journal_entries WHERE id = $1 AND user_id = $2`,
		id, userID,
	).Scan(&e.ID, &e.UserID, &e.DayNumber, &entryDate, &entryTime, &e.Location,
		&e.VerseRef, &e.VerseText, &e.VerseParts, &e.CardVerseRef, &e.CardVerseText, &e.AIBackground, &e.Reflection, &e.PracticalStep,
		&e.Status, &e.ShareSummary, &e.PlanID, &e.PlanDay, &e.CreatedAt, &e.UpdatedAt)
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
