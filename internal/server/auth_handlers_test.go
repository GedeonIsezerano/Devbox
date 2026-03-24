package server

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	gossh "golang.org/x/crypto/ssh"

	"github.com/user/devbox/internal/crypto"
	"github.com/user/devbox/internal/database"
)

// --- Test helpers ---

func jsonBody(v any) io.Reader {
	data, _ := json.Marshal(v)
	return bytes.NewReader(data)
}

func parseJSON(resp *httptest.ResponseRecorder, v any) {
	json.NewDecoder(resp.Body).Decode(v)
}

// generateTestKey creates an ECDSA key pair and returns the SSH signer,
// the authorized key string, and the SHA256 fingerprint.
func generateTestKey(t *testing.T) (gossh.Signer, string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ecdsa key: %v", err)
	}
	signer, err := gossh.NewSignerFromKey(key)
	if err != nil {
		t.Fatalf("create ssh signer: %v", err)
	}
	pubKey := signer.PublicKey()
	fingerprint := gossh.FingerprintSHA256(pubKey)
	authorizedKey := string(gossh.MarshalAuthorizedKey(pubKey))
	return signer, authorizedKey, fingerprint
}

// registerUser is a helper that registers a user via the API and returns the response.
func registerUser(t *testing.T, srv *Server, name, pubKey, fingerprint string) registerResponse {
	t.Helper()
	body := jsonBody(registerRequest{
		Name:        name,
		PublicKey:   pubKey,
		Fingerprint: fingerprint,
	})
	req := httptest.NewRequest("POST", "/auth/register", body)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d: %s", resp.Code, resp.Body.String())
	}

	var result registerResponse
	parseJSON(resp, &result)
	return result
}

// getChallenge is a helper that calls the challenge endpoint and returns the nonce.
func getChallenge(t *testing.T, srv *Server) string {
	t.Helper()
	req := httptest.NewRequest("POST", "/auth/challenge", nil)
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("challenge: expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var result challengeResponse
	parseJSON(resp, &result)
	return result.Nonce
}

// --- Registration Tests ---

func TestRegister(t *testing.T) {
	srv := newTestServer(t)
	_, pubKey, fingerprint := generateTestKey(t)

	body := jsonBody(registerRequest{
		Name:        "alice",
		PublicKey:   pubKey,
		Fingerprint: fingerprint,
	})
	req := httptest.NewRequest("POST", "/auth/register", body)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", resp.Code, resp.Body.String())
	}

	var result registerResponse
	parseJSON(resp, &result)

	if result.Name != "alice" {
		t.Fatalf("expected name alice, got %s", result.Name)
	}
	if result.UserID == "" {
		t.Fatal("expected non-empty user_id")
	}
}

func TestRegisterFirstUserIsAdmin(t *testing.T) {
	srv := newTestServer(t)

	// First user should be admin.
	_, pubKey1, fp1 := generateTestKey(t)
	resp1 := registerUser(t, srv, "first", pubKey1, fp1)
	if !resp1.IsAdmin {
		t.Fatal("expected first user to be admin")
	}

	// Second user should NOT be admin.
	_, pubKey2, fp2 := generateTestKey(t)
	resp2 := registerUser(t, srv, "second", pubKey2, fp2)
	if resp2.IsAdmin {
		t.Fatal("expected second user to NOT be admin")
	}
}

func TestRegisterDuplicateKey(t *testing.T) {
	srv := newTestServer(t)
	_, pubKey, fingerprint := generateTestKey(t)

	// Register first user.
	registerUser(t, srv, "alice", pubKey, fingerprint)

	// Attempt to register with the same fingerprint.
	body := jsonBody(registerRequest{
		Name:        "bob",
		PublicKey:   pubKey,
		Fingerprint: fingerprint,
	})
	req := httptest.NewRequest("POST", "/auth/register", body)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", resp.Code, resp.Body.String())
	}
}

// --- Challenge Tests ---

func TestChallenge(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest("POST", "/auth/challenge", nil)
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var result challengeResponse
	parseJSON(resp, &result)

	if result.Nonce == "" {
		t.Fatal("expected non-empty nonce")
	}
	if result.ExpiresAt == "" {
		t.Fatal("expected non-empty expires_at")
	}

	// Nonce should be valid hex.
	if _, err := hex.DecodeString(result.Nonce); err != nil {
		t.Fatalf("nonce is not valid hex: %v", err)
	}
}

// --- Verify Tests ---

func TestVerify(t *testing.T) {
	srv := newTestServer(t)
	signer, pubKey, fingerprint := generateTestKey(t)

	// 1. Register a user.
	regResp := registerUser(t, srv, "alice", pubKey, fingerprint)

	// 2. Get a challenge nonce.
	nonce := getChallenge(t, srv)

	// 3. Sign the nonce.
	nonceBytes, err := hex.DecodeString(nonce)
	if err != nil {
		t.Fatalf("decode nonce: %v", err)
	}
	sigBytes, err := crypto.SignSSH(signer, nonceBytes)
	if err != nil {
		t.Fatalf("sign nonce: %v", err)
	}
	sigB64 := base64.StdEncoding.EncodeToString(sigBytes)

	// 4. Verify.
	body := jsonBody(verifyRequest{
		Fingerprint: fingerprint,
		Signature:   sigB64,
		Nonce:       nonce,
	})
	req := httptest.NewRequest("POST", "/auth/verify", body)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var result verifyResponse
	parseJSON(resp, &result)

	if result.SessionToken == "" {
		t.Fatal("expected non-empty session_token")
	}
	if result.UserID != regResp.UserID {
		t.Fatalf("expected user_id %s, got %s", regResp.UserID, result.UserID)
	}
	if result.ExpiresIn != 900 {
		t.Fatalf("expected expires_in 900, got %d", result.ExpiresIn)
	}
}

func TestVerifyInvalidSignature(t *testing.T) {
	srv := newTestServer(t)
	_, pubKey, fingerprint := generateTestKey(t)

	// Register a user.
	registerUser(t, srv, "alice", pubKey, fingerprint)

	// Get a challenge nonce.
	nonce := getChallenge(t, srv)

	// Use a garbage signature.
	body := jsonBody(verifyRequest{
		Fingerprint: fingerprint,
		Signature:   base64.StdEncoding.EncodeToString([]byte("bad-signature-data")),
		Nonce:       nonce,
	})
	req := httptest.NewRequest("POST", "/auth/verify", body)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestVerifyExpiredNonce(t *testing.T) {
	srv := newTestServer(t)
	signer, pubKey, fingerprint := generateTestKey(t)

	// Register a user.
	registerUser(t, srv, "alice", pubKey, fingerprint)

	// Create a nonce and immediately consume it so it's "expired" / gone.
	nonce := getChallenge(t, srv)
	srv.Nonces.Consume(nonce) // consume it so it's no longer valid

	// Sign the nonce.
	nonceBytes, _ := hex.DecodeString(nonce)
	sigBytes, _ := crypto.SignSSH(signer, nonceBytes)
	sigB64 := base64.StdEncoding.EncodeToString(sigBytes)

	// Attempt to verify with the consumed nonce.
	body := jsonBody(verifyRequest{
		Fingerprint: fingerprint,
		Signature:   sigB64,
		Nonce:       nonce,
	})
	req := httptest.NewRequest("POST", "/auth/verify", body)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestVerifyUnknownFingerprint(t *testing.T) {
	srv := newTestServer(t)
	signer, _, _ := generateTestKey(t)

	// Get a challenge nonce (no user registered).
	nonce := getChallenge(t, srv)

	// Sign the nonce.
	nonceBytes, _ := hex.DecodeString(nonce)
	sigBytes, _ := crypto.SignSSH(signer, nonceBytes)
	sigB64 := base64.StdEncoding.EncodeToString(sigBytes)

	// Attempt to verify with an unknown fingerprint.
	body := jsonBody(verifyRequest{
		Fingerprint: "SHA256:unknownfingerprint",
		Signature:   sigB64,
		Nonce:       nonce,
	})
	req := httptest.NewRequest("POST", "/auth/verify", body)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", resp.Code, resp.Body.String())
	}
}

// --- Token Auth Tests ---

func TestTokenAuth(t *testing.T) {
	srv := newTestServer(t)
	db := srv.DB

	// Create a user.
	user, err := database.CreateUser(db, "alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Create a PAT.
	rawToken, err := crypto.GenerateToken("dbx_pat_")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	tokenHash := crypto.HashToken(rawToken)
	_, err = database.CreateToken(db, user.ID, "test-pat", tokenHash, "pat", `{"permissions":["admin"]}`, "", false)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	// Authenticate with the PAT.
	body := jsonBody(tokenAuthRequest{Token: rawToken})
	req := httptest.NewRequest("POST", "/auth/token", body)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var result tokenAuthResponse
	parseJSON(resp, &result)

	if result.SessionToken == "" {
		t.Fatal("expected non-empty session_token")
	}
	if result.UserID != user.ID {
		t.Fatalf("expected user_id %s, got %s", user.ID, result.UserID)
	}
	if result.ExpiresIn != 900 {
		t.Fatalf("expected expires_in 900, got %d", result.ExpiresIn)
	}
}

func TestTokenAuthExpired(t *testing.T) {
	srv := newTestServer(t)
	db := srv.DB

	// Create a user.
	user, err := database.CreateUser(db, "alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Create an expired PAT (expires_at in the past).
	rawToken, err := crypto.GenerateToken("dbx_pat_")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	tokenHash := crypto.HashToken(rawToken)
	_, err = database.CreateToken(db, user.ID, "expired-pat", tokenHash, "pat", `{"permissions":["admin"]}`, "2020-01-01T00:00:00Z", false)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	// Attempt to authenticate with the expired PAT.
	body := jsonBody(tokenAuthRequest{Token: rawToken})
	req := httptest.NewRequest("POST", "/auth/token", body)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestTokenAuthProvisionSingleUse(t *testing.T) {
	srv := newTestServer(t)
	db := srv.DB

	// Create a user.
	user, err := database.CreateUser(db, "alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Create a provision token (single use).
	rawToken, err := crypto.GenerateToken("dbx_prov_")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	tokenHash := crypto.HashToken(rawToken)
	_, err = database.CreateToken(db, user.ID, "provision", tokenHash, "provision", `{"permissions":["admin"]}`, "", true)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	// First use should succeed.
	body := jsonBody(tokenAuthRequest{Token: rawToken})
	req := httptest.NewRequest("POST", "/auth/token", body)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("first use: expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var result tokenAuthResponse
	parseJSON(resp, &result)
	if result.SessionToken == "" {
		t.Fatal("expected non-empty session_token on first use")
	}

	// Second use should fail (token consumed).
	body = jsonBody(tokenAuthRequest{Token: rawToken})
	req = httptest.NewRequest("POST", "/auth/token", body)
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("second use: expected 401, got %d: %s", resp.Code, resp.Body.String())
	}
}

// --- Logout Tests ---

func TestLogout(t *testing.T) {
	srv := newTestServer(t)
	signer, pubKey, fingerprint := generateTestKey(t)

	// 1. Register a user.
	registerUser(t, srv, "alice", pubKey, fingerprint)

	// 2. Authenticate via SSH to get a session token.
	nonce := getChallenge(t, srv)
	nonceBytes, _ := hex.DecodeString(nonce)
	sigBytes, _ := crypto.SignSSH(signer, nonceBytes)
	sigB64 := base64.StdEncoding.EncodeToString(sigBytes)

	verifyBody := jsonBody(verifyRequest{
		Fingerprint: fingerprint,
		Signature:   sigB64,
		Nonce:       nonce,
	})
	verifyReq := httptest.NewRequest("POST", "/auth/verify", verifyBody)
	verifyReq.Header.Set("Content-Type", "application/json")
	verifyResp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(verifyResp, verifyReq)

	if verifyResp.Code != http.StatusOK {
		t.Fatalf("verify: expected 200, got %d: %s", verifyResp.Code, verifyResp.Body.String())
	}

	var verifyResult verifyResponse
	parseJSON(verifyResp, &verifyResult)
	sessionToken := verifyResult.SessionToken

	// 3. Logout.
	logoutReq := httptest.NewRequest("POST", "/auth/logout", nil)
	logoutReq.Header.Set("Authorization", "Bearer "+sessionToken)
	logoutResp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(logoutResp, logoutReq)

	if logoutResp.Code != http.StatusNoContent {
		t.Fatalf("logout: expected 204, got %d: %s", logoutResp.Code, logoutResp.Body.String())
	}

	// 4. Subsequent request with the same token should return 401.
	checkReq := httptest.NewRequest("POST", "/auth/logout", nil)
	checkReq.Header.Set("Authorization", "Bearer "+sessionToken)
	checkResp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(checkResp, checkReq)

	if checkResp.Code != http.StatusUnauthorized {
		t.Fatalf("after logout: expected 401, got %d: %s", checkResp.Code, checkResp.Body.String())
	}
}
