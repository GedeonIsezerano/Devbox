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
const sessionIDKey contextKey = "sessionID"

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

// SetSessionID stores a session ID in the context.
func SetSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDKey, sessionID)
}

// GetSessionID retrieves the session ID from the context. Returns an empty
// string if no session ID is present.
func GetSessionID(ctx context.Context) string {
	v, _ := ctx.Value(sessionIDKey).(string)
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
// A background goroutine periodically removes expired entries to prevent
// unbounded map growth.
func RateLimiter(maxPerMinute int) func(http.Handler) http.Handler {
	var mu sync.Mutex
	clients := make(map[string]*rateLimitEntry)

	// Periodic cleanup of expired entries to prevent unbounded growth.
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			mu.Lock()
			now := time.Now()
			for ip, entry := range clients {
				if now.Sub(entry.windowStart) >= time.Minute {
					delete(clients, ip)
				}
			}
			mu.Unlock()
		}
	}()

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
// Only uses RemoteAddr. Does not trust X-Forwarded-For by default since it can
// be spoofed by clients. Operators behind a reverse proxy should configure the
// proxy to set RemoteAddr correctly (e.g., Caddy does this automatically).
func extractIP(r *http.Request) string {
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
			ctx = SetSessionID(ctx, sess.ID)
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
