package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type adminSettingsData struct {
	Provider   string
	Model      string
	APIKeySet  bool
	TestResult string // "ok", "fail", or ""
	TestMsg    string
}

func (a *App) AdminSettings(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	data := adminSettingsData{
		Provider:  a.AI.Provider(),
		Model:     a.AI.Model(),
		APIKeySet: a.AI.APIKeySet(),
	}
	data.TestResult = r.URL.Query().Get("test") // "ok", "fail", or ""
	data.TestMsg = r.URL.Query().Get("msg")
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
