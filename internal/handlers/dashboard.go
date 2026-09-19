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
	WelcomeMsg    string
	Entries       []models.Preview
	ShareCards    []shareCardPreview
	LanguageStyle string
}

// pick returns variants[seed % len(variants)] — rotates daily, consistent within a day.
func pick(variants []string, seed int) string {
	return variants[seed%len(variants)]
}

func welcomeMsg(condition, lastVerseRef string, seed int) string {
	switch condition {
	case "new":
		return pick([]string{
			"Ini ruang jedamu. Mulai kapan saja, tidak ada yang terlambat.",
			"Selamat datang di selah. Ruang ini menantimu.",
			"Tidak perlu sempurna untuk memulai. Cukup hadir.",
			"Ruang ini dibuat untukmu. Mulai dari mana saja.",
		}, seed)

	case "done":
		return pick([]string{
			"Renungan hari ini sudah selesai. Semoga firman-Nya tinggal sepanjang hari.",
			"Sudah meluangkan waktu untuk yang paling penting hari ini.",
			"Sesi hari ini sudah tertulis. Bawa pesannya ke mana pun kamu pergi.",
			"Firman hari ini sudah kamu renungkan — itu bekal yang cukup.",
			"Sudah memulai hari dengan yang terbaik. Semoga terus terasa.",
			"Renungan hari ini sudah ada. Semoga kata-kata itu menemanimu.",
			"Satu langkah kecil yang berarti — renungan hari ini sudah selesai.",
			"Waktu yang kamu sisihkan tadi tidak sia-sia.",
		}, seed)

	case "yesterday":
		ref := lastVerseRef
		if ref == "" {
			ref = "kemarin"
		}
		return pick([]string{
			"Kemarin kamu merenungkan " + ref + ". Siap untuk hari ini?",
			"Renungan terakhirmu dari " + ref + ". Hari ini mau bawa ayat apa?",
			"Kemarin bersama " + ref + ". Firman selalu ada untuk hari yang baru.",
			ref + " menemanimu kemarin. Hari ini ruang ini terbuka lagi.",
			"Semoga " + ref + " masih terasa hari ini.",
		}, seed)

	case "gap":
		return pick([]string{
			"Senang kamu kembali ke sini.",
			"Tidak apa-apa jeda sebentar. Ruang ini selalu terbuka.",
			"Selamat kembali. Tidak pernah terlambat untuk mulai lagi.",
			"Selah menunggumu. Senang kamu kembali.",
			"Kembali lagi — ruang ini tidak kemana-mana.",
			"Setiap kembali itu berarti. Senang kamu di sini.",
			"Tidak ada yang tertinggal. Kamu bisa mulai dari sini.",
			"Ruang ini selalu ada, kapan pun kamu siap.",
			"Yang penting kamu kembali. Itu sudah cukup.",
		}, seed)

	case "long":
		return pick([]string{
			"Sudah beberapa waktu. Ruang ini senang kamu kembali.",
			"Lama tidak bertemu. Yang penting sekarang kamu di sini.",
			"Apapun yang terjadi, selamat kembali. Mulai saja dari sini.",
			"Selah masih di sini. Tidak ada yang tertinggal.",
			"Kamu kembali — dan itu hal yang baik.",
			"Tidak ada terlambat di sini. Selamat kembali.",
			"Jeda panjang tidak apa-apa. Yang penting kamu di sini sekarang.",
			"Ruang ini tidak berubah. Selamat kembali, kapan pun itu.",
		}, seed)
	}
	return ""
}

func (a *App) Dashboard(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserID(r)
	wib, _ := time.LoadLocation("Asia/Jakarta")
	now := time.Now().In(wib)
	today := now.Truncate(24 * time.Hour)

	var userName, langStyle string
	_ = a.DB.QueryRowContext(r.Context(),
		`SELECT name, language_style FROM users WHERE id = $1`, userID).Scan(&userName, &langStyle)
	if langStyle == "" {
		langStyle = "casual"
	}

	// Detect welcome message condition from last entry.
	var lastDate time.Time
	var lastVerseRef string
	var totalEntries int
	var doneToday bool
	_ = a.DB.QueryRowContext(r.Context(), `
		SELECT
			COUNT(*),
			COALESCE(MAX(entry_date), '0001-01-01'),
			COALESCE((SELECT verse_ref FROM journal_entries WHERE user_id = $1 ORDER BY entry_date DESC, day_number DESC LIMIT 1), ''),
			EXISTS(SELECT 1 FROM journal_entries WHERE user_id = $1 AND entry_date = CURRENT_DATE AND status = 'completed')
		FROM journal_entries WHERE user_id = $1`,
		userID).Scan(&totalEntries, &lastDate, &lastVerseRef, &doneToday)

	var condition string
	switch {
	case totalEntries == 0:
		condition = "new"
	case doneToday:
		condition = "done"
	case lastDate.Equal(today.AddDate(0, 0, -1)):
		condition = "yesterday"
	case now.Sub(lastDate) >= 7*24*time.Hour:
		condition = "long"
	default:
		condition = "gap"
	}

	seed := int(now.Unix() / 86400) // days since epoch — unique per day, never repeats
	msg := welcomeMsg(condition, lastVerseRef, seed)

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

	hour := now.Hour()
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
		WelcomeMsg:    msg,
		Entries:       entries,
		ShareCards:    shareCards,
		LanguageStyle: langStyle,
	})
}
