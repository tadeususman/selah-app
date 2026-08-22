// Package session implements a minimal server-side session store.
// The cookie only ever holds a random session id — never user data —
// and the id is looked up against the `sessions` table on each
// request. This is intentionally simple: this app is meant for a
// single user (or a small family/household) on a private server, so
// there's no need for JWTs, refresh tokens, or third-party auth.
package session

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/http"
	"time"
)

const (
	CookieName = "jf_session"
	ttl        = 30 * 24 * time.Hour // 30 days — this is a personal app
)

type Manager struct {
	db *sql.DB
}

func NewManager(db *sql.DB) *Manager {
	return &Manager{db: db}
}

// Create issues a new session for userID and writes the cookie.
func (m *Manager) Create(w http.ResponseWriter, r *http.Request, userID int64) error {
	id, err := randomID()
	if err != nil {
		return err
	}
	expires := time.Now().Add(ttl)
	_, err = m.db.ExecContext(r.Context(),
		`INSERT INTO sessions (id, user_id, expires_at) VALUES ($1, $2, $3)`,
		id, userID, expires)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    id,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// Secure should be true once served over HTTPS (recommended:
		// terminate TLS at a reverse proxy like Caddy in front of this).
		Secure: r.TLS != nil,
	})
	return nil
}

// UserID resolves the current request's session cookie to a user id.
// Returns ok=false if there's no valid, unexpired session.
func (m *Manager) UserID(r *http.Request) (int64, bool) {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return 0, false
	}
	var userID int64
	err = m.db.QueryRowContext(r.Context(),
		`SELECT user_id FROM sessions WHERE id = $1 AND expires_at > now()`,
		c.Value).Scan(&userID)
	if err != nil {
		return 0, false
	}
	return userID, true
}

// Destroy logs the user out: deletes the DB row and clears the cookie.
func (m *Manager) Destroy(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil {
		_, _ = m.db.ExecContext(context.Background(), `DELETE FROM sessions WHERE id = $1`, c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
	})
}

func randomID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
