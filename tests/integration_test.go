package tests

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/user/devbox/internal/cli"
	"github.com/user/devbox/internal/crypto"
	"github.com/user/devbox/internal/database"
	"github.com/user/devbox/internal/server"
)

// testEnv holds the shared state for an integration test session.
type testEnv struct {
	Server    *server.Server
	TS        *httptest.Server
	Client    *cli.Client
	Signer    gossh.Signer
	PubKeyStr string
	FP        string
	UserID    string
}

// setupTestEnv creates an in-memory server, registers a user via SSH
// challenge-response, and returns a fully authenticated client.
func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	// 1. Open in-memory SQLite.
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// 2. Create age encryptor from raw key bytes.
	enc, err := crypto.NewAgeEncryptor([]byte("integration-test-master-key-32b!"))
	if err != nil {
		t.Fatalf("create encryptor: %v", err)
	}

	// 3. Nonce store.
	nonces := crypto.NewNonceStore(5 * time.Minute)
	t.Cleanup(func() { nonces.Stop() })

	// 4. Build server.
	srv := server.NewServer(server.Config{
		ListenAddr: ":0",
		DB:         db,
		Encryptor:  enc,
		Nonces:     nonces,
	})

	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(func() { ts.Close() })

	// 5. Generate an SSH key pair.
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ecdsa key: %v", err)
	}
	signer, err := gossh.NewSignerFromKey(ecKey)
	if err != nil {
		t.Fatalf("create ssh signer: %v", err)
	}
	pubKey := signer.PublicKey()
	fingerprint := gossh.FingerprintSHA256(pubKey)
	authorizedKey := string(gossh.MarshalAuthorizedKey(pubKey))

	// 6. Create an unauthenticated client.
	client, err := cli.NewClient(ts.URL, "", "")
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	// 7. Register user.
	regResp, err := client.Register("testuser", authorizedKey, fingerprint)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// 8. Authenticate via challenge-response.
	challengeResp, err := client.Challenge()
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}

	nonceBytes, err := hex.DecodeString(challengeResp.Nonce)
	if err != nil {
		t.Fatalf("decode nonce: %v", err)
	}
	sigBytes, err := crypto.SignSSH(signer, nonceBytes)
	if err != nil {
		t.Fatalf("sign nonce: %v", err)
	}

	verifyResp, err := client.Verify(fingerprint, sigBytes, challengeResp.Nonce)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	client.AuthToken = verifyResp.SessionToken

	return &testEnv{
		Server:    srv,
		TS:        ts,
		Client:    client,
		Signer:    signer,
		PubKeyStr: authorizedKey,
		FP:        fingerprint,
		UserID:    regResp.UserID,
	}
}

func TestFullPushPullCycle(t *testing.T) {
	env := setupTestEnv(t)

	// --- Set up a fake git repo with package.json (Node.js) ---
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "remote", "add", "origin", "https://github.com/testuser/testproject.git")
	if err := os.WriteFile(filepath.Join(repoDir, "package.json"), []byte(`{"name":"testproject","version":"1.0.0"}`), 0644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}

	// --- Create a realistic .env.local ---
	envContent := `NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY=pk_test_abc123
CLERK_SECRET_KEY=sk_test_secret456
DATABASE_URL=postgresql://localhost:5432/mydb
NEXT_PUBLIC_SENTRY_DSN=https://examplePublicKey@o0.ingest.sentry.io/0
STRIPE_SECRET_KEY=sk_test_stripe789
REDIS_URL=redis://localhost:6379`

	envPath := filepath.Join(repoDir, ".env.local")
	if err := os.WriteFile(envPath, []byte(envContent), 0644); err != nil {
		t.Fatalf("write .env.local: %v", err)
	}

	// --- Verify env detection ---
	filename, projectType := cli.DetectEnvFile(repoDir)
	if filename != ".env.local" {
		t.Fatalf("expected .env.local, got %s", filename)
	}
	if projectType != "Node.js" {
		t.Fatalf("expected Node.js, got %s", projectType)
	}

	// --- Create project ---
	projResp, err := env.Client.CreateProject("testproject", "github.com/testuser/testproject", ".env.local")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if projResp.ID == "" {
		t.Fatal("expected non-empty project ID")
	}
	if projResp.Name != "testproject" {
		t.Fatalf("expected name testproject, got %s", projResp.Name)
	}
	if projResp.EnvFile != ".env.local" {
		t.Fatalf("expected env_file .env.local, got %s", projResp.EnvFile)
	}

	// --- Push env vars (version 0 -> 1) ---
	encoded := base64.StdEncoding.EncodeToString([]byte(envContent))
	pushResp, err := env.Client.PushEnv(projResp.ID, encoded, 0, ".env.local")
	if err != nil {
		t.Fatalf("push env (v0->v1): %v", err)
	}
	if pushResp.Version != 1 {
		t.Fatalf("expected version 1, got %d", pushResp.Version)
	}

	// --- Pull env vars and verify content integrity ---
	pullResp, err := env.Client.PullEnv(projResp.ID)
	if err != nil {
		t.Fatalf("pull env: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(pullResp.Data)
	if err != nil {
		t.Fatalf("decode pulled data: %v", err)
	}
	if string(decoded) != envContent {
		t.Fatalf("content mismatch after pull.\nExpected:\n%s\nGot:\n%s", envContent, string(decoded))
	}
	if pullResp.Version != 1 {
		t.Fatalf("expected pull version 1, got %d", pullResp.Version)
	}
	if pullResp.EnvFile != ".env.local" {
		t.Fatalf("expected env_file .env.local, got %s", pullResp.EnvFile)
	}

	// --- Push again (version 1 -> 2) ---
	pushResp2, err := env.Client.PushEnv(projResp.ID, encoded, 1, ".env.local")
	if err != nil {
		t.Fatalf("push env (v1->v2): %v", err)
	}
	if pushResp2.Version != 2 {
		t.Fatalf("expected version 2, got %d", pushResp2.Version)
	}

	// --- Optimistic locking: push with stale version should return 409 ---
	_, err = env.Client.PushEnv(projResp.ID, encoded, 1, ".env.local")
	if err == nil {
		t.Fatal("expected error on stale version push, got nil")
	}
	var apiErr *cli.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 409 {
		t.Fatalf("expected status 409, got %d", apiErr.StatusCode)
	}

	// --- Verify version via dedicated endpoint ---
	version, err := env.Client.GetEnvVersion(projResp.ID)
	if err != nil {
		t.Fatalf("get env version: %v", err)
	}
	if version != 2 {
		t.Fatalf("expected version 2, got %d", version)
	}

	// --- Create a PAT and use it to pull ---
	tokenResp, err := env.Client.CreateToken("test-pat", "pat", "project:read", "90d", "")
	if err != nil {
		t.Fatalf("create PAT: %v", err)
	}
	if tokenResp.Token == "" {
		t.Fatal("expected non-empty PAT token")
	}
	if tokenResp.ID == "" {
		t.Fatal("expected non-empty PAT ID")
	}

	// Authenticate with the PAT.
	patClient, err := cli.NewClient(env.TS.URL, "", "")
	if err != nil {
		t.Fatalf("create pat client: %v", err)
	}
	patAuthResp, err := patClient.TokenAuth(tokenResp.Token)
	if err != nil {
		t.Fatalf("PAT auth: %v", err)
	}
	patClient.AuthToken = patAuthResp.SessionToken

	// Pull with PAT-authenticated client.
	patPullResp, err := patClient.PullEnv(projResp.ID)
	if err != nil {
		t.Fatalf("PAT pull: %v", err)
	}
	patDecoded, err := base64.StdEncoding.DecodeString(patPullResp.Data)
	if err != nil {
		t.Fatalf("decode PAT pull data: %v", err)
	}
	if string(patDecoded) != envContent {
		t.Fatal("PAT pull content does not match original")
	}

	// --- Create provision token, use once, verify second use fails ---
	provResp, err := env.Client.CreateToken("test-prov", "provision", "project:read", "1h", projResp.ID)
	if err != nil {
		t.Fatalf("create provision token: %v", err)
	}
	if provResp.Token == "" {
		t.Fatal("expected non-empty provision token")
	}

	// First use should succeed.
	provClient, err := cli.NewClient(env.TS.URL, "", "")
	if err != nil {
		t.Fatalf("create prov client: %v", err)
	}
	provAuthResp, err := provClient.TokenAuth(provResp.Token)
	if err != nil {
		t.Fatalf("provision token first use: %v", err)
	}
	if provAuthResp.SessionToken == "" {
		t.Fatal("expected session token from provision auth")
	}

	// Second use should fail (token consumed).
	provClient2, err := cli.NewClient(env.TS.URL, "", "")
	if err != nil {
		t.Fatalf("create prov client 2: %v", err)
	}
	_, err = provClient2.TokenAuth(provResp.Token)
	if err == nil {
		t.Fatal("expected error on second use of provision token")
	}
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 401 {
		t.Fatalf("expected status 401, got %d", apiErr.StatusCode)
	}

	// --- Test Python project detection ---
	pythonDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(pythonDir, "pyproject.toml"), []byte(`[project]
name = "myapp"
version = "0.1.0"
`), 0644); err != nil {
		t.Fatalf("write pyproject.toml: %v", err)
	}
	pyFilename, pyProjectType := cli.DetectEnvFile(pythonDir)
	if pyFilename != ".env" {
		t.Fatalf("expected .env for Python, got %s", pyFilename)
	}
	if pyProjectType != "Python" {
		t.Fatalf("expected Python, got %s", pyProjectType)
	}

	// --- List projects and verify ours is present ---
	projects, err := env.Client.ListProjects()
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	if projects[0].ID != projResp.ID {
		t.Fatalf("expected project ID %s, got %s", projResp.ID, projects[0].ID)
	}

	// --- List tokens ---
	tokens, err := env.Client.ListTokens()
	if err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	// The PAT should still be present. The provision token was consumed (deleted).
	foundPAT := false
	for _, tok := range tokens {
		if tok.ID == tokenResp.ID {
			foundPAT = true
		}
	}
	if !foundPAT {
		t.Fatal("expected to find PAT in token list")
	}

	// --- Revoke the PAT ---
	if err := env.Client.RevokeToken(tokenResp.ID); err != nil {
		t.Fatalf("revoke token: %v", err)
	}
	tokensAfter, err := env.Client.ListTokens()
	if err != nil {
		t.Fatalf("list tokens after revoke: %v", err)
	}
	for _, tok := range tokensAfter {
		if tok.ID == tokenResp.ID {
			t.Fatal("PAT should be gone after revocation")
		}
	}

	// --- Delete project ---
	if err := env.Client.DeleteProject(projResp.ID); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	projectsAfter, err := env.Client.ListProjects()
	if err != nil {
		t.Fatalf("list projects after delete: %v", err)
	}
	if len(projectsAfter) != 0 {
		t.Fatalf("expected 0 projects after delete, got %d", len(projectsAfter))
	}

	// --- Logout ---
	if err := env.Client.Logout(); err != nil {
		t.Fatalf("logout: %v", err)
	}
	// Authenticated request after logout should fail.
	_, err = env.Client.ListProjects()
	if err == nil {
		t.Fatal("expected error after logout")
	}
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 401 {
		t.Fatalf("expected 401 after logout, got %d", apiErr.StatusCode)
	}
}

func TestEnvFileDetectionIntegration(t *testing.T) {
	tests := []struct {
		name         string
		markerFile   string
		markerData   string
		wantFilename string
		wantType     string
	}{
		{
			name:         "Next.js (next.config.js)",
			markerFile:   "next.config.js",
			markerData:   "module.exports = {}",
			wantFilename: ".env.local",
			wantType:     "Next.js",
		},
		{
			name:         "Next.js (next.config.ts)",
			markerFile:   "next.config.ts",
			markerData:   "export default {}",
			wantFilename: ".env.local",
			wantType:     "Next.js",
		},
		{
			name:         "Next.js (next.config.mjs)",
			markerFile:   "next.config.mjs",
			markerData:   "export default {}",
			wantFilename: ".env.local",
			wantType:     "Next.js",
		},
		{
			name:         "Nuxt",
			markerFile:   "nuxt.config.ts",
			markerData:   "export default defineNuxtConfig({})",
			wantFilename: ".env.local",
			wantType:     "Nuxt",
		},
		{
			name:         "TypeScript",
			markerFile:   "tsconfig.json",
			markerData:   `{"compilerOptions":{}}`,
			wantFilename: ".env.local",
			wantType:     "TypeScript",
		},
		{
			name:         "Node.js (package.json)",
			markerFile:   "package.json",
			markerData:   `{"name":"test","version":"1.0.0"}`,
			wantFilename: ".env.local",
			wantType:     "Node.js",
		},
		{
			name:         "Python (pyproject.toml)",
			markerFile:   "pyproject.toml",
			markerData:   `[project]\nname = "test"`,
			wantFilename: ".env",
			wantType:     "Python",
		},
		{
			name:         "Python (requirements.txt)",
			markerFile:   "requirements.txt",
			markerData:   "flask==2.0.0\nrequests==2.28.0",
			wantFilename: ".env",
			wantType:     "Python",
		},
		{
			name:         "Go",
			markerFile:   "go.mod",
			markerData:   "module example.com/test\n\ngo 1.21",
			wantFilename: ".env",
			wantType:     "Go",
		},
		{
			name:         "Rust",
			markerFile:   "Cargo.toml",
			markerData:   `[package]\nname = "test"\nversion = "0.1.0"`,
			wantFilename: ".env",
			wantType:     "Rust",
		},
		{
			name:         "Ruby",
			markerFile:   "Gemfile",
			markerData:   "source 'https://rubygems.org'\ngem 'rails'",
			wantFilename: ".env",
			wantType:     "Ruby",
		},
		{
			name:         "PHP",
			markerFile:   "composer.json",
			markerData:   `{"name":"vendor/project"}`,
			wantFilename: ".env",
			wantType:     "PHP",
		},
		{
			name:         "Unknown (no marker files)",
			markerFile:   "", // no marker file
			markerData:   "",
			wantFilename: ".env",
			wantType:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.markerFile != "" {
				if err := os.WriteFile(filepath.Join(dir, tt.markerFile), []byte(tt.markerData), 0644); err != nil {
					t.Fatalf("write marker file %s: %v", tt.markerFile, err)
				}
			}

			filename, projectType := cli.DetectEnvFile(dir)
			if filename != tt.wantFilename {
				t.Errorf("filename: got %q, want %q", filename, tt.wantFilename)
			}
			if projectType != tt.wantType {
				t.Errorf("projectType: got %q, want %q", projectType, tt.wantType)
			}
		})
	}
}

func TestMultipleProjectsIsolation(t *testing.T) {
	env := setupTestEnv(t)

	// Create two projects with different env content.
	proj1, err := env.Client.CreateProject("project-alpha", "github.com/test/alpha", ".env.local")
	if err != nil {
		t.Fatalf("create project 1: %v", err)
	}
	proj2, err := env.Client.CreateProject("project-beta", "github.com/test/beta", ".env")
	if err != nil {
		t.Fatalf("create project 2: %v", err)
	}

	env1 := "ALPHA_SECRET=alpha123\nALPHA_DB=postgres://alpha"
	env2 := "BETA_SECRET=beta456\nBETA_REDIS=redis://beta"

	// Push to project 1.
	enc1 := base64.StdEncoding.EncodeToString([]byte(env1))
	_, err = env.Client.PushEnv(proj1.ID, enc1, 0, ".env.local")
	if err != nil {
		t.Fatalf("push proj1: %v", err)
	}

	// Push to project 2.
	enc2 := base64.StdEncoding.EncodeToString([]byte(env2))
	_, err = env.Client.PushEnv(proj2.ID, enc2, 0, ".env")
	if err != nil {
		t.Fatalf("push proj2: %v", err)
	}

	// Pull from project 1 and verify.
	pull1, err := env.Client.PullEnv(proj1.ID)
	if err != nil {
		t.Fatalf("pull proj1: %v", err)
	}
	dec1, _ := base64.StdEncoding.DecodeString(pull1.Data)
	if string(dec1) != env1 {
		t.Fatalf("proj1 content mismatch.\nExpected: %s\nGot: %s", env1, string(dec1))
	}
	if pull1.EnvFile != ".env.local" {
		t.Fatalf("proj1 env_file: got %s, want .env.local", pull1.EnvFile)
	}

	// Pull from project 2 and verify.
	pull2, err := env.Client.PullEnv(proj2.ID)
	if err != nil {
		t.Fatalf("pull proj2: %v", err)
	}
	dec2, _ := base64.StdEncoding.DecodeString(pull2.Data)
	if string(dec2) != env2 {
		t.Fatalf("proj2 content mismatch.\nExpected: %s\nGot: %s", env2, string(dec2))
	}
	if pull2.EnvFile != ".env" {
		t.Fatalf("proj2 env_file: got %s, want .env", pull2.EnvFile)
	}
}

func TestEncryptionIntegrity(t *testing.T) {
	// Verify that the full cycle encrypt->store->pull->decrypt returns exact content,
	// including special characters, multi-line values, and edge cases.
	env := setupTestEnv(t)

	proj, err := env.Client.CreateProject("special-chars", "", ".env")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	// Content with special characters, equals signs in values, empty lines, etc.
	content := `DATABASE_URL=postgres://user:p@ss=w0rd@host:5432/db?sslmode=require&options=-c search_path=public
API_KEY=sk_live_abc123+def/456==
MULTILINE_B64=eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.signature
EMPTY_VALUE=
SPECIAL_CHARS=!@#$%^&*()_+-=[]{}|;':\",./<>?
UNICODE_VALUE=Hello World`

	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	_, err = env.Client.PushEnv(proj.ID, encoded, 0, ".env")
	if err != nil {
		t.Fatalf("push: %v", err)
	}

	pullResp, err := env.Client.PullEnv(proj.ID)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}

	decoded, err := base64.StdEncoding.DecodeString(pullResp.Data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if string(decoded) != content {
		t.Fatalf("content mismatch after encrypt/decrypt cycle.\nExpected length: %d\nGot length: %d\nExpected:\n%s\nGot:\n%s",
			len(content), len(decoded), content, string(decoded))
	}
}

func TestUnauthenticatedAccess(t *testing.T) {
	env := setupTestEnv(t)

	// Create a project as the authenticated user.
	proj, err := env.Client.CreateProject("auth-test", "", ".env")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	// Unauthenticated client.
	unauth, err := cli.NewClient(env.TS.URL, "", "")
	if err != nil {
		t.Fatalf("create unauth client: %v", err)
	}

	// All project/env endpoints should return 401.
	var apiErr *cli.APIError

	_, err = unauth.ListProjects()
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 401 {
		t.Fatalf("list projects: expected 401, got %v", err)
	}

	_, err = unauth.CreateProject("nope", "", ".env")
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 401 {
		t.Fatalf("create project: expected 401, got %v", err)
	}

	_, err = unauth.PullEnv(proj.ID)
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 401 {
		t.Fatalf("pull env: expected 401, got %v", err)
	}

	encoded := base64.StdEncoding.EncodeToString([]byte("SECRET=nope"))
	_, err = unauth.PushEnv(proj.ID, encoded, 0, ".env")
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 401 {
		t.Fatalf("push env: expected 401, got %v", err)
	}

	_, err = unauth.CreateToken("nope", "pat", "project:read", "1h", "")
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 401 {
		t.Fatalf("create token: expected 401, got %v", err)
	}
}

func TestSSHAuthFlow(t *testing.T) {
	// Verify the full SSH challenge-response flow works end-to-end via HTTP.
	env := setupTestEnv(t)

	// The setupTestEnv already exercised one full SSH auth flow.
	// Now verify that a second auth creates a new distinct session.
	challengeResp, err := env.Client.Challenge()
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}

	nonceBytes, _ := hex.DecodeString(challengeResp.Nonce)
	sigBytes, err := crypto.SignSSH(env.Signer, nonceBytes)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	verifyResp, err := env.Client.Verify(env.FP, sigBytes, challengeResp.Nonce)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	if verifyResp.SessionToken == "" {
		t.Fatal("expected non-empty session token")
	}
	if verifyResp.UserID != env.UserID {
		t.Fatalf("expected user_id %s, got %s", env.UserID, verifyResp.UserID)
	}

	// The old session should still work.
	_, err = env.Client.ListProjects()
	if err != nil {
		t.Fatalf("old session should still work: %v", err)
	}

	// The new session should also work.
	newClient, err := cli.NewClient(env.TS.URL, verifyResp.SessionToken, "")
	if err != nil {
		t.Fatalf("create new client: %v", err)
	}
	_, err = newClient.ListProjects()
	if err != nil {
		t.Fatalf("new session should work: %v", err)
	}
}

func TestVersionConflictDetails(t *testing.T) {
	env := setupTestEnv(t)

	proj, err := env.Client.CreateProject("conflict-test", "", ".env")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	data := base64.StdEncoding.EncodeToString([]byte("V1=first"))

	// Push version 1.
	_, err = env.Client.PushEnv(proj.ID, data, 0, ".env")
	if err != nil {
		t.Fatalf("push v1: %v", err)
	}

	// Push version 2.
	data2 := base64.StdEncoding.EncodeToString([]byte("V2=second"))
	_, err = env.Client.PushEnv(proj.ID, data2, 1, ".env")
	if err != nil {
		t.Fatalf("push v2: %v", err)
	}

	// Attempt to push with version 1 (should fail with 409).
	data3 := base64.StdEncoding.EncodeToString([]byte("V3=third"))
	_, err = env.Client.PushEnv(proj.ID, data3, 1, ".env")
	if err == nil {
		t.Fatal("expected conflict error")
	}
	var apiErr *cli.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 409 {
		t.Fatalf("expected 409, got %d", apiErr.StatusCode)
	}

	// Verify the correct version (2) is still stored.
	pullResp, err := env.Client.PullEnv(proj.ID)
	if err != nil {
		t.Fatalf("pull after conflict: %v", err)
	}
	if pullResp.Version != 2 {
		t.Fatalf("expected version 2 after conflict, got %d", pullResp.Version)
	}
	decoded, _ := base64.StdEncoding.DecodeString(pullResp.Data)
	if string(decoded) != "V2=second" {
		t.Fatalf("content should be V2=second after conflict, got %s", string(decoded))
	}
}

// --- Helper ---

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"HOME="+dir,
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
