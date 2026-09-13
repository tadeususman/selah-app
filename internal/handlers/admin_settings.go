package handlers

import (
	"fmt"
	"net/http"
	"net/url"
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
	Provider   string
	Model      string
	APIKeySet  bool
	TestResult string
	TestMsg    string
	Usage      *aiUsageSummary
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
