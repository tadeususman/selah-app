// Package ai wraps calls to the claude-bridge running on the host.
// The bridge exposes POST /ask which runs `claude -p` under the hood,
// so no Anthropic API key is needed here — auth is handled by the bridge.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

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

const backgroundSystemPrompt = `Kamu adalah asisten devotional yang membantu pengguna merenungkan ayat Alkitab.
Tugasmu: berikan konteks historis singkat, makna kata dalam bahasa asli (Ibrani/Yunani) bila relevan,
dan penjelasan makna ayat tersebut. Tulis dengan hangat, jelas, dan tidak menggurui.
Gunakan Bahasa Indonesia. Jangan mengarang referensi yang tidak kamu yakini benar; jika kurang yakin, katakan begitu.
Jawaban maksimal sekitar 200-300 kata.`

// VerseBackground asks for historical/original-language context for a verse.
func (c *Client) VerseBackground(ctx context.Context, verseRef, verseText string) (string, error) {
	prompt := fmt.Sprintf("Ayat: %s\n\nTeks: %s\n\nTolong jelaskan latar belakang, konteks historis, dan makna ayat ini.", verseRef, verseText)
	return c.send(ctx, backgroundSystemPrompt, []ChatMessage{{Role: "user", Content: prompt}})
}

const discussSystemPrompt = `Kamu adalah teman diskusi devotional yang membantu pengguna merenungkan lebih dalam
ayat Alkitab yang sedang mereka baca. Ajukan pertanyaan reflektif bila cocok, jawab pertanyaan mereka dengan
jujur dan berdasarkan Alkitab, dan bantu mereka menemukan langkah praktis dari perenungan mereka.
Gunakan Bahasa Indonesia, nada hangat dan personal, jawaban ringkas (di bawah ~200 kata) kecuali diminta lebih detail.`

// Discuss continues the back-and-forth conversation for an entry.
// history should include all prior turns so the bridge gets full context.
func (c *Client) Discuss(ctx context.Context, history []ChatMessage) (string, error) {
	return c.send(ctx, discussSystemPrompt, history)
}
