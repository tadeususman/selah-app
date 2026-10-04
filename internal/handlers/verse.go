package handlers

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"journalflow/internal/ai"
	"journalflow/internal/middleware"
)

// verseBank is a curated list of verses for the no-context "Pilihkan Ayat" flow.
// App picks randomly while excluding verses used in the last 60 days.
var verseBank = []string{
	// Mazmur
	"Mazmur 1:1-2", "Mazmur 4:8", "Mazmur 8:1", "Mazmur 16:8", "Mazmur 18:1-2",
	"Mazmur 19:1", "Mazmur 19:14", "Mazmur 23:1", "Mazmur 25:4-5", "Mazmur 27:1",
	"Mazmur 27:14", "Mazmur 31:14-15", "Mazmur 32:8", "Mazmur 34:8", "Mazmur 34:18",
	"Mazmur 37:4", "Mazmur 37:23-24", "Mazmur 40:1-2", "Mazmur 46:1", "Mazmur 46:10",
	"Mazmur 51:10", "Mazmur 55:22", "Mazmur 62:1-2", "Mazmur 63:1", "Mazmur 73:26",
	"Mazmur 84:11", "Mazmur 86:5", "Mazmur 90:12", "Mazmur 91:1-2", "Mazmur 100:4-5",
	"Mazmur 103:1-2", "Mazmur 103:12", "Mazmur 107:1", "Mazmur 116:1-2", "Mazmur 119:9-10",
	"Mazmur 119:105", "Mazmur 121:1-2", "Mazmur 130:5", "Mazmur 131:1-2", "Mazmur 139:1-2",
	"Mazmur 139:13-14", "Mazmur 143:8", "Mazmur 145:18",
	// Amsal
	"Amsal 3:5-6", "Amsal 3:7-8", "Amsal 4:23", "Amsal 9:10", "Amsal 11:2",
	"Amsal 12:18", "Amsal 13:12", "Amsal 14:12", "Amsal 15:1", "Amsal 15:13",
	"Amsal 16:3", "Amsal 16:9", "Amsal 17:17", "Amsal 17:22", "Amsal 18:10",
	"Amsal 19:21", "Amsal 22:6", "Amsal 24:16", "Amsal 27:17",
	// Taurat & Sejarah
	"Ulangan 6:4-5", "Ulangan 31:6", "Ulangan 33:27",
	"Yosua 1:8", "Yosua 1:9",
	"1 Samuel 16:7",
	"Nehemia 8:10",
	// Hikmat
	"Ayub 19:25", "Ayub 23:10",
	"Pengkhotbah 3:11", "Pengkhotbah 4:9-10",
	// Nabi-nabi
	"Yesaya 26:3", "Yesaya 30:15", "Yesaya 40:28-29", "Yesaya 40:31", "Yesaya 41:10",
	"Yesaya 43:1-2", "Yesaya 43:19", "Yesaya 49:15-16", "Yesaya 53:5", "Yesaya 54:10",
	"Yesaya 55:8-9", "Yesaya 58:11",
	"Yeremia 17:7-8", "Yeremia 29:11", "Yeremia 31:3",
	"Ratapan 3:22-23",
	"Hosea 14:4",
	"Mikha 6:8", "Mikha 7:18",
	"Habakuk 3:17-18",
	"Zefanya 3:17",
	"Zakharia 4:6",
	// Injil
	"Matius 5:3", "Matius 5:6", "Matius 5:8", "Matius 6:25-26", "Matius 6:33",
	"Matius 11:28-29", "Matius 22:37-38", "Matius 28:20",
	"Markus 9:23-24", "Markus 10:45", "Markus 12:30-31",
	"Lukas 1:37", "Lukas 4:18", "Lukas 6:38", "Lukas 11:9-10", "Lukas 15:20", "Lukas 18:1",
	"Yohanes 1:14", "Yohanes 3:16", "Yohanes 6:35", "Yohanes 8:12", "Yohanes 8:32",
	"Yohanes 10:10", "Yohanes 11:25", "Yohanes 13:34-35", "Yohanes 14:6", "Yohanes 14:27",
	"Yohanes 15:5", "Yohanes 15:13", "Yohanes 16:33",
	// Kisah Para Rasul
	"Kisah Para Rasul 1:8", "Kisah Para Rasul 17:28",
	// Roma
	"Roma 1:16", "Roma 3:23-24", "Roma 5:1", "Roma 5:3-4", "Roma 5:8",
	"Roma 6:23", "Roma 8:1", "Roma 8:28", "Roma 8:38-39",
	"Roma 10:9-10", "Roma 12:1-2", "Roma 12:9-10", "Roma 12:12", "Roma 12:21", "Roma 15:13",
	// 1 Korintus
	"1 Korintus 1:18", "1 Korintus 2:9", "1 Korintus 6:19-20", "1 Korintus 9:24-25",
	"1 Korintus 10:13", "1 Korintus 12:27", "1 Korintus 13:4-5", "1 Korintus 13:7",
	"1 Korintus 15:57", "1 Korintus 15:58",
	// 2 Korintus
	"2 Korintus 1:3-4", "2 Korintus 3:18", "2 Korintus 4:16-17", "2 Korintus 5:17",
	"2 Korintus 9:8", "2 Korintus 12:9",
	// Galatia
	"Galatia 2:20", "Galatia 5:1", "Galatia 5:22-23", "Galatia 6:2", "Galatia 6:9",
	// Efesus
	"Efesus 1:3", "Efesus 1:7", "Efesus 2:8-9", "Efesus 2:10", "Efesus 3:16-17",
	"Efesus 3:20", "Efesus 4:2-3", "Efesus 4:29", "Efesus 4:32",
	"Efesus 5:1-2", "Efesus 5:15-16", "Efesus 6:10", "Efesus 6:18",
	// Filipi
	"Filipi 1:6", "Filipi 1:21", "Filipi 2:3-4", "Filipi 2:5", "Filipi 2:13",
	"Filipi 3:13-14", "Filipi 4:4-5", "Filipi 4:6-7", "Filipi 4:11-12", "Filipi 4:13", "Filipi 4:19",
	// Kolose
	"Kolose 1:9-10", "Kolose 1:16-17", "Kolose 2:6-7", "Kolose 3:1",
	"Kolose 3:2", "Kolose 3:13", "Kolose 3:15-16", "Kolose 3:17", "Kolose 3:23",
	// Tesalonika & Timotius
	"1 Tesalonika 5:16-18",
	"2 Tesalonika 3:3",
	"1 Timotius 4:12", "1 Timotius 6:6",
	"2 Timotius 1:7", "2 Timotius 2:15", "2 Timotius 3:16-17", "2 Timotius 4:7-8",
	// Titus & lainnya
	"Titus 3:4-5",
	// Ibrani
	"Ibrani 2:18", "Ibrani 3:13", "Ibrani 4:12", "Ibrani 4:16", "Ibrani 6:19",
	"Ibrani 10:23", "Ibrani 10:24-25", "Ibrani 11:1", "Ibrani 11:6",
	"Ibrani 12:1-2", "Ibrani 13:5-6", "Ibrani 13:8", "Ibrani 13:15",
	// Yakobus
	"Yakobus 1:2-3", "Yakobus 1:5", "Yakobus 1:17", "Yakobus 1:22",
	"Yakobus 2:17", "Yakobus 3:17", "Yakobus 4:7-8", "Yakobus 5:16",
	// 1 & 2 Petrus
	"1 Petrus 1:3-4", "1 Petrus 1:6-7", "1 Petrus 2:2", "1 Petrus 2:9",
	"1 Petrus 3:15", "1 Petrus 4:10", "1 Petrus 5:7", "1 Petrus 5:8-9",
	"2 Petrus 1:3", "2 Petrus 1:5-7", "2 Petrus 3:9", "2 Petrus 3:18",
	// 1 Yohanes
	"1 Yohanes 1:9", "1 Yohanes 2:1-2", "1 Yohanes 3:1", "1 Yohanes 3:16-17",
	"1 Yohanes 4:7-8", "1 Yohanes 4:18", "1 Yohanes 4:19", "1 Yohanes 5:4", "1 Yohanes 5:14-15",
	// Yudas & Wahyu
	"Yudas 1:24-25",
	"Wahyu 2:10", "Wahyu 3:20", "Wahyu 21:4", "Wahyu 22:13",
}

type sabdaXML struct {
	Book struct {
		Chapter struct {
			Verses struct {
				Verse []struct {
					Text string `xml:"text"`
				} `xml:"verse"`
			} `xml:"verses"`
		} `xml:"chapter"`
	} `xml:"book"`
}

func (a *App) VerseFetch(w http.ResponseWriter, r *http.Request) {
	ref := strings.TrimSpace(r.URL.Query().Get("ref"))

	writeErr := func(code int, msg string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]string{"error": msg})
	}

	if ref == "" {
		writeErr(http.StatusBadRequest, "Referensi ayat wajib diisi")
		return
	}

	apiURL := "https://alkitab.sabda.org/api/passage.php?passage=" + url.QueryEscape(ref)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		writeErr(http.StatusBadGateway, "Tidak bisa menghubungi sumber ayat")
		return
	}
	defer resp.Body.Close()

	var parsed sabdaXML
	if err := xml.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		writeErr(http.StatusInternalServerError, "Gagal membaca respons")
		return
	}

	var parts []string
	for _, v := range parsed.Book.Chapter.Verses.Verse {
		t := strings.TrimSpace(v.Text)
		if t != "" {
			parts = append(parts, t)
		}
	}

	if len(parts) == 0 {
		writeErr(http.StatusNotFound, "Ayat tidak ditemukan, coba periksa referensinya")
		return
	}

	text := strings.Join(parts, " ")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"text": text})
}

// ---- POST /api/verse/recommend (AI-powered verse recommendation) ----

func (a *App) VerseRecommend(w http.ResponseWriter, r *http.Request) {
	writeErr := func(code int, msg string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]string{"error": msg})
	}

	if err := r.ParseForm(); err != nil {
		writeErr(http.StatusBadRequest, "bad form")
		return
	}
	userInput := strings.TrimSpace(r.FormValue("q"))
	userID := middleware.UserID(r)

	// No-context path: pick from curated verse bank, skip verses used in last 60 days.
	if userInput == "" {
		usedRows, _ := a.DB.QueryContext(r.Context(),
			`SELECT DISTINCT verse_ref FROM journal_entries
			 WHERE user_id = $1 AND verse_ref != ''
			   AND created_at > NOW() - INTERVAL '60 days'`, userID)
		usedSet := make(map[string]bool)
		if usedRows != nil {
			defer usedRows.Close()
			for usedRows.Next() {
				var ref string
				if usedRows.Scan(&ref) == nil {
					usedSet[strings.ToLower(strings.TrimSpace(ref))] = true
				}
			}
		}
		// Also exclude current verse shown in modal (frontend refresh flow).
		if ex := strings.TrimSpace(r.FormValue("exclude")); ex != "" {
			for _, ref := range strings.Split(ex, ",") {
				if ref = strings.TrimSpace(ref); ref != "" {
					usedSet[strings.ToLower(ref)] = true
				}
			}
		}
		perm := rand.Perm(len(verseBank))
		picked := verseBank[perm[0]] // fallback: first in shuffle
		for _, i := range perm {
			if !usedSet[strings.ToLower(verseBank[i])] {
				picked = verseBank[i]
				break
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"recommendations": []map[string]string{{"ref": picked, "reason": ""}},
		})
		return
	}

	// Context path: use AI.
	var exclude []string
	rows, err := a.DB.QueryContext(r.Context(),
		`SELECT verse_ref FROM journal_entries
		 WHERE user_id = $1 AND verse_ref != '' AND status = 'completed'
		 ORDER BY created_at DESC LIMIT 20`, userID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var ref string
			if rows.Scan(&ref) == nil && ref != "" {
				exclude = append(exclude, ref)
			}
		}
	}
	if ex := strings.TrimSpace(r.FormValue("exclude")); ex != "" {
		for _, ref := range strings.Split(ex, ",") {
			if ref = strings.TrimSpace(ref); ref != "" {
				exclude = append(exclude, ref)
			}
		}
	}

	recs, err := a.AI.RecommendVerse(r.Context(), userInput, exclude)
	if err != nil {
		log.Printf("[verse/recommend] AI error (input=%q): %v", userInput, err)
		writeErr(http.StatusInternalServerError, "Teman Selah sedang tidak bisa dihubungi. Coba lagi sebentar.")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"recommendations": recs})
}

// ---- GET /api/verse/search?q=... (AI-powered phrase search) ----

type verseOption struct {
	Ref  string `json:"ref"`
	Text string `json:"text"`
}

func (a *App) VerseSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	writeErr := func(code int, msg string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]string{"error": msg})
	}

	if query == "" {
		writeErr(http.StatusBadRequest, "Masukkan frasa atau tema ayat yang ingin dicari")
		return
	}

	refs, err := a.AI.SearchVerse(r.Context(), query)
	if err != nil {
		if errors.Is(err, ai.ErrNotRelevant) {
			// Input tidak cocok sebagai frasa Alkitab — coba rekomendasikan ayat berdasarkan konteks yang ditulis
			options, fetchErr := a.fetchVerseOptions(r.Context(), query)
			if errors.Is(fetchErr, ai.ErrNotRelevant) {
				writeErr(http.StatusBadRequest, "Teman Selah hanya bisa membantu mencari ayat atau merekomendasikan ayat berdasarkan situasi hidupmu. Coba ceritakan apa yang sedang kamu rasakan atau pikirkan hari ini.")
				return
			}
			if fetchErr != nil || len(options) == 0 {
				log.Printf("[verse/search] recommend fallback error (query=%q): %v", query, fetchErr)
				writeErr(http.StatusNotFound, "Ayat tidak ditemukan. Coba tulis frasa dari Alkitab atau ceritakan situasimu.")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"options": options,
				"label":   "Rekomendasi dari Teman Selah",
			})
			return
		}
		log.Printf("[verse/search] AI error (query=%q): %v", query, err)
		writeErr(http.StatusInternalServerError, "Teman Selah tidak bisa membantu saat ini, coba lagi.")
		return
	}
	if len(refs) == 0 {
		writeErr(http.StatusNotFound, "Ayat tidak ditemukan. Coba gunakan frasa yang berbeda.")
		return
	}

	options, _ := a.fetchVerseTexts(refs)
	if len(options) == 0 {
		writeErr(http.StatusNotFound, "Ayat tidak ditemukan. Coba gunakan frasa yang berbeda.")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"options": options})
}

// fetchVerseTexts fetches verse text from SABDA for each ref and returns options (max 3).
func (a *App) fetchVerseTexts(refs []string) ([]verseOption, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	var options []verseOption
	for _, ref := range refs {
		if len(options) >= 3 {
			break
		}
		apiURL := "https://alkitab.sabda.org/api/passage.php?passage=" + url.QueryEscape(ref)
		resp, err := client.Get(apiURL)
		if err != nil {
			continue
		}
		var parsed sabdaXML
		decodeErr := xml.NewDecoder(resp.Body).Decode(&parsed)
		resp.Body.Close()
		if decodeErr != nil {
			continue
		}
		var parts []string
		for _, v := range parsed.Book.Chapter.Verses.Verse {
			t := strings.TrimSpace(v.Text)
			if t != "" {
				parts = append(parts, t)
			}
		}
		if len(parts) == 0 {
			continue
		}
		text := strings.Join(parts, " ")
		if utf8.RuneCountInString(text) > 500 {
			continue
		}
		options = append(options, verseOption{Ref: ref, Text: text})
	}
	return options, nil
}

// fetchVerseOptions calls RecommendVerse then fetches the actual verse texts.
// Returns ErrNotRelevant (from ai package) if the input is unrelated to life or faith.
func (a *App) fetchVerseOptions(ctx context.Context, userInput string) ([]verseOption, error) {
	recs, err := a.AI.RecommendVerse(ctx, userInput, nil)
	if err != nil {
		return nil, err
	}
	refs := make([]string, 0, len(recs))
	for _, r := range recs {
		refs = append(refs, r.Ref)
	}
	return a.fetchVerseTexts(refs)
}
