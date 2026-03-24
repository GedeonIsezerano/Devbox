package cli

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestClientAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"nonce":      "abc123",
			"expires_at": "2099-01-01T00:00:00Z",
		})
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL, "my-secret-token", "")
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	// Challenge doesn't require auth, but the client should still set it.
	_, _ = c.Challenge()

	want := "Bearer my-secret-token"
	if gotAuth != want {
		t.Errorf("Authorization header = %q, want %q", gotAuth, want)
	}
}

func TestClientAuthHeaderEmpty(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"nonce":      "abc123",
			"expires_at": "2099-01-01T00:00:00Z",
		})
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL, "", "")
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	_, _ = c.Challenge()

	if gotAuth != "" {
		t.Errorf("Authorization header = %q, want empty when no token", gotAuth)
	}
}

func TestClientCustomCA(t *testing.T) {
	// Create a TLS test server with a self-signed cert.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"nonce":      "test-nonce",
			"expires_at": "2099-01-01T00:00:00Z",
		})
	}))
	defer srv.Close()

	// Extract the CA cert from the test server.
	cert := srv.TLS.Certificates[0]
	certDER, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}

	// Write the CA cert to a PEM file.
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER.Raw,
	})
	if err := os.WriteFile(caPath, pemData, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Create client with the custom CA.
	c, err := NewClient(srv.URL, "", caPath)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	// This should succeed because we trust the self-signed cert.
	resp, err := c.Challenge()
	if err != nil {
		t.Fatalf("Challenge() error: %v", err)
	}
	if resp.Nonce != "test-nonce" {
		t.Errorf("Nonce = %q, want %q", resp.Nonce, "test-nonce")
	}
}

func TestClientCustomCAInvalid(t *testing.T) {
	badPath := filepath.Join(t.TempDir(), "nonexistent.pem")
	_, err := NewClient("https://example.com", "", badPath)
	if err == nil {
		t.Error("NewClient() should error for non-existent CA file")
	}
}

func TestClientRequestResponse(t *testing.T) {
	// Mock server that echoes back a register response.
	var gotBody registerRequestBody
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/auth/register":
			json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(RegisterResponse{
				UserID:  "usr_123",
				Name:    gotBody.Name,
				IsAdmin: false,
			})
		case r.Method == "POST" && r.URL.Path == "/auth/challenge":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(ChallengeResponse{
				Nonce:     "hex-nonce",
				ExpiresAt: "2099-01-01T00:00:00Z",
			})
		case r.Method == "POST" && r.URL.Path == "/auth/verify":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(VerifyResponse{
				SessionToken: "dbx_ses_abc",
				UserID:       "usr_123",
				ExpiresIn:    900,
			})
		case r.Method == "POST" && r.URL.Path == "/auth/token":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(VerifyResponse{
				SessionToken: "dbx_ses_tok",
				UserID:       "usr_456",
				ExpiresIn:    900,
			})
		case r.Method == "POST" && r.URL.Path == "/auth/logout":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "POST" && r.URL.Path == "/projects":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(ProjectResponse{
				ID:   "proj_1",
				Name: "myproject",
			})
		case r.Method == "GET" && r.URL.Path == "/projects":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]ProjectResponse{
				{ID: "proj_1", Name: "myproject"},
			})
		case r.Method == "DELETE" && r.URL.Path == "/projects/proj_1":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "GET" && r.URL.Path == "/projects/proj_1/env":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(EnvResponse{
				Data:    "base64data",
				Version: 1,
				EnvFile: ".env",
			})
		case r.Method == "PUT" && r.URL.Path == "/projects/proj_1/env":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(PushEnvResponse{Version: 2})
		case r.Method == "GET" && r.URL.Path == "/projects/proj_1/env/version":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(EnvVersionResponse{Version: 3})
		case r.Method == "POST" && r.URL.Path == "/tokens":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(CreateTokenResponse{
				Token:     "dbx_pat_xyz",
				ID:        "tok_1",
				ExpiresAt: "2099-01-01T00:00:00Z",
			})
		case r.Method == "GET" && r.URL.Path == "/tokens":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(TokenListResponse{
				Tokens: []TokenResponse{
					{ID: "tok_1", Name: "test"},
				},
			})
		case r.Method == "DELETE" && r.URL.Path == "/tokens/tok_1":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL, "test-token", "")
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	// Test Register.
	reg, err := c.Register("alice", "ssh-ed25519 AAAA...", "SHA256:fp")
	if err != nil {
		t.Fatalf("Register() error: %v", err)
	}
	if reg.UserID != "usr_123" {
		t.Errorf("Register().UserID = %q, want %q", reg.UserID, "usr_123")
	}
	if gotBody.Name != "alice" {
		t.Errorf("Register request name = %q, want %q", gotBody.Name, "alice")
	}

	// Test Challenge.
	ch, err := c.Challenge()
	if err != nil {
		t.Fatalf("Challenge() error: %v", err)
	}
	if ch.Nonce != "hex-nonce" {
		t.Errorf("Challenge().Nonce = %q, want %q", ch.Nonce, "hex-nonce")
	}

	// Test Verify.
	ver, err := c.Verify("SHA256:fp", []byte("sig"), "nonce")
	if err != nil {
		t.Fatalf("Verify() error: %v", err)
	}
	if ver.SessionToken != "dbx_ses_abc" {
		t.Errorf("Verify().SessionToken = %q, want %q", ver.SessionToken, "dbx_ses_abc")
	}

	// Test TokenAuth.
	ta, err := c.TokenAuth("dbx_pat_xyz")
	if err != nil {
		t.Fatalf("TokenAuth() error: %v", err)
	}
	if ta.SessionToken != "dbx_ses_tok" {
		t.Errorf("TokenAuth().SessionToken = %q, want %q", ta.SessionToken, "dbx_ses_tok")
	}

	// Test Logout.
	if err := c.Logout(); err != nil {
		t.Fatalf("Logout() error: %v", err)
	}

	// Test CreateProject.
	proj, err := c.CreateProject("myproject", "github.com/user/repo", ".env")
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	if proj.ID != "proj_1" {
		t.Errorf("CreateProject().ID = %q, want %q", proj.ID, "proj_1")
	}

	// Test ListProjects.
	projects, err := c.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects() error: %v", err)
	}
	if len(projects) != 1 || projects[0].ID != "proj_1" {
		t.Errorf("ListProjects() = %+v, want 1 project with ID proj_1", projects)
	}

	// Test DeleteProject.
	if err := c.DeleteProject("proj_1"); err != nil {
		t.Fatalf("DeleteProject() error: %v", err)
	}

	// Test PullEnv.
	env, err := c.PullEnv("proj_1")
	if err != nil {
		t.Fatalf("PullEnv() error: %v", err)
	}
	if env.Data != "base64data" {
		t.Errorf("PullEnv().Data = %q, want %q", env.Data, "base64data")
	}

	// Test PushEnv.
	pushResp, err := c.PushEnv("proj_1", "newdata", 1, ".env.local")
	if err != nil {
		t.Fatalf("PushEnv() error: %v", err)
	}
	if pushResp.Version != 2 {
		t.Errorf("PushEnv().Version = %d, want %d", pushResp.Version, 2)
	}

	// Test GetEnvVersion.
	ver2, err := c.GetEnvVersion("proj_1")
	if err != nil {
		t.Fatalf("GetEnvVersion() error: %v", err)
	}
	if ver2 != 3 {
		t.Errorf("GetEnvVersion() = %d, want %d", ver2, 3)
	}

	// Test CreateToken.
	tokResp, err := c.CreateToken("mytoken", "pat", "env:read", "90d", "")
	if err != nil {
		t.Fatalf("CreateToken() error: %v", err)
	}
	if tokResp.Token != "dbx_pat_xyz" {
		t.Errorf("CreateToken().Token = %q, want %q", tokResp.Token, "dbx_pat_xyz")
	}

	// Test ListTokens.
	tokens, err := c.ListTokens()
	if err != nil {
		t.Fatalf("ListTokens() error: %v", err)
	}
	if len(tokens) != 1 || tokens[0].ID != "tok_1" {
		t.Errorf("ListTokens() = %+v, want 1 token with ID tok_1", tokens)
	}

	// Test RevokeToken.
	if err := c.RevokeToken("tok_1"); err != nil {
		t.Fatalf("RevokeToken() error: %v", err)
	}
}

func TestClientAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "name is required"})
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL, "", "")
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}

	_, err = c.Register("", "", "")
	if err == nil {
		t.Fatal("Register() should return error for 400 response")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("APIError.StatusCode = %d, want %d", apiErr.StatusCode, http.StatusBadRequest)
	}
	if apiErr.Message != "name is required" {
		t.Errorf("APIError.Message = %q, want %q", apiErr.Message, "name is required")
	}

	// Verify the Error() method.
	errStr := apiErr.Error()
	if errStr == "" {
		t.Error("APIError.Error() should return non-empty string")
	}
}
