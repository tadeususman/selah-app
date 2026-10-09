package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"journalflow/internal/middleware"
)

const (
	reportNoteMax    = 300
	reportMaxPerDay  = 20
	reportContextMax = 1000 // karakter pertanyaan user yang disimpan sebagai konteks
)

var reportReasons = map[string]string{
	"tidak_pantas":   "Tidak pantas atau menyinggung",
	"keliru":         "Keliru (ayat/ajaran/fakta)",
	"tidak_nyambung": "Tidak nyambung dengan pertanyaan",
	"lainnya":        "Lainnya",
}

func reportJSON(w http.ResponseWriter, code int, v map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// AIReportCreate lets a user flag one Teman Selah answer. The answer text and
// the user's preceding message are snapshotted so the report can be judged
// later; everything is removed with the journal/account (ON DELETE CASCADE).
func (a *App) AIReportCreate(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	if err := r.ParseForm(); err != nil {
		reportJSON(w, http.StatusBadRequest, map[string]any{"error": "Permintaan tidak valid."})
		return
	}
	msgID, err := strconv.ParseInt(r.FormValue("message_id"), 10, 64)
	reason := r.FormValue("reason")
	if _, ok := reportReasons[reason]; err != nil || !ok {
		reportJSON(w, http.StatusBadRequest, map[string]any{"error": "Pilih alasan laporan."})
		return
	}
	note := strings.TrimSpace(r.FormValue("note"))
	if utf8.RuneCountInString(note) > reportNoteMax {
		reportJSON(w, http.StatusBadRequest, map[string]any{"error": "Catatan terlalu panjang (maksimal 300 karakter)."})
		return
	}

	var recent int
	_ = a.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM ai_reports WHERE user_id = $1 AND created_at > now() - interval '24 hours'`, userID).Scan(&recent)
	if recent >= reportMaxPerDay {
		reportJSON(w, http.StatusTooManyRequests, map[string]any{"error": "Terlalu banyak laporan hari ini. Coba lagi besok."})
		return
	}

	// the message must be an AI answer in one of this user's own journals
	var entryID int64
	var aiText string
	var createdAt time.Time
	err = a.DB.QueryRowContext(r.Context(), `
		SELECT m.entry_id, m.content, m.created_at
		FROM journal_messages m JOIN journal_entries e ON e.id = m.entry_id
		WHERE m.id = $1 AND e.user_id = $2 AND m.role = 'ai'`, msgID, userID).Scan(&entryID, &aiText, &createdAt)
	if err != nil {
		reportJSON(w, http.StatusNotFound, map[string]any{"error": "Jawaban tidak ditemukan."})
		return
	}
	var userText string
	_ = a.DB.QueryRowContext(r.Context(), `
		SELECT content FROM journal_messages
		WHERE entry_id = $1 AND created_at < $2 AND role IN ('user', 'reflection')
		ORDER BY created_at DESC, id DESC LIMIT 1`, entryID, createdAt).Scan(&userText)
	if utf8.RuneCountInString(userText) > reportContextMax {
		userText = string([]rune(userText)[:reportContextMax])
	}

	res, err := a.DB.ExecContext(r.Context(), `
		INSERT INTO ai_reports (user_id, entry_id, message_id, reason, note, ai_text, user_text)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (user_id, message_id) DO NOTHING`,
		userID, entryID, msgID, reason, note, aiText, userText)
	if err != nil {
		reportJSON(w, http.StatusInternalServerError, map[string]any{"error": "Laporan belum bisa dikirim. Coba lagi."})
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		a.Notify.Send("Selah: laporan jawaban AI\nAlasan: " + reportReasons[reason] + "\nBuka Admin → Laporan AI untuk meninjau.")
	}
	reportJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type adminReport struct {
	ID         int64
	Reason     string
	Note       string
	AIText     string
	UserText   string
	Status     string
	Reporter   string
	CreatedAt  *time.Time
	ReviewedAt *time.Time
}

type adminReportsData struct {
	Open     []adminReport
	Resolved []adminReport
	Flash    string
}

func (a *App) loadReports(r *http.Request, status string, limit int) []adminReport {
	rows, err := a.DB.QueryContext(r.Context(), `
		SELECT p.id, p.reason, p.note, p.ai_text, p.user_text, p.status,
		       CASE WHEN u.is_guest THEN 'Tamu' ELSE COALESCE(NULLIF(u.name, ''), u.email, u.phone, '') END,
		       p.created_at, p.reviewed_at
		FROM ai_reports p JOIN users u ON u.id = p.user_id
		WHERE p.status = $1 ORDER BY p.created_at DESC LIMIT $2`, status, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []adminReport
	for rows.Next() {
		var x adminReport
		if rows.Scan(&x.ID, &x.Reason, &x.Note, &x.AIText, &x.UserText, &x.Status, &x.Reporter, &x.CreatedAt, &x.ReviewedAt) == nil {
			if label, ok := reportReasons[x.Reason]; ok {
				x.Reason = label
			}
			out = append(out, x)
		}
	}
	return out
}

func (a *App) AdminReportsPage(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	data := adminReportsData{
		Open:     a.loadReports(r, "open", 100),
		Resolved: a.loadReports(r, "resolved", 20),
	}
	if r.URL.Query().Get("ok") == "resolved" {
		data.Flash = "Laporan ditandai selesai."
	}
	a.render(w, "admin_reports.html", data)
}

func (a *App) AdminReportResolve(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	_, _ = a.DB.ExecContext(r.Context(),
		`UPDATE ai_reports SET status = 'resolved', reviewed_at = now() WHERE id = $1 AND status = 'open'`, id)
	http.Redirect(w, r, "/admin/reports?ok=resolved", http.StatusSeeOther)
}
