package handlers

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"journalflow/internal/ai"
)

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
	var exclude []string
	if ex := strings.TrimSpace(r.FormValue("exclude")); ex != "" {
		for _, ref := range strings.Split(ex, ",") {
			if ref = strings.TrimSpace(ref); ref != "" {
				exclude = append(exclude, ref)
			}
		}
	}

	recs, err := a.AI.RecommendVerse(r.Context(), userInput, exclude)
	if err != nil {
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
		if utf8.RuneCountInString(text) > 1000 {
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
