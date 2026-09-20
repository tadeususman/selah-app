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
	VerseText    string
	ShareSummary string
	DayNumber    int
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

func welcomeMsg(condition, lastVerseRef, langStyle string, seed int) string {
	formal := langStyle == "formal"

	switch condition {
	case "new":
		if formal {
			return pick([]string{
				"Selah adalah ruang teduh Anda. Mulai kapan saja.",
				"Tidak ada yang terlambat untuk memulai di sini.",
				"Selah selalu terbuka — tidak perlu terburu-buru.",
				"Cukup mulai dari sini, perlahan-lahan.",
			}, seed)
		}
		return pick([]string{
			"Selah adalah ruang jedamu. Mulai kapan saja.",
			"Tidak ada yang terlambat untuk memulai di sini.",
			"Selah selalu terbuka — tidak perlu buru-buru.",
			"Cukup mulai dari sini, pelan-pelan.",
		}, seed)

	case "done":
		if formal {
			return pick([]string{
				"Renungan hari ini sudah selesai. Semoga firman-Nya tinggal di hati Anda.",
				"Waktu yang Anda luangkan tadi tidak sia-sia.",
				"Hari ini sudah ada renungan — itu yang paling penting.",
				"Firman hari ini sudah tertulis. Semoga terasa sampai malam.",
				"Sudah menyisihkan waktu untuk yang paling penting hari ini.",
				"Semoga pesan dari renungan tadi menetap di hati Anda.",
				"Sudah ada waktu bersama Tuhan hari ini. Itu sangat berarti.",
				"Firman hari ini sudah ada — bawa terus sepanjang hari.",
			}, seed)
		}
		return pick([]string{
			"Sudah renungan hari ini. Semoga firman-Nya tinggal di hatimu.",
			"Waktu yang kamu luangkan tadi tidak sia-sia.",
			"Hari ini sudah ada renungan — itu yang paling penting.",
			"Firman hari ini sudah tertulis. Semoga terasa sampai malam.",
			"Sudah menyisihkan waktu untuk yang paling penting hari ini.",
			"Renungan hari ini selesai. Semoga pesannya menetap di hatimu.",
			"Sudah ada waktu bersama Tuhan hari ini. Itu berarti.",
			"Firman hari ini sudah ada — bawa terus sepanjang hari.",
		}, seed)

	case "yesterday":
		ref := lastVerseRef
		if ref == "" {
			ref = "kemarin"
		}
		if formal {
			return pick([]string{
				"Kemarin Anda merenungkan " + ref + ". Hari ini mau lanjut lagi?",
				ref + " menemani Anda kemarin. Hari ini ada apa lagi?",
				"Semoga " + ref + " masih terasa di hati Anda hari ini.",
				"Kemarin bersama " + ref + ". Selah sudah terbuka kembali.",
				"Terakhir Anda di sini bersama " + ref + ". Selamat datang kembali.",
			}, seed)
		}
		return pick([]string{
			"Kemarin kamu merenungkan " + ref + ". Hari ini mau lanjut lagi?",
			ref + " menemanimu kemarin. Hari ini ada apa lagi?",
			"Semoga " + ref + " masih terasa di hatimu hari ini.",
			"Kemarin bersama " + ref + ". Selah sudah terbuka lagi nih.",
			"Terakhir kamu di sini bersama " + ref + ". Selamat datang kembali.",
		}, seed)

	case "gap":
		if formal {
			return pick([]string{
				"Senang Anda kembali ke Selah.",
				"Tidak apa-apa beristirahat sebentar — Selah tetap ada di sini.",
				"Selamat datang kembali. Selah tidak kemana-mana.",
				"Senang Anda mampir lagi ke sini.",
				"Selah masih di sini, menunggu Anda.",
				"Tidak ada yang terlewat. Mulai saja dari sini.",
				"Kapan pun Anda siap, Selah selalu terbuka.",
				"Senang Anda kembali lagi.",
			}, seed)
		}
		return pick([]string{
			"Senang kamu kembali ke Selah.",
			"Tidak apa-apa istirahat sebentar — Selah tetap ada di sini.",
			"Kembali lagi. Selah tidak kemana-mana kok.",
			"Senang kamu mampir lagi ke sini.",
			"Selah masih di sini, menunggu kamu.",
			"Tidak ada yang terlewat. Mulai saja dari sini.",
			"Kapan pun kamu siap, Selah selalu terbuka.",
			"Hei, senang kamu balik lagi.",
		}, seed)

	case "long":
		if formal {
			return pick([]string{
				"Sudah lama tidak bertemu. Senang Anda kembali.",
				"Selah masih di sini, seperti biasa.",
				"Anda kembali — itu yang paling penting.",
				"Lama tidak bertemu. Senang Anda ada di sini lagi.",
				"Tidak ada yang berubah di Selah. Selamat datang kembali.",
				"Apapun yang terjadi, Selah selalu terbuka untuk Anda.",
				"Tidak pernah terlambat untuk kembali ke sini.",
				"Selamat datang lagi — mulai saja perlahan dari sini.",
			}, seed)
		}
		return pick([]string{
			"Sudah lama tidak ketemu. Senang kamu kembali.",
			"Selah masih di sini, seperti biasa.",
			"Kamu kembali — itu yang paling penting.",
			"Lama tidak ketemu. Senang kamu ada di sini lagi.",
			"Tidak ada yang berubah di Selah. Selamat datang kembali.",
			"Apapun yang terjadi, Selah selalu terbuka buatmu.",
			"Tidak pernah terlambat untuk kembali ke sini.",
			"Selamat datang lagi — mulai saja pelan-pelan dari sini.",
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
	msg := welcomeMsg(condition, lastVerseRef, langStyle, seed)

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
		SELECT id, verse_ref, verse_text, share_summary, day_number, entry_date
		FROM journal_entries
		WHERE user_id = $1 AND status = 'completed' AND share_summary != ''
		ORDER BY entry_date DESC, day_number DESC
		LIMIT 4`, userID)
	var shareCards []shareCardPreview
	if err == nil {
		defer cardRows.Close()
		for cardRows.Next() {
			var c shareCardPreview
			if cardRows.Scan(&c.ID, &c.VerseRef, &c.VerseText, &c.ShareSummary, &c.DayNumber, &c.EntryDate) == nil {
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
