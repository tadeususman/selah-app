// Package ai wraps calls to the claude-bridge running on the host.
// The bridge exposes POST /ask which runs `claude -p` under the hood,
// so no Anthropic API key is needed here — auth is handled by the bridge.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNotRelevant is returned by SearchVerse when the query has no relation to the Bible or Christian faith.
var ErrNotRelevant = errors.New("query tidak relevan dengan Alkitab")

type Client struct {
	bridgeURL string
	http      *http.Client
}

func NewClient(bridgeURL string) *Client {
	return &Client{
		bridgeURL: bridgeURL,
		http:      &http.Client{Timeout: 120 * time.Second},
	}
}

// ChatMessage is one turn in a conversation.
// Role is "user" or "assistant" (stored as "ai" in DB — handlers
// normalise to "assistant" before passing here).
type ChatMessage struct {
	Role    string
	Content string
}

type bridgeRequest struct {
	Prompt string `json:"prompt"`
	App    string `json:"app"`
}

type bridgeResponse struct {
	Output string `json:"output"`
	Error  string `json:"error"`
}

// send builds a single text prompt from the system instruction + message
// history and hands it to the bridge's /ask endpoint.
func (c *Client) send(ctx context.Context, system string, history []ChatMessage) (string, error) {
	var sb strings.Builder
	if system != "" {
		sb.WriteString(system)
		sb.WriteString("\n\n")
	}
	for _, msg := range history {
		if msg.Role == "user" {
			sb.WriteString("User: ")
		} else {
			sb.WriteString("Assistant: ")
		}
		sb.WriteString(msg.Content)
		sb.WriteString("\n\n")
	}

	body, err := json.Marshal(bridgeRequest{Prompt: strings.TrimSpace(sb.String()), App: "selah"})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.bridgeURL+"/ask", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("bridge tidak bisa dihubungi (%s): %w", c.bridgeURL, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var parsed bridgeResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("bridge respons tidak terduga (status %d): %s", resp.StatusCode, raw)
	}
	if parsed.Error != "" {
		return "", fmt.Errorf("bridge error: %s", parsed.Error)
	}
	return parsed.Output, nil
}

const backgroundSystemPrompt = `Kamu adalah sahabat yang kebetulan paham teologi Alkitab secara mendalam — bukan ceramah, tapi ngobrol serius soal Firman.
Kamu familiar dengan konteks historis, bahasa asli (Ibrani/Yunani), alur narasi Alkitab, dan tradisi penafsiran (hermeneutik).
Waktu aku kasih ayat, ceritain: dari mana ayat ini berasal dan apa konteks aslinya, apa nuansa kata asli yang sering hilang di terjemahan, dan apa maknanya buat pembaca hari ini.
Bahasa santai, pakai "aku" dan "kamu". Kalau tidak yakin, bilang jujur. Sekitar 200-300 kata.`

// VerseBackground asks for historical/original-language context for a verse.
func (c *Client) VerseBackground(ctx context.Context, verseRef, verseText string) (string, error) {
	prompt := fmt.Sprintf("Ayat: %s\n\nTeks: %s\n\nTolong jelaskan latar belakang, konteks historis, dan makna ayat ini.", verseRef, verseText)
	return c.send(ctx, backgroundSystemPrompt, []ChatMessage{{Role: "user", Content: prompt}})
}

const discussSystemPromptLight = `Kamu adalah "Teman Selah" — sahabat diskusi yang hangat dan beriman. Bantu pengguna memahami ayat yang sedang direnungkan dan temukan maknanya untuk kehidupan sehari-hari.
Ngobrol seperti teman, bukan dosen. Pakai "aku" dan "kamu". Fokus pada makna ayat dalam konteks cerita Alkitab, bagaimana ayat ini berbicara ke situasi pengguna hari ini, dan dorongan iman yang praktis.
Tidak perlu menyebut istilah bahasa Ibrani atau Yunani — bicaralah dengan bahasa yang hangat dan mudah dipahami.
Jawaban singkat dan fokus (di bawah 200 kata) kecuali diminta lebih dalam.
Jangan menutup setiap respons dengan pertanyaan balik. Berikan jawaban yang tuntas. Balik bertanya hanya jika konteks memang mengundang dialog lanjutan.
PENTING: Kamu hanya membahas topik yang berkaitan dengan Alkitab, iman Kristen, atau ayat yang sedang direnungkan. Jika pengguna bertanya di luar topik itu, tolak dengan lembut dan ajak kembali ke diskusi ayat. Contoh: "Hmm, itu di luar yang bisa aku bantu di sini. Yuk kita fokus ke ayat yang sedang kamu renungkan — ada bagian yang mau digali lebih dalam?"`

const discussSystemPromptDeep = `Kamu adalah sahabat diskusi yang paham teologi Alkitab secara serius — latar belakang historis, bahasa asli (Ibrani/Yunani), alur teologi lintas kitab, dan bagaimana teks berhubungan dengan Kristus dan narasi keselamatan.
Tapi kamu ngobrol seperti teman, bukan dosen. Pakai "aku" dan "kamu". Jawab pertanyaan dengan berdasar — kutip konteks teks, sertakan nuansa kata asli Ibrani/Yunani bila relevan, hubungkan dengan kitab lain kalau perlu, tapi tetap hangat dan personal.
Kalau ada celah teologis yang menarik, tunjukkan. Kalau aku salah paham sesuatu, koreksi dengan lembut.
Jawaban singkat dan fokus (di bawah 200 kata) kecuali aku minta lebih dalam.
Jangan menutup setiap respons dengan pertanyaan balik. Berikan jawaban yang tuntas. Balik bertanya hanya jika konteks memang mengundang dialog lanjutan.
PENTING: Kamu hanya membahas topik yang berkaitan dengan Alkitab, teologi, iman Kristen, atau ayat yang sedang direnungkan. Jika pengguna bertanya di luar topik itu, tolak dengan lembut dan ajak kembali ke diskusi ayat. Contoh: "Hmm, itu di luar yang bisa aku bantu di sini. Yuk kita fokus ke ayat yang sedang kamu renungkan — ada bagian yang mau digali lebih dalam?"`

// Discuss continues the back-and-forth conversation for an entry.
// history should include all prior turns so the bridge gets full context.
// originalLang=true uses the deep prompt with Hebrew/Greek word analysis.
func (c *Client) Discuss(ctx context.Context, history []ChatMessage, originalLang bool) (string, error) {
	prompt := discussSystemPromptLight
	if originalLang {
		prompt = discussSystemPromptDeep
	}
	return c.send(ctx, prompt, history)
}

const closingSystemPrompt = `Kamu adalah "Teman Selah" — sahabat rohani yang hangat, berbicara dengan bahasa Indonesia yang santai (pakai "aku" dan "kamu").
Pengguna baru saja menyelesaikan sesi perenungan Alkitab. Kamu sudah menemani mereka dari awal — membaca ayat, berdiskusi, sampai akhir sesi.
Tugasmu sekarang: tutup sesi ini dengan pesan penutup yang hangat, personal, dan menyemangati. Acu langsung ke apa yang sudah mereka tulis. Jika ada langkah praktis, acu ke sana juga. Jika tidak ada, tetap tutup dengan hangat tanpa menyinggung soal langkah. Jangan generik.
Singkat saja — 3 sampai 5 kalimat. Akhiri dengan doa pendek yang tulus. Dalam doa, gunakan kata ganti "Engkau" dan "Mu" untuk menyapa Tuhan — bukan "kamu".`

const verseSearchSystemPrompt = `Kamu adalah asisten pencarian ayat Alkitab Indonesia.
User mengingat sebuah frasa atau tema dari Alkitab dan ingin tahu referensinya.
PENTING: Jika pertanyaan tidak ada hubungannya sama sekali dengan Alkitab, iman Kristen, atau tema-tema rohani (kasih, doa, pengampunan, iman, keselamatan, dll), jawab HANYA dengan satu kata: TIDAK_RELEVAN
Jika relevan, temukan 1 sampai 3 ayat yang paling cocok.
Jawab HANYA dengan daftar referensi, satu per baris, tanpa penjelasan atau teks ayat.
Gunakan format standar: Nama Kitab Pasal:Ayat
Contoh:
Matius 5:44
Roma 8:28
Mazmur 23:1`

// SearchVerse asks the AI for verse references matching a phrase or theme.
// Returns a slice of reference strings (e.g. ["Matius 5:44", "Roma 8:28"]).
func (c *Client) SearchVerse(ctx context.Context, query string) ([]string, error) {
	prompt := fmt.Sprintf("Temukan ayat Alkitab yang relevan dengan frasa atau tema ini: \"%s\"", query)
	result, err := c.send(ctx, verseSearchSystemPrompt, []ChatMessage{{Role: "user", Content: prompt}})
	if err != nil {
		return nil, err
	}
	var refs []string
	for _, line := range strings.Split(result, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == "TIDAK_RELEVAN" {
			return nil, ErrNotRelevant
		}
		// Strip leading list markers like "1.", "-", "*"
		if len(line) > 2 && (line[0] == '-' || line[0] == '*' || (line[1] == '.' && line[0] >= '1' && line[0] <= '9')) {
			line = strings.TrimSpace(line[2:])
		}
		if line != "" {
			refs = append(refs, line)
		}
	}
	return refs, nil
}

// ClosingMessage generates a warm closing/encouragement message at the end of a devotion session.
// history should include the full session context including reflection and practical step.
func (c *Client) ClosingMessage(ctx context.Context, history []ChatMessage) (string, error) {
	return c.send(ctx, closingSystemPrompt, history)
}
