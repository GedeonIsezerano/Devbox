package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/user/devbox/internal/crypto"
	"github.com/user/devbox/internal/database"
)

// newTestServer creates a Server backed by an in-memory DB, a test encryptor,
// and a nonce store. It registers t.Cleanup to close everything properly.
// This helper is reused by all server tests.
func newTestServer(t *testing.T) *Server {
	t.Helper()

	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	enc, err := crypto.NewAgeEncryptor([]byte("test-master-key-at-least-16-bytes"))
	if err != nil {
		t.Fatalf("create test encryptor: %v", err)
	}

	nonces := crypto.NewNonceStore(5 * time.Minute)
	t.Cleanup(func() { nonces.Stop() })

	srv := NewServer(Config{
		ListenAddr: ":0",
		DB:         db,
		Encryptor:  enc,
		Nonces:     nonces,
	})

	return srv
}

// testDB returns the *sql.DB from a test server for direct database operations.
func testDB(srv *Server) *sql.DB {
	return srv.DB
}

func TestHealthEndpoint(t *testing.T) {
	srv := newTestServer(t)
	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)
	srv.Handler.ServeHTTP(resp, req)
	if resp.Code != 200 {
		t.Fatalf("expected 200, got %d", resp.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %s", body["status"])
	}
}

func TestHealthEndpointContentType(t *testing.T) {
	srv := newTestServer(t)
	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)
	srv.Handler.ServeHTTP(resp, req)

	ct := resp.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %s", ct)
	}
}

func TestNotFoundRoute(t *testing.T) {
	srv := newTestServer(t)
	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/nonexistent", nil)
	srv.Handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.Code)
	}
}
