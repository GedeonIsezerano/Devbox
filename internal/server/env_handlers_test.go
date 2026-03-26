package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// createTestProject creates a project via the API and returns its ID.
func createTestProject(t *testing.T, srv *Server, token, name, envFile string) string {
	t.Helper()

	body := jsonBody(createProjectRequest{Name: name, EnvFile: envFile})
	req := authRequest(httptest.NewRequest("POST", "/projects", body), token)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %s", resp.Code, resp.Body.String())
	}

	var result projectResponse
	json.NewDecoder(resp.Body).Decode(&result)
	return result.ID
}

func TestPushEnv(t *testing.T) {
	srv := newTestServer(t)
	_, token := createTestUserWithSession(t, srv, "alice")
	projectID := createTestProject(t, srv, token, "test-project", ".env")

	plaintext := "SECRET_KEY=abc123\nDB_URL=postgres://localhost"
	data := base64.StdEncoding.EncodeToString([]byte(plaintext))

	body := jsonBody(pushEnvRequest{
		Data:            data,
		ExpectedVersion: 0,
	})
	req := authRequest(httptest.NewRequest("PUT", "/projects/"+projectID+"/env", body), token)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("push env: expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var result pushEnvResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if result.Version != 1 {
		t.Fatalf("expected version 1, got %d", result.Version)
	}
}

func TestPullEnv(t *testing.T) {
	srv := newTestServer(t)
	_, token := createTestUserWithSession(t, srv, "alice")
	projectID := createTestProject(t, srv, token, "test-project", ".env.local")

	// Push env data first.
	plaintext := "SECRET_KEY=abc123\nDB_URL=postgres://localhost"
	data := base64.StdEncoding.EncodeToString([]byte(plaintext))

	pushBody := jsonBody(pushEnvRequest{
		Data:            data,
		ExpectedVersion: 0,
	})
	pushReq := authRequest(httptest.NewRequest("PUT", "/projects/"+projectID+"/env", pushBody), token)
	pushReq.Header.Set("Content-Type", "application/json")
	pushResp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(pushResp, pushReq)

	if pushResp.Code != http.StatusOK {
		t.Fatalf("push env: expected 200, got %d: %s", pushResp.Code, pushResp.Body.String())
	}

	// Pull it back.
	pullReq := authRequest(httptest.NewRequest("GET", "/projects/"+projectID+"/env", nil), token)
	pullResp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(pullResp, pullReq)

	if pullResp.Code != http.StatusOK {
		t.Fatalf("pull env: expected 200, got %d: %s", pullResp.Code, pullResp.Body.String())
	}

	var result pullEnvResponse
	if err := json.NewDecoder(pullResp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if result.Version != 1 {
		t.Fatalf("expected version 1, got %d", result.Version)
	}

	if result.EnvFile != ".env.local" {
		t.Fatalf("expected env_file .env.local, got %s", result.EnvFile)
	}

	// Decode the data and compare.
	decoded, err := base64.StdEncoding.DecodeString(result.Data)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}

	if string(decoded) != plaintext {
		t.Fatalf("expected plaintext %q, got %q", plaintext, string(decoded))
	}
}

func TestPushEnvVersionConflict(t *testing.T) {
	srv := newTestServer(t)
	_, token := createTestUserWithSession(t, srv, "alice")
	projectID := createTestProject(t, srv, token, "test-project", ".env")

	// Push version 1.
	data := base64.StdEncoding.EncodeToString([]byte("KEY=v1"))
	body := jsonBody(pushEnvRequest{Data: data, ExpectedVersion: 0})
	req := authRequest(httptest.NewRequest("PUT", "/projects/"+projectID+"/env", body), token)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("first push: expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	// Push version 2.
	data = base64.StdEncoding.EncodeToString([]byte("KEY=v2"))
	body = jsonBody(pushEnvRequest{Data: data, ExpectedVersion: 1})
	req = authRequest(httptest.NewRequest("PUT", "/projects/"+projectID+"/env", body), token)
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("second push: expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	// Try to push with stale expected_version=1 (current is 2) — should get 409.
	data = base64.StdEncoding.EncodeToString([]byte("KEY=v3-stale"))
	body = jsonBody(pushEnvRequest{Data: data, ExpectedVersion: 1})
	req = authRequest(httptest.NewRequest("PUT", "/projects/"+projectID+"/env", body), token)
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", resp.Code, resp.Body.String())
	}

	var errResp map[string]any
	json.NewDecoder(resp.Body).Decode(&errResp)

	if errResp["error"] != "version conflict" {
		t.Fatalf("expected error 'version conflict', got %v", errResp["error"])
	}
	if int(errResp["current_version"].(float64)) != 2 {
		t.Fatalf("expected current_version 2, got %v", errResp["current_version"])
	}
}

func TestGetEnvVersion(t *testing.T) {
	srv := newTestServer(t)
	_, token := createTestUserWithSession(t, srv, "alice")
	projectID := createTestProject(t, srv, token, "test-project", ".env")

	// Before any push, version should be 0.
	req := authRequest(httptest.NewRequest("GET", "/projects/"+projectID+"/env/version", nil), token)
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var result envVersionResponse
	json.NewDecoder(resp.Body).Decode(&result)

	if result.Version != 0 {
		t.Fatalf("expected version 0 before any push, got %d", result.Version)
	}

	// Push some data.
	data := base64.StdEncoding.EncodeToString([]byte("KEY=val"))
	pushBody := jsonBody(pushEnvRequest{Data: data, ExpectedVersion: 0})
	pushReq := authRequest(httptest.NewRequest("PUT", "/projects/"+projectID+"/env", pushBody), token)
	pushReq.Header.Set("Content-Type", "application/json")
	pushResp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(pushResp, pushReq)

	if pushResp.Code != http.StatusOK {
		t.Fatalf("push: expected 200, got %d: %s", pushResp.Code, pushResp.Body.String())
	}

	// Now version should be 1.
	req = authRequest(httptest.NewRequest("GET", "/projects/"+projectID+"/env/version", nil), token)
	resp = httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	json.NewDecoder(resp.Body).Decode(&result)

	if result.Version != 1 {
		t.Fatalf("expected version 1, got %d", result.Version)
	}
}

func TestEnvUnauthorized(t *testing.T) {
	srv := newTestServer(t)
	_, token1 := createTestUserWithSession(t, srv, "alice")
	_, token2 := createTestUserWithSession(t, srv, "bob")

	// Alice creates a project.
	projectID := createTestProject(t, srv, token1, "alice-project", ".env")

	// Bob tries to pull env — should get 404 (not 403).
	req := authRequest(httptest.NewRequest("GET", "/projects/"+projectID+"/env", nil), token2)
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-member pull, got %d: %s", resp.Code, resp.Body.String())
	}

	// Bob tries to push env — should get 404.
	data := base64.StdEncoding.EncodeToString([]byte("KEY=val"))
	pushBody := jsonBody(pushEnvRequest{Data: data, ExpectedVersion: 0})
	req = authRequest(httptest.NewRequest("PUT", "/projects/"+projectID+"/env", pushBody), token2)
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-member push, got %d: %s", resp.Code, resp.Body.String())
	}

	// Bob tries to get env version — should get 404.
	req = authRequest(httptest.NewRequest("GET", "/projects/"+projectID+"/env/version", nil), token2)
	resp = httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-member version, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestPushEnvUpdatesEnvFile(t *testing.T) {
	srv := newTestServer(t)
	_, token := createTestUserWithSession(t, srv, "alice")
	projectID := createTestProject(t, srv, token, "test-project", ".env")

	// Push with a different env_file.
	data := base64.StdEncoding.EncodeToString([]byte("KEY=val"))
	body := jsonBody(pushEnvRequest{
		Data:            data,
		ExpectedVersion: 0,
		EnvFile:         ".env.production",
	})
	req := authRequest(httptest.NewRequest("PUT", "/projects/"+projectID+"/env", body), token)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("push: expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	// Pull should reflect the updated env_file.
	pullReq := authRequest(httptest.NewRequest("GET", "/projects/"+projectID+"/env", nil), token)
	pullResp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(pullResp, pullReq)

	if pullResp.Code != http.StatusOK {
		t.Fatalf("pull: expected 200, got %d: %s", pullResp.Code, pullResp.Body.String())
	}

	var result pullEnvResponse
	json.NewDecoder(pullResp.Body).Decode(&result)

	if result.EnvFile != ".env.production" {
		t.Fatalf("expected env_file .env.production, got %s", result.EnvFile)
	}
}

func TestPushEnvTooLarge(t *testing.T) {
	srv := newTestServer(t)
	_, token := createTestUserWithSession(t, srv, "alice")
	projectID := createTestProject(t, srv, token, "test-project", ".env")

	// Create a blob over 64KB.
	bigData := strings.Repeat("A", 65*1024) // 65KB of plaintext
	data := base64.StdEncoding.EncodeToString([]byte(bigData))

	body := jsonBody(pushEnvRequest{
		Data:            data,
		ExpectedVersion: 0,
	})
	req := authRequest(httptest.NewRequest("PUT", "/projects/"+projectID+"/env", body), token)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestPushEnvReaderCannotWrite(t *testing.T) {
	srv := newTestServer(t)
	_, token := createTestUserWithSession(t, srv, "alice")
	bobID, readerToken := createTestUserWithSession(t, srv, "bob")

	// Alice creates a project.
	projectID := createTestProject(t, srv, token, "test-project", ".env")

	// Add bob as a reader.
	_, err := srv.DB.Exec(
		"INSERT INTO project_members (project_id, user_id, role) VALUES (?, ?, ?)",
		projectID, bobID, "reader",
	)
	if err != nil {
		t.Fatalf("add reader member: %v", err)
	}

	// Bob (reader) can pull.
	// First, push some data as alice.
	data := base64.StdEncoding.EncodeToString([]byte("KEY=val"))
	pushBody := jsonBody(pushEnvRequest{Data: data, ExpectedVersion: 0})
	pushReq := authRequest(httptest.NewRequest("PUT", "/projects/"+projectID+"/env", pushBody), token)
	pushReq.Header.Set("Content-Type", "application/json")
	pushResp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(pushResp, pushReq)

	if pushResp.Code != http.StatusOK {
		t.Fatalf("alice push: expected 200, got %d: %s", pushResp.Code, pushResp.Body.String())
	}

	// Bob can read env.
	pullReq := authRequest(httptest.NewRequest("GET", "/projects/"+projectID+"/env", nil), readerToken)
	pullResp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(pullResp, pullReq)

	if pullResp.Code != http.StatusOK {
		t.Fatalf("bob pull: expected 200, got %d: %s", pullResp.Code, pullResp.Body.String())
	}

	// Bob (reader) cannot push — should get 404 (insufficient role treated as not found).
	data = base64.StdEncoding.EncodeToString([]byte("KEY=hacked"))
	pushBody = jsonBody(pushEnvRequest{Data: data, ExpectedVersion: 1})
	pushReq = authRequest(httptest.NewRequest("PUT", "/projects/"+projectID+"/env", pushBody), readerToken)
	pushReq.Header.Set("Content-Type", "application/json")
	pushResp = httptest.NewRecorder()
	srv.Handler.ServeHTTP(pushResp, pushReq)

	if pushResp.Code != http.StatusNotFound {
		t.Fatalf("bob push: expected 404, got %d: %s", pushResp.Code, pushResp.Body.String())
	}
}

func TestPushEnvPathTraversal(t *testing.T) {
	srv := newTestServer(t)
	_, token := createTestUserWithSession(t, srv, "alice")
	projectID := createTestProject(t, srv, token, "test-project", ".env")

	data := base64.StdEncoding.EncodeToString([]byte("KEY=val"))

	tests := []struct {
		name    string
		envFile string
	}{
		{"dotdot", "../../etc/passwd"},
		{"slash", "/etc/passwd"},
		{"backslash", "..\\windows\\system32"},
		{"no_dot_env_prefix", "secret.txt"},
		{"dotdot_in_name", ".env.."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := jsonBody(pushEnvRequest{
				Data:            data,
				ExpectedVersion: 0,
				EnvFile:         tt.envFile,
			})
			req := authRequest(httptest.NewRequest("PUT", "/projects/"+projectID+"/env", body), token)
			req.Header.Set("Content-Type", "application/json")
			resp := httptest.NewRecorder()
			srv.Handler.ServeHTTP(resp, req)

			if resp.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for env_file %q, got %d: %s", tt.envFile, resp.Code, resp.Body.String())
			}
		})
	}
}

func TestCreateProjectPathTraversal(t *testing.T) {
	srv := newTestServer(t)
	_, token := createTestUserWithSession(t, srv, "alice")

	tests := []struct {
		name    string
		envFile string
	}{
		{"dotdot", "../../etc/passwd"},
		{"slash", "/etc/passwd"},
		{"no_prefix", "secret.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := jsonBody(createProjectRequest{
				Name:    "test-" + tt.name,
				EnvFile: tt.envFile,
			})
			req := authRequest(httptest.NewRequest("POST", "/projects", body), token)
			req.Header.Set("Content-Type", "application/json")
			resp := httptest.NewRecorder()
			srv.Handler.ServeHTTP(resp, req)

			if resp.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for env_file %q, got %d: %s", tt.envFile, resp.Code, resp.Body.String())
			}
		})
	}
}
