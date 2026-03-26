package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/user/devbox/internal/crypto"
	"github.com/user/devbox/internal/database"
)

func TestRateLimiter(t *testing.T) {
	limiter := RateLimiter(10)
	handler := limiter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First 10 requests should succeed.
	for i := 0; i < 10; i++ {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		handler.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, resp.Code)
		}
	}

	// 11th request should be rate-limited.
	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", resp.Code)
	}
	retryAfter := resp.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("expected Retry-After header to be set")
	}
}

func TestRateLimiterDifferentIPs(t *testing.T) {
	limiter := RateLimiter(2)
	handler := limiter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Exhaust limit for IP A.
	for i := 0; i < 2; i++ {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		handler.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("IP A request %d: expected 200, got %d", i+1, resp.Code)
		}
	}

	// IP A should be rate-limited.
	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusTooManyRequests {
		t.Fatalf("IP A: expected 429, got %d", resp.Code)
	}

	// IP B should still be fine.
	resp = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.2:5678"
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("IP B: expected 200, got %d", resp.Code)
	}
}

func TestRateLimiterIgnoresXForwardedFor(t *testing.T) {
	limiter := RateLimiter(2)
	handler := limiter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Exhaust the limit for the real IP.
	for i := 0; i < 2; i++ {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		handler.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, resp.Code)
		}
	}

	// Next request with a spoofed X-Forwarded-For should still be rate-limited
	// because we only use RemoteAddr.
	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 (X-Forwarded-For should be ignored), got %d", resp.Code)
	}
}

func TestRequestSizeLimit(t *testing.T) {
	const limit int64 = 1024 // 1KB for test

	sizeMiddleware := MaxBodySize(limit)
	handler := sizeMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, limit+1)
		_, err := r.Body.Read(buf)
		if err != nil {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Request within limit should work.
	smallBody := bytes.NewReader(make([]byte, 512))
	resp := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", smallBody)
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("small body: expected 200, got %d", resp.Code)
	}

	// Request over limit should return 413.
	bigBody := bytes.NewReader(make([]byte, limit+512))
	resp = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/", bigBody)
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("big body: expected 413, got %d", resp.Code)
	}
}

func TestAuthMiddleware(t *testing.T) {
	srv := newTestServer(t)
	authMiddleware := RequireAuth(srv.DB)
	handler := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Request without Authorization header should return 401.
	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.Code)
	}

	// Request with invalid token should return 401.
	resp = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer invalid_token_xyz")
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid token, got %d", resp.Code)
	}

	// Request with malformed Authorization header should return 401.
	resp = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Basic abc123")
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for non-Bearer auth, got %d", resp.Code)
	}
}

func TestAuthMiddlewareValidSession(t *testing.T) {
	srv := newTestServer(t)
	db := srv.DB

	// 1. Create a user.
	user, err := database.CreateUser(db, "testuser")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// 2. Generate a raw token and hash it.
	rawToken := "dbx_test_token_abc123"
	tokenHash := crypto.HashToken(rawToken)

	// 3. Create a session with the token hash.
	_, err = database.CreateSession(db, user.ID, tokenHash, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// 4. Set up middleware and a handler that checks user ID in context.
	var capturedUserID string
	authMiddleware := RequireAuth(db)
	handler := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUserID = GetUserID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	// 5. Make a request with the raw token.
	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	if capturedUserID != user.ID {
		t.Fatalf("expected user ID %s in context, got %s", user.ID, capturedUserID)
	}
}
