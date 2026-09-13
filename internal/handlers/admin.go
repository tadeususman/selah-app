package handlers

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"golang.org/x/crypto/bcrypt"
	"journalflow/internal/middleware"
)

type adminUser struct {
	ID         int64
	Email      string
	Name       string
	IsAdmin    bool
	CreatedAt  time.Time
	LastActive *time.Time
	Journals   int
}

type adminPageData struct {
	Users      []adminUser
	FlashOK    string
	FlashErr   string
	ResetEmail string
	ResetPW    string
}

func (a *App) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	userID := middleware.UserID(r)
	var isAdmin bool
	err := a.DB.QueryRowContext(r.Context(),
		`SELECT is_admin FROM users WHERE id = $1`, userID).Scan(&isAdmin)
	if err != nil || !isAdmin {
		http.Error(w, "Akses ditolak", http.StatusForbidden)
		return false
	}
	return true
}

type adminHubData struct {
	UserCount    int
	NewThisWeek  int
}

func (a *App) AdminPage(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var data adminHubData
	_ = a.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users`).Scan(&data.UserCount)
	_ = a.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM users WHERE created_at >= NOW() - INTERVAL '7 days'`).Scan(&data.NewThisWeek)
	a.render(w, "admin.html", data)
}

func (a *App) AdminUsersPage(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}

	rows, err := a.DB.QueryContext(r.Context(), `
		SELECT u.id, u.email, u.name, u.is_admin, u.created_at,
		       COUNT(j.id) AS journals,
		       MAX(j.created_at) AS last_active
		FROM users u
		LEFT JOIN journal_entries j ON j.user_id = u.id
		GROUP BY u.id
		ORDER BY u.created_at ASC`)
	if err != nil {
		http.Error(w, "could not load users", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var users []adminUser
	for rows.Next() {
		var u adminUser
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.IsAdmin, &u.CreatedAt, &u.Journals, &u.LastActive); err != nil {
			continue
		}
		users = append(users, u)
	}

	data := adminPageData{Users: users}
	switch r.URL.Query().Get("ok") {
	case "created":
		data.FlashOK = "User berhasil ditambahkan."
	case "deleted":
		data.FlashOK = "User berhasil dihapus."
	case "toggled":
		data.FlashOK = "Role user berhasil diubah."
	case "reset":
		data.ResetEmail = r.URL.Query().Get("email")
		data.ResetPW = r.URL.Query().Get("newpw")
	}
	switch r.URL.Query().Get("err") {
	case "duplicate":
		data.FlashErr = "Email sudah terdaftar."
	case "self":
		data.FlashErr = "Tidak bisa menghapus akun sendiri."
	}
	a.render(w, "admin_users.html", data)
}

func (a *App) AdminCreateUser(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	email := r.FormValue("email")
	name := r.FormValue("name")
	password := r.FormValue("password")
	isAdmin := r.FormValue("is_admin") == "1"

	if email == "" || password == "" {
		http.Redirect(w, r, "/admin/users?err=empty", http.StatusSeeOther)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "could not hash password", http.StatusInternalServerError)
		return
	}

	_, err = a.DB.ExecContext(r.Context(),
		`INSERT INTO users (email, password_hash, name, is_admin) VALUES ($1, $2, $3, $4)`,
		email, string(hash), name, isAdmin)
	if err != nil {
		http.Redirect(w, r, "/admin/users?err=duplicate", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/users?ok=created", http.StatusSeeOther)
}

func (a *App) AdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	targetID, err := strconv.ParseInt(r.FormValue("user_id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	currentUserID := middleware.UserID(r)
	if targetID == currentUserID {
		http.Redirect(w, r, "/admin/users?err=self", http.StatusSeeOther)
		return
	}
	_, _ = a.DB.ExecContext(r.Context(), `DELETE FROM users WHERE id = $1`, targetID)
	http.Redirect(w, r, "/admin/users?ok=deleted", http.StatusSeeOther)
}

func (a *App) AdminResetPassword(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	targetID, err := strconv.ParseInt(r.FormValue("user_id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}

	var email string
	_ = a.DB.QueryRowContext(r.Context(), `SELECT email FROM users WHERE id = $1`, targetID).Scan(&email)

	newPw, err := generateTempPassword()
	if err != nil {
		http.Error(w, "could not generate password", http.StatusInternalServerError)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPw), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "could not hash password", http.StatusInternalServerError)
		return
	}

	_, err = a.DB.ExecContext(r.Context(),
		`UPDATE users SET password_hash = $1 WHERE id = $2`, string(hash), targetID)
	if err != nil {
		http.Error(w, "could not reset password", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r,
		"/admin/users?ok=reset&email="+url.QueryEscape(email)+"&newpw="+url.QueryEscape(newPw),
		http.StatusSeeOther)
}

func generateTempPassword() (string, error) {
	const chars = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i, v := range b {
		b[i] = chars[int(v)%len(chars)]
	}
	return string(b), nil
}

func (a *App) AdminToggleAdmin(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	targetID, err := strconv.ParseInt(r.FormValue("user_id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	_, _ = a.DB.ExecContext(r.Context(),
		`UPDATE users SET is_admin = NOT is_admin WHERE id = $1`, targetID)
	http.Redirect(w, r, "/admin/users?ok=toggled", http.StatusSeeOther)
}

type adminAIPeriodStats struct {
	From          string
	To            string
	Total         int
	InputTokens   int
	OutputTokens  int
	CostUSD       float64
}

type adminAIUserStat struct {
	Name        string
	Journals    int
	AIResponses int
	LastActive  string
}

type adminAIDailyStat struct {
	Date  string
	Count int
}

type adminAIStatsData struct {
	Provider    string
	StatsJSON   string
	Error       string
	UserStats   []adminAIUserStat
	DailyStats  []adminAIDailyStat
	Period      *adminAIPeriodStats
}

func (a *App) AdminAIStats(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	data := adminAIStatsData{Provider: a.AI.Provider()}

	if a.AI.Provider() == "qwen" {
		a.loadQwenStats(r, &data, from, to)
	} else {
		a.loadBridgeStats(r, &data, from, to)
	}

	// Per-user stats dari journal_messages (berlaku untuk semua provider)
	rows, err := a.DB.QueryContext(r.Context(), `
		SELECT u.name,
		       COUNT(DISTINCT j.id) AS journals,
		       COUNT(CASE WHEN m.role = 'ai' THEN 1 END) AS ai_responses,
		       MAX(j.created_at) AS last_active
		FROM users u
		LEFT JOIN journal_entries j ON j.user_id = u.id
		LEFT JOIN journal_messages m ON m.entry_id = j.id
		GROUP BY u.id, u.name
		ORDER BY ai_responses DESC
		LIMIT 10`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var s adminAIUserStat
			var lastActive *time.Time
			if rows.Scan(&s.Name, &s.Journals, &s.AIResponses, &lastActive) == nil {
				if lastActive != nil {
					s.LastActive = lastActive.In(time.FixedZone("WIB", 7*3600)).Format("2 Jan, 15:04")
				} else {
					s.LastActive = "—"
				}
				data.UserStats = append(data.UserStats, s)
			}
		}
	}

	a.render(w, "admin_ai.html", data)
}

func (a *App) loadBridgeStats(r *http.Request, data *adminAIStatsData, from, to string) {
	raw, err := a.AI.Stats(r.Context(), "", "")
	if err != nil {
		data.Error = "Bridge tidak bisa dihubungi: " + err.Error()
	} else {
		var pretty interface{}
		if json.Unmarshal(raw, &pretty) == nil {
			if b, e := json.MarshalIndent(pretty, "", "  "); e == nil {
				data.StatsJSON = string(b)
			}
		}
		if data.StatsJSON == "" {
			data.StatsJSON = string(raw)
		}
	}

	if from != "" || to != "" {
		praw, perr := a.AI.Stats(r.Context(), from, to)
		if perr == nil {
			var ps struct {
				Total        int     `json:"total"`
				InputTokens  int     `json:"total_input_tokens"`
				OutputTokens int     `json:"total_output_tokens"`
				CostUSD      float64 `json:"total_cost_usd"`
			}
			if json.Unmarshal(praw, &ps) == nil {
				data.Period = &adminAIPeriodStats{
					From: from, To: to,
					Total: ps.Total, InputTokens: ps.InputTokens,
					OutputTokens: ps.OutputTokens, CostUSD: ps.CostUSD,
				}
			}
		}
	}

	drows, err := a.DB.QueryContext(r.Context(), `
		SELECT to_char(m.created_at AT TIME ZONE 'Asia/Jakarta', 'YYYY-MM-DD') AS day,
		       COUNT(*) AS cnt
		FROM journal_messages m
		WHERE m.role = 'ai'
		  AND m.created_at >= NOW() - INTERVAL '12 months'
		GROUP BY day ORDER BY day ASC`)
	if err == nil {
		defer drows.Close()
		for drows.Next() {
			var s adminAIDailyStat
			if drows.Scan(&s.Date, &s.Count) == nil {
				data.DailyStats = append(data.DailyStats, s)
			}
		}
	}
}

func (a *App) loadQwenStats(r *http.Request, data *adminAIStatsData, from, to string) {
	// Period filter
	if from != "" || to != "" {
		q := `SELECT COALESCE(COUNT(*),0), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0), COALESCE(SUM(cost_usd),0)
		      FROM ai_usage_log WHERE 1=1`
		args := []any{}
		if from != "" {
			args = append(args, from)
			q += " AND created_at >= $" + fmt.Sprintf("%d", len(args)) + "::date"
		}
		if to != "" {
			args = append(args, to)
			q += " AND created_at < ($" + fmt.Sprintf("%d", len(args)) + "::date + INTERVAL '1 day')"
		}
		var total, in, out int
		var cost float64
		if a.DB.QueryRowContext(r.Context(), q, args...).Scan(&total, &in, &out, &cost) == nil {
			data.Period = &adminAIPeriodStats{
				From: from, To: to,
				Total: total, InputTokens: in, OutputTokens: out, CostUSD: cost,
			}
		}
	}

	// Daily chart dari ai_usage_log
	drows, err := a.DB.QueryContext(r.Context(), `
		SELECT to_char(created_at AT TIME ZONE 'Asia/Jakarta', 'YYYY-MM-DD') AS day,
		       COUNT(*) AS cnt
		FROM ai_usage_log
		WHERE created_at >= NOW() - INTERVAL '12 months'
		GROUP BY day ORDER BY day ASC`)
	if err == nil {
		defer drows.Close()
		for drows.Next() {
			var s adminAIDailyStat
			if drows.Scan(&s.Date, &s.Count) == nil {
				data.DailyStats = append(data.DailyStats, s)
			}
		}
	}
}
