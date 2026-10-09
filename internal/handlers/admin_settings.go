package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type aiUsageSummary struct {
	TotalCostUSD      float64
	MonthCostUSD      float64
	TotalInputTokens  int
	TotalOutputTokens int
	LastUsed          *time.Time
}

type adminSettingsData struct {
	Provider    string
	Model       string
	APIKeySet   bool
	TestResult  string
	TestMsg     string
	Usage       *aiUsageSummary
	Guest       guestSettings
	GuestToday  int
	GuestActive int
	SavedOK     bool
	SavedErr    bool
}

func (a *App) AdminSettings(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	data := adminSettingsData{
		Provider:   a.AI.Provider(),
		Model:      a.AI.Model(),
		APIKeySet:  a.AI.APIKeySet(),
		TestResult: r.URL.Query().Get("test"),
		TestMsg:    r.URL.Query().Get("msg"),
	}

	var u aiUsageSummary
	_ = a.DB.QueryRowContext(r.Context(), `
		SELECT
			COALESCE(SUM(cost_usd), 0),
			COALESCE(SUM(CASE WHEN created_at >= date_trunc('month', NOW()) THEN cost_usd ELSE 0 END), 0),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			MAX(created_at)
		FROM ai_usage_log
	`).Scan(&u.TotalCostUSD, &u.MonthCostUSD, &u.TotalInputTokens, &u.TotalOutputTokens, &u.LastUsed)
	data.Usage = &u

	data.Guest = a.loadGuestSettings(r.Context())
	_ = a.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(DISTINCT u.id) FROM users u JOIN journal_entries j ON j.user_id = u.id
		 WHERE u.is_guest AND j.created_at > now() - interval '24 hours'`).Scan(&data.GuestToday)
	_ = a.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE is_guest`).Scan(&data.GuestActive)
	data.SavedOK = r.URL.Query().Get("saved") == "guest"
	data.SavedErr = r.URL.Query().Get("err") == "guest"

	a.render(w, "admin_settings.html", data)
}

func (a *App) AdminSettingsTest(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	start := time.Now()
	_, err := a.AI.Ping(r.Context())
	ms := time.Since(start).Milliseconds()
	if err != nil {
		http.Redirect(w, r,
			"/admin/settings?test=fail&msg="+url.QueryEscape(err.Error()),
			http.StatusSeeOther)
		return
	}
	http.Redirect(w, r,
		"/admin/settings?test=ok&msg="+url.QueryEscape(fmt.Sprintf("%d ms", ms)),
		http.StatusSeeOther)
}

// AdminGuestSettingsSave stores the "Coba dulu" limits. Each number is
// clamped so a typo can't open the trial to unlimited AI spend by accident
// (0 is only valid for the two per-day caps, where it means "no limit").
func (a *App) AdminGuestSettingsSave(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	num := func(name string, min, max int) (int, bool) {
		n, err := strconv.Atoi(strings.TrimSpace(r.FormValue(name)))
		return n, err == nil && n >= min && n <= max
	}
	var gs guestSettings
	var ok1, ok2, ok3, ok4 bool
	gs.Enabled = r.FormValue("enabled") == "1"
	gs.MaxJournals, ok1 = num("max_journals", 1, 20)
	gs.MaxChats, ok2 = num("max_chats", 1, 100)
	gs.PerIPPerDay, ok3 = num("per_ip_day", 0, 10000)
	gs.GlobalDay, ok4 = num("global_day", 0, 100000)
	if !(ok1 && ok2 && ok3 && ok4) || a.saveGuestSettings(r.Context(), gs) != nil {
		http.Redirect(w, r, "/admin/settings?err=guest", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/settings?saved=guest", http.StatusSeeOther)
}
