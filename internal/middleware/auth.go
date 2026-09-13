package middleware

import (
	"context"
	"net/http"

	"journalflow/internal/session"
)

type ctxKey string

const userIDKey ctxKey = "userID"

// RequireAuth redirects to /login when there's no valid session,
// otherwise stashes the user id in the request context for handlers.
func RequireAuth(sm *session.Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := sm.UserID(r)
			if !ok {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			ctx := context.WithValue(r.Context(), userIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
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
