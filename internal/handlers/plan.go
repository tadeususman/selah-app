package handlers

import (
	"context"
	"database/sql"
	"encoding/xml"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"journalflow/internal/ai"
	"journalflow/internal/middleware"
	"journalflow/internal/models"
)

const planSituationLimit = 500
const planNameLimit = 60

// ---- GET /plan (list all plans — active and completed) ----

type planListData struct {
	Active    []models.PlanPreview
	Completed []models.PlanPreview
}

func (a *App) PlanList(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	rows, err := a.DB.QueryContext(r.Context(), `
		SELECT p.id, p.name, p.cover_text, p.duration, p.status, p.created_at, p.completed_at,
		       COALESCE(COUNT(DISTINCT je.plan_day) FILTER (
		           WHERE je.status = 'completed' AND je.plan_day IS NOT NULL
		       ), 0) AS done_count
		FROM plans p
		LEFT JOIN journal_entries je
		       ON je.plan_id = p.id AND je.user_id = p.user_id
		WHERE p.user_id = $1
		GROUP BY p.id
		ORDER BY (p.status = 'active') DESC, p.created_at DESC`,
		userID)
	if err != nil {
		http.Error(w, "could not load plans", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var active, completed []models.PlanPreview
	for rows.Next() {
		var p models.PlanPreview
		var completedAt sql.NullTime
		if err := rows.Scan(&p.ID, &p.Name, &p.CoverText, &p.Duration, &p.Status, &p.CreatedAt, &completedAt, &p.DoneCount); err != nil {
			http.Error(w, "could not read plans", http.StatusInternalServerError)
			return
		}
		if completedAt.Valid {
			p.CompletedAt = completedAt.Time
		}
		if p.Status == "active" {
			active = append(active, p)
		} else {
			completed = append(completed, p)
		}
	}
	a.render(w, "plan_list.html", planListData{Active: active, Completed: completed})
}

// ---- GET /plan/new (form: user tells us the situation/theme) ----

type planNewData struct {
	Error     string
	Situation string
}

func (a *App) PlanNew(w http.ResponseWriter, r *http.Request) {
	a.render(w, "plan_new.html", planNewData{})
}

// ---- POST /plan (generate via AI, fetch verses from SABDA, persist) ----

func (a *App) PlanCreate(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	situation := strings.TrimSpace(r.FormValue("situation"))
	if situation == "" {
		a.render(w, "plan_new.html", planNewData{Error: "Ceritakan dulu apa yang ingin kamu renungkan."})
		return
	}
	if utf8.RuneCountInString(situation) > planSituationLimit {
		a.render(w, "plan_new.html", planNewData{
			Error:     fmt.Sprintf("Terlalu panjang (maksimal %d karakter).", planSituationLimit),
			Situation: situation,
		})
		return
	}

	location := strings.TrimSpace(r.FormValue("location"))
	var lat, lon *float64
	if v, err := strconv.ParseFloat(r.FormValue("lat"), 64); err == nil {
		lat = &v
	}
	if v, err := strconv.ParseFloat(r.FormValue("lon"), 64); err == nil {
		lon = &v
	}

	// Rate limit: max 3 active plans per user to prevent runaway AI usage
	var activeCount int
	_ = a.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM plans WHERE user_id = $1 AND status = 'active'`, userID).Scan(&activeCount)
	if activeCount >= 3 {
		a.render(w, "plan_new.html", planNewData{
			Error:     "Kamu sudah punya 3 rencana aktif. Selesaikan atau hapus salah satu dulu ya.",
			Situation: situation,
		})
		return
	}

	var langStyle string
	_ = a.DB.QueryRowContext(r.Context(),
		`SELECT COALESCE(language_style, 'casual') FROM users WHERE id = $1`, userID).Scan(&langStyle)

	draft, err := a.AI.GeneratePlan(r.Context(), situation, langStyle)
	if err != nil {
		log.Printf("[plan/create] GeneratePlan error: %v", err)
		a.render(w, "plan_new.html", planNewData{
			Error:     "Teman Selah sedang tidak bisa menyusun rencana. Coba lagi sebentar.",
			Situation: situation,
		})
		return
	}

	type dayWithText struct {
		Ref, Text, Intro string
	}
	days := make([]dayWithText, 0, len(draft.Days))
	for _, d := range draft.Days {
		text, err := fetchVerseFromSabda(r.Context(), d.VerseRef)
		if err != nil || text == "" {
			log.Printf("[plan/create] verse fetch failed for %q: %v", d.VerseRef, err)
			a.render(w, "plan_new.html", planNewData{
				Error:     "Beberapa ayat tidak bisa diambil. Coba buat rencana lagi dengan tema serupa.",
				Situation: situation,
			})
			return
		}
		days = append(days, dayWithText{Ref: d.VerseRef, Text: text, Intro: d.IntroText})
	}

	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var planID int64
	err = tx.QueryRowContext(r.Context(),
		`INSERT INTO plans (user_id, name, cover_text, duration, location, latitude, longitude)
		 VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7) RETURNING id`,
		userID, draft.Name, draft.CoverText, draft.Duration, location, lat, lon).Scan(&planID)
	if err != nil {
		log.Printf("[plan/create] insert plan: %v", err)
		http.Error(w, "could not create plan", http.StatusInternalServerError)
		return
	}
	for i, d := range days {
		if _, err := tx.ExecContext(r.Context(),
			`INSERT INTO plan_days (plan_id, day_number, verse_ref, verse_text, intro_text) VALUES ($1, $2, $3, $4, $5)`,
			planID, i+1, d.Ref, d.Text, d.Intro); err != nil {
			log.Printf("[plan/create] insert plan_days: %v", err)
			http.Error(w, "could not create plan days", http.StatusInternalServerError)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		log.Printf("[plan/create] commit: %v", err)
		http.Error(w, "could not save plan", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/plan?created="+strconv.FormatInt(planID, 10), http.StatusSeeOther)
}

// ---- GET /plan/{id} (cover + swipe slider per day + static celebration) ----

type planDayView struct {
	DayNumber    int
	VerseRef     string
	VerseText    string
	IntroText    string
	Status       string // "" (pending) or "completed"
	EntryID      int64  // 0 if no journal entry yet
	Reflection   string
	ShareSummary string
}

type planDetailData struct {
	Plan      models.Plan
	Days      []planDayView
	DoneCount int
	AllDone   bool
	StartIdx  int
	UserName  string
}

func (a *App) PlanDetail(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	var p models.Plan
	var completedAt sql.NullTime
	err = a.DB.QueryRowContext(r.Context(), `
		SELECT id, user_id, name, cover_text, duration, status, final_reflection,
		       COALESCE(share_summary, ''), COALESCE(location, ''), created_at, completed_at
		FROM plans WHERE id = $1 AND user_id = $2`,
		id, userID,
	).Scan(&p.ID, &p.UserID, &p.Name, &p.CoverText, &p.Duration, &p.Status, &p.FinalReflection, &p.ShareSummary, &p.Location, &p.CreatedAt, &completedAt)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if completedAt.Valid {
		p.CompletedAt = &completedAt.Time
	}

	rows, err := a.DB.QueryContext(r.Context(), `
		SELECT pd.day_number, pd.verse_ref, pd.verse_text, pd.intro_text,
		       COALESCE(je.id, 0), COALESCE(je.status, ''),
		       COALESCE(je.reflection, ''), COALESCE(je.share_summary, '')
		FROM plan_days pd
		LEFT JOIN journal_entries je
		       ON je.plan_id = pd.plan_id
		      AND je.plan_day = pd.day_number
		      AND je.user_id = $2
		WHERE pd.plan_id = $1
		ORDER BY pd.day_number ASC`, p.ID, userID)
	if err != nil {
		http.Error(w, "could not load days", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var days []planDayView
	doneCount := 0
	firstPending := -1
	for rows.Next() {
		var d planDayView
		if err := rows.Scan(&d.DayNumber, &d.VerseRef, &d.VerseText, &d.IntroText,
			&d.EntryID, &d.Status, &d.Reflection, &d.ShareSummary); err != nil {
			http.Error(w, "could not read days", http.StatusInternalServerError)
			return
		}
		if d.Status == "completed" {
			doneCount++
		} else if firstPending == -1 {
			firstPending = len(days)
		}
		days = append(days, d)
	}
	allDone := doneCount == len(days) && len(days) > 0

	// Slide layout: [cover][day 1][day 2]...[day N][celebration?]
	// Initial scroll target:
	// - 0 (cover) if nothing done yet — give user context
	// - firstPending+1 if some days done — jump to today's task
	// - len(days)+1 (celebration) if everything done
	var startIdx int
	switch {
	case allDone:
		startIdx = len(days) + 1
	case doneCount == 0:
		startIdx = 0
	default:
		startIdx = firstPending + 1
	}

	var userName string
	_ = a.DB.QueryRowContext(r.Context(), `SELECT name FROM users WHERE id = $1`, userID).Scan(&userName)

	a.render(w, "plan_detail.html", planDetailData{
		Plan: p, Days: days, DoneCount: doneCount, AllDone: allDone, StartIdx: startIdx,
		UserName: userName,
	})
}

// ---- GET /plan/{id}/final-status (polled by celebration slide) ----

func (a *App) PlanFinalStatus(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var reflection, status string
	err = a.DB.QueryRowContext(r.Context(),
		`SELECT COALESCE(final_reflection, ''), status FROM plans WHERE id = $1 AND user_id = $2`,
		id, userID,
	).Scan(&reflection, &status)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(`{"status":"` + status + `","reflection":` + jsonStringLiteral(reflection) + `}`))
}

// jsonStringLiteral escapes a string into a JSON string literal (including surrounding quotes).
// We avoid pulling encoding/json for a single field.
func jsonStringLiteral(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// ---- POST /plan/{id}/generate-share-summary ----

func (a *App) PlanGenerateShareSummary(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var name string
	err = a.DB.QueryRowContext(r.Context(), `
		SELECT p.name FROM plans p
		WHERE p.id = $1 AND p.user_id = $2 AND p.status = 'completed'`,
		id, userID,
	).Scan(&name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	rows, err := a.DB.QueryContext(r.Context(), `
		SELECT pd.day_number, pd.verse_ref, pd.verse_text,
		       COALESCE(je.reflection, ''), COALESCE(je.practical_step, '')
		FROM plan_days pd
		LEFT JOIN journal_entries je
		       ON je.plan_id = pd.plan_id
		      AND je.plan_day = pd.day_number
		      AND je.user_id = $2
		WHERE pd.plan_id = $1
		ORDER BY pd.day_number ASC`, id, userID)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
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
	summary, err := a.AI.PlanShareSummary(r.Context(), name, days)
	if err != nil || summary == "" {
		http.Error(w, "AI error", http.StatusInternalServerError)
		return
	}
	_, _ = a.DB.ExecContext(r.Context(),
		`UPDATE plans SET share_summary = $1 WHERE id = $2`, summary, id)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true,"summary":` + jsonStringLiteral(summary) + `}`))
}

// ---- POST /plan/{id}/regenerate-final ----

func (a *App) PlanRegenerateFinal(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// verify ownership and completed status
	var status string
	err = a.DB.QueryRowContext(r.Context(),
		`SELECT status FROM plans WHERE id = $1 AND user_id = $2`, id, userID,
	).Scan(&status)
	if err != nil || status != "completed" {
		http.Error(w, "not found or not completed", http.StatusNotFound)
		return
	}
	// clear existing reflection so polling UI picks up pending state
	_, _ = a.DB.ExecContext(r.Context(),
		`UPDATE plans SET final_reflection = NULL WHERE id = $1`, id)
	go generatePlanFinalReflectionFor(a, id, userID)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// ---- POST /plan/{id}/day/{day}/regenerate-intro ----
// Temporary (iterate on intro quality) — kick AI to rewrite one day's intro_text.

func (a *App) PlanRegenerateIntro(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	idStr := chi.URLParam(r, "id")
	dayStr := chi.URLParam(r, "day")
	planID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	targetDay, err := strconv.Atoi(dayStr)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Verify ownership + load plan name/cover + langStyle
	var planName, coverText, langStyle string
	err = a.DB.QueryRowContext(r.Context(),
		`SELECT p.name, p.cover_text, COALESCE(u.language_style, 'casual')
		 FROM plans p JOIN users u ON u.id = p.user_id
		 WHERE p.id = $1 AND p.user_id = $2`, planID, userID,
	).Scan(&planName, &coverText, &langStyle)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Load all days (needed as context for flow preservation)
	rows, err := a.DB.QueryContext(r.Context(),
		`SELECT day_number, verse_ref, verse_text, intro_text FROM plan_days
		 WHERE plan_id = $1 ORDER BY day_number ASC`, planID)
	if err != nil {
		http.Error(w, "could not load plan days", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var days []ai.PlanDayContext
	for rows.Next() {
		var d ai.PlanDayContext
		if err := rows.Scan(&d.DayNumber, &d.VerseRef, &d.VerseText, &d.IntroText); err == nil {
			days = append(days, d)
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	newIntro, err := a.AI.RegeneratePlanDayIntro(ctx, planName, coverText, langStyle, targetDay, days)
	if err != nil || newIntro == "" {
		log.Printf("[plan/regenerate-intro] AI error (plan=%d day=%d): %v", planID, targetDay, err)
		http.Error(w, "Teman Selah sedang tidak bisa menulis ulang. Coba lagi sebentar.", http.StatusBadGateway)
		return
	}

	if _, err := a.DB.ExecContext(r.Context(),
		`UPDATE plan_days SET intro_text = $1 WHERE plan_id = $2 AND day_number = $3`,
		newIntro, planID, targetDay); err != nil {
		http.Error(w, "could not save new intro", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(`{"ok":true,"intro_text":` + jsonStringLiteral(newIntro) + `}`))
}

// ---- POST /plan/{id}/rename (inline edit nama plan) ----

func (a *App) PlanRename(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "Nama tidak boleh kosong.", http.StatusBadRequest)
		return
	}
	if utf8.RuneCountInString(name) > planNameLimit {
		http.Error(w, fmt.Sprintf("Nama terlalu panjang (maks %d karakter).", planNameLimit), http.StatusBadRequest)
		return
	}
	res, err := a.DB.ExecContext(r.Context(),
		`UPDATE plans SET name = $1 WHERE id = $2 AND user_id = $3`, name, id, userID)
	if err != nil {
		http.Error(w, "could not rename plan", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(`{"ok":true,"name":` + jsonStringLiteral(name) + `}`))
}

// ---- POST /plan/{id}/delete ----

func (a *App) PlanDelete(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// DELETE cascades to plan_days (FK CASCADE); journal_entries.plan_id/plan_day
	// become NULL via FK ON DELETE SET NULL — journals stay intact.
	res, err := a.DB.ExecContext(r.Context(),
		`DELETE FROM plans WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		log.Printf("[plan/delete] db error: %v", err)
		http.Error(w, "could not delete plan", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/plan", http.StatusSeeOther)
}

// fetchVerseFromSabda fetches the verse text for a single reference from alkitab.sabda.org (TB).
func fetchVerseFromSabda(ctx context.Context, ref string) (string, error) {
	apiURL := "https://alkitab.sabda.org/api/passage.php?passage=" + url.QueryEscape(ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var parsed sabdaXML
	if err := xml.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", err
	}
	var parts []string
	for _, v := range parsed.Book.Chapter.Verses.Verse {
		t := strings.TrimSpace(v.Text)
		if t != "" {
			parts = append(parts, t)
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("ayat tidak ditemukan: %s", ref)
	}
	return strings.Join(parts, " "), nil
}
