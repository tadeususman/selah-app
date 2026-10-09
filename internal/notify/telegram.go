// Package notify sends admin alerts to Telegram. It is a no-op when the bot
// token or chat id is unset, so dev and unconfigured deploys are unaffected.
package notify

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Telegram struct {
	token  string
	chatID string
	client *http.Client
}

func New(token, chatID string) *Telegram {
	return &Telegram{token: token, chatID: chatID, client: &http.Client{Timeout: 10 * time.Second}}
}

func (t *Telegram) Enabled() bool { return t != nil && t.token != "" && t.chatID != "" }

// Send posts text in the background; failures are logged, never returned,
// so a Telegram outage can't break the request that triggered the alert.
func (t *Telegram) Send(text string) {
	if !t.Enabled() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			"https://api.telegram.org/bot"+t.token+"/sendMessage",
			strings.NewReader(url.Values{"chat_id": {t.chatID}, "text": {text}}.Encode()))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := t.client.Do(req)
		if err != nil {
			// err can embed the URL (with the token); log only the fact.
			log.Printf("[telegram] send failed")
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			log.Printf("[telegram] send failed: status %d", resp.StatusCode)
		}
	}()
}
