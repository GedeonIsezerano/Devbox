package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// createTokenResponse matches the JSON returned by POST /tokens.
type createTokenResponse struct {
	Token     string `json:"token"`
	ID        string `json:"id"`
	ExpiresAt string `json:"expires_at"`
}

// listTokensResponse matches the JSON returned by GET /tokens.
type listTokensResponse struct {
	Tokens []tokenItem `json:"tokens"`
}

// tokenItem represents a single token in the list response.
type tokenItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Scope     string `json:"scope"`
	ExpiresAt string `json:"expires_at"`
	LastUsed  string `json:"last_used"`
	CreatedAt string `json:"created_at"`
}

func TestCreatePAT(t *testing.T) {
	srv := newTestServer(t)
	_, token := createTestUserWithSession(t, srv, "alice")

	body := jsonBody(map[string]string{
		"name":  "claude-code-web",
		"type":  "pat",
		"scope": "project:read",
		"ttl":   "90d",
	})
	req := authRequest(httptest.NewRequest("POST", "/tokens", body), token)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", resp.Code, resp.Body.String())
	}

	var result createTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if !strings.HasPrefix(result.Token, "dbx_pat_") {
		t.Fatalf("expected token prefix dbx_pat_, got %s", result.Token)
	}
	if result.ID == "" {
		t.Fatal("expected non-empty id")
	}
	if !strings.HasPrefix(result.ID, "tok_") {
		t.Fatalf("expected id prefix tok_, got %s", result.ID)
	}
	if result.ExpiresAt == "" {
		t.Fatal("expected non-empty expires_at")
	}
}

func TestCreateProvision(t *testing.T) {
	srv := newTestServer(t)
	_, token := createTestUserWithSession(t, srv, "alice")

	// Create a project first so we have a valid project_id.
	projID := createTestProject(t, srv, token, "test-proj", ".env")

	body := jsonBody(map[string]string{
		"name":       "provision-token",
		"type":       "provision",
		"scope":      "project:read",
		"ttl":        "1h",
		"project_id": projID,
	})
	req := authRequest(httptest.NewRequest("POST", "/tokens", body), token)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", resp.Code, resp.Body.String())
	}

	var result createTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if !strings.HasPrefix(result.Token, "dbx_prov_") {
		t.Fatalf("expected token prefix dbx_prov_, got %s", result.Token)
	}
	if result.ID == "" {
		t.Fatal("expected non-empty id")
	}
	if result.ExpiresAt == "" {
		t.Fatal("expected non-empty expires_at")
	}
}

func TestCreateProvisionRequiresProjectID(t *testing.T) {
	srv := newTestServer(t)
	_, token := createTestUserWithSession(t, srv, "alice")

	body := jsonBody(map[string]string{
		"name":  "provision-token",
		"type":  "provision",
		"scope": "project:read",
		"ttl":   "1h",
	})
	req := authRequest(httptest.NewRequest("POST", "/tokens", body), token)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestListTokens(t *testing.T) {
	srv := newTestServer(t)
	_, token := createTestUserWithSession(t, srv, "alice")

	// Create two tokens.
	for _, name := range []string{"token-1", "token-2"} {
		body := jsonBody(map[string]string{
			"name":  name,
			"type":  "pat",
			"scope": "project:read",
			"ttl":   "90d",
		})
		req := authRequest(httptest.NewRequest("POST", "/tokens", body), token)
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		srv.Handler.ServeHTTP(resp, req)
		if resp.Code != http.StatusCreated {
			t.Fatalf("create token %s: expected 201, got %d: %s", name, resp.Code, resp.Body.String())
		}
	}

	// List tokens.
	req := authRequest(httptest.NewRequest("GET", "/tokens", nil), token)
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var result listTokensResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(result.Tokens) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(result.Tokens))
	}

	// Verify no hash is included — check the raw JSON.
	rawBody := resp.Body.String()
	if strings.Contains(rawBody, "hash") {
		t.Fatal("response should not include hash field")
	}

	// Verify fields are present.
	for _, tok := range result.Tokens {
		if tok.ID == "" {
			t.Fatal("expected non-empty id")
		}
		if tok.Name == "" {
			t.Fatal("expected non-empty name")
		}
		if tok.Type != "pat" {
			t.Fatalf("expected type pat, got %s", tok.Type)
		}
		if tok.CreatedAt == "" {
			t.Fatal("expected non-empty created_at")
		}
	}
}

func TestRevokeToken(t *testing.T) {
	srv := newTestServer(t)
	_, token := createTestUserWithSession(t, srv, "alice")

	// Create a token.
	body := jsonBody(map[string]string{
		"name":  "to-revoke",
		"type":  "pat",
		"scope": "project:read",
		"ttl":   "90d",
	})
	req := authRequest(httptest.NewRequest("POST", "/tokens", body), token)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", resp.Code, resp.Body.String())
	}

	var created createTokenResponse
	json.NewDecoder(resp.Body).Decode(&created)

	// Revoke it.
	req = authRequest(httptest.NewRequest("DELETE", "/tokens/"+created.ID, nil), token)
	resp = httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("revoke: expected 204, got %d: %s", resp.Code, resp.Body.String())
	}

	// List should now be empty.
	req = authRequest(httptest.NewRequest("GET", "/tokens", nil), token)
	resp = httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	var result listTokensResponse
	json.NewDecoder(resp.Body).Decode(&result)

	if len(result.Tokens) != 0 {
		t.Fatalf("expected 0 tokens after revoke, got %d", len(result.Tokens))
	}
}

func TestRevokeOtherUserToken(t *testing.T) {
	srv := newTestServer(t)
	_, aliceToken := createTestUserWithSession(t, srv, "alice")
	_, bobToken := createTestUserWithSession(t, srv, "bob")

	// Alice creates a token.
	body := jsonBody(map[string]string{
		"name":  "alice-token",
		"type":  "pat",
		"scope": "project:read",
		"ttl":   "90d",
	})
	req := authRequest(httptest.NewRequest("POST", "/tokens", body), aliceToken)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", resp.Code, resp.Body.String())
	}

	var created createTokenResponse
	json.NewDecoder(resp.Body).Decode(&created)

	// Bob tries to revoke it — should get 404.
	req = authRequest(httptest.NewRequest("DELETE", "/tokens/"+created.ID, nil), bobToken)
	resp = httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for other user's token, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestTokenMandatoryExpiry(t *testing.T) {
	srv := newTestServer(t)
	_, token := createTestUserWithSession(t, srv, "alice")

	// Create token without TTL — should default to 90d.
	body := jsonBody(map[string]string{
		"name":  "no-ttl",
		"type":  "pat",
		"scope": "project:read",
	})
	req := authRequest(httptest.NewRequest("POST", "/tokens", body), token)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", resp.Code, resp.Body.String())
	}

	var result createTokenResponse
	json.NewDecoder(resp.Body).Decode(&result)

	if result.ExpiresAt == "" {
		t.Fatal("expected non-empty expires_at for default TTL")
	}
}

func TestTokenMaxTTL(t *testing.T) {
	srv := newTestServer(t)
	_, token := createTestUserWithSession(t, srv, "alice")

	// Try to create a token with TTL > 365d — should fail.
	body := jsonBody(map[string]string{
		"name":  "too-long",
		"type":  "pat",
		"scope": "project:read",
		"ttl":   "366d",
	})
	req := authRequest(httptest.NewRequest("POST", "/tokens", body), token)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for TTL > 365d, got %d: %s", resp.Code, resp.Body.String())
	}
}
