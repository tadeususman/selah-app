package middleware

import (
	"context"
	"net/http"
	"time"

	"journalflow/internal/session"
)

type ctxKey string

const userIDKey ctxKey = "userID"

// RequireAuth redirects to /login when there's no valid session,
// otherwise stashes the user id in the request context for handlers.
func RequireAuth(sm *session.Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, isGuest, ok := sm.UserIDGuest(r)
			if !ok {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			syncGuestCookie(w, r, isGuest)
			sm.Refresh(w, r)
			ctx := context.WithValue(r.Context(), userIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// syncGuestCookie keeps the cosmetic jf_guest hint (banner + "register first"
// pop-up) in step with the real account, so a stale cookie left over from an
// earlier trial can never make a registered user look like a guest.
func syncGuestCookie(w http.ResponseWriter, r *http.Request, isGuest bool) {
	c, err := r.Cookie("jf_guest")
	has := err == nil && c.Value == "1"
	switch {
	case !isGuest && has:
		http.SetCookie(w, &http.Cookie{Name: "jf_guest", Value: "", Path: "/", Expires: time.Unix(0, 0), MaxAge: -1})
	case isGuest && !has:
		http.SetCookie(w, &http.Cookie{
			Name: "jf_guest", Value: "1", Path: "/", Expires: time.Now().Add(7 * 24 * time.Hour),
			SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		})
	}
}

// UserID reads the authenticated user id set by RequireAuth.
func UserID(r *http.Request) int64 {
	v, _ := r.Context().Value(userIDKey).(int64)
	return v
}

// UserIDFromCtx reads the authenticated user id from a context directly.
func UserIDFromCtx(ctx context.Context) int64 {
	v, _ := ctx.Value(userIDKey).(int64)
	return v
}
