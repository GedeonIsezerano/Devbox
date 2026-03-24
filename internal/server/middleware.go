package server

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/user/devbox/internal/crypto"
	"github.com/user/devbox/internal/database"
)

// contextKey is a private type for context keys in this package.
type contextKey string

const userIDKey contextKey = "userID"

// SetUserID stores a user ID in the context.
func SetUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// GetUserID retrieves the user ID from the context. Returns an empty string
// if no user ID is present.
func GetUserID(ctx context.Context) string {
	v, _ := ctx.Value(userIDKey).(string)
	return v
}

// rateLimitEntry tracks per-IP request counts within a time window.
type rateLimitEntry struct {
	count       int
	windowStart time.Time
}

// RateLimiter returns middleware that enforces a per-IP rate limit of
// maxPerMinute requests per 60-second sliding window. When the limit is
// exceeded, it responds with 429 Too Many Requests and a Retry-After header.
func RateLimiter(maxPerMinute int) func(http.Handler) http.Handler {
	var mu sync.Mutex
	clients := make(map[string]*rateLimitEntry)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractIP(r)

			mu.Lock()
			entry, ok := clients[ip]
			now := time.Now()

			if !ok || now.Sub(entry.windowStart) >= time.Minute {
				// New window.
				clients[ip] = &rateLimitEntry{
					count:       1,
					windowStart: now,
				}
				mu.Unlock()
				next.ServeHTTP(w, r)
				return
			}

			entry.count++
			if entry.count > maxPerMinute {
				remaining := time.Minute - now.Sub(entry.windowStart)
				mu.Unlock()
				w.Header().Set("Retry-After", fmt.Sprintf("%d", int(remaining.Seconds())+1))
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			mu.Unlock()
			next.ServeHTTP(w, r)
		})
	}
}

// extractIP gets the client IP from the request, stripping the port if present.
func extractIP(r *http.Request) string {
	// Check X-Forwarded-For first for proxied requests.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// RequireAuth returns middleware that validates a Bearer token from the
// Authorization header. It hashes the token with SHA-256, looks up the
// session in the database, and stores the user ID in the request context.
// Returns 401 Unauthorized if the token is missing or invalid.
func RequireAuth(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "authorization required", http.StatusUnauthorized)
				return
			}

			if !strings.HasPrefix(authHeader, "Bearer ") {
				http.Error(w, "authorization required", http.StatusUnauthorized)
				return
			}

			rawToken := strings.TrimPrefix(authHeader, "Bearer ")
			if rawToken == "" {
				http.Error(w, "authorization required", http.StatusUnauthorized)
				return
			}

			tokenHash := crypto.HashToken(rawToken)
			sess, err := database.FindSession(db, tokenHash)
			if err != nil {
				http.Error(w, "authorization required", http.StatusUnauthorized)
				return
			}

			ctx := SetUserID(r.Context(), sess.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// MaxBodySize returns middleware that limits the size of request bodies.
// If the body exceeds n bytes, the reader will return an error, and the
// handler should respond with 413 Request Entity Too Large.
func MaxBodySize(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}
