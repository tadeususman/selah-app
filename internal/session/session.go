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
	ttl        = 90 * 24 * time.Hour  // idle lifetime; extended while the user stays active
	maxAge     = 180 * 24 * time.Hour // hard cap from login, so a stolen cookie can't live forever
	refreshGap = 24 * time.Hour       // extend at most once a day to avoid a DB write per request
)

// isSecure reports whether the client reached us over HTTPS, including when
// TLS is terminated by a reverse proxy (nginx/Cloudflare) in front of the app.
func isSecure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

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
		Secure:   isSecure(r),
	})
	return nil
}

// Refresh slides the session forward while the user is active: if less than
// ttl-refreshGap remains, expiry moves to now+ttl (never past created_at+maxAge)
// and the cookie is re-issued. No-op for recently refreshed sessions.
func (m *Manager) Refresh(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return
	}
	var expires time.Time
	err = m.db.QueryRowContext(r.Context(), `
		UPDATE sessions
		SET expires_at = LEAST(now() + make_interval(secs => $2), created_at + make_interval(secs => $3))
		WHERE id = $1 AND expires_at > now()
		  AND expires_at < LEAST(now() + make_interval(secs => $2), created_at + make_interval(secs => $3)) - make_interval(secs => $4)
		RETURNING expires_at`,
		c.Value, ttl.Seconds(), maxAge.Seconds(), refreshGap.Seconds()).Scan(&expires)
	if err != nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    c.Value,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecure(r),
	})
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
