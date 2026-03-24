package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/user/devbox/internal/crypto"
	"github.com/user/devbox/internal/database"
)

// createTestUserWithSession creates a user and session directly in the DB,
// returning the user ID and raw bearer token (no auth flow needed).
func createTestUserWithSession(t *testing.T, srv *Server, name string) (userID, bearerToken string) {
	t.Helper()

	user, err := database.CreateUser(srv.DB, name)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	rawToken := "test-session-token-" + user.ID
	hash := crypto.HashToken(rawToken)
	_, err = database.CreateSession(srv.DB, user.ID, hash, "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	return user.ID, rawToken
}

// authRequest sets the Authorization header on a request.
func authRequest(req *http.Request, token string) *http.Request {
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func TestCreateProject(t *testing.T) {
	srv := newTestServer(t)
	_, token := createTestUserWithSession(t, srv, "alice")

	body := jsonBody(createProjectRequest{
		Name:      "my-project",
		RemoteURL: "github.com/user/repo",
		EnvFile:   ".env.local",
	})
	req := authRequest(httptest.NewRequest("POST", "/projects", body), token)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", resp.Code, resp.Body.String())
	}

	var result projectResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if result.Name != "my-project" {
		t.Fatalf("expected name my-project, got %s", result.Name)
	}
	if result.RemoteURL != "github.com/user/repo" {
		t.Fatalf("expected remote_url github.com/user/repo, got %s", result.RemoteURL)
	}
	if result.EnvFile != ".env.local" {
		t.Fatalf("expected env_file .env.local, got %s", result.EnvFile)
	}
	if result.ID == "" {
		t.Fatal("expected non-empty id")
	}
	if result.OwnerID == "" {
		t.Fatal("expected non-empty owner_id")
	}
}

func TestListProjects(t *testing.T) {
	srv := newTestServer(t)
	userID1, token1 := createTestUserWithSession(t, srv, "alice")
	_, token2 := createTestUserWithSession(t, srv, "bob")

	// Alice creates a project.
	body := jsonBody(createProjectRequest{Name: "alice-project", EnvFile: ".env"})
	req := authRequest(httptest.NewRequest("POST", "/projects", body), token1)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d: %s", resp.Code, resp.Body.String())
	}

	// Alice lists projects — should see 1.
	req = authRequest(httptest.NewRequest("GET", "/projects", nil), token1)
	resp = httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("list projects: expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var aliceProjects []projectResponse
	if err := json.NewDecoder(resp.Body).Decode(&aliceProjects); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(aliceProjects) != 1 {
		t.Fatalf("expected 1 project for alice, got %d", len(aliceProjects))
	}
	if aliceProjects[0].Name != "alice-project" {
		t.Fatalf("expected name alice-project, got %s", aliceProjects[0].Name)
	}
	if aliceProjects[0].OwnerID != userID1 {
		t.Fatalf("expected owner_id %s, got %s", userID1, aliceProjects[0].OwnerID)
	}

	// Bob lists projects — should see 0.
	req = authRequest(httptest.NewRequest("GET", "/projects", nil), token2)
	resp = httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("list projects: expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var bobProjects []projectResponse
	if err := json.NewDecoder(resp.Body).Decode(&bobProjects); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(bobProjects) != 0 {
		t.Fatalf("expected 0 projects for bob, got %d", len(bobProjects))
	}
}

func TestDeleteProject(t *testing.T) {
	srv := newTestServer(t)
	_, token := createTestUserWithSession(t, srv, "alice")

	// Create a project.
	body := jsonBody(createProjectRequest{Name: "to-delete", EnvFile: ".env"})
	req := authRequest(httptest.NewRequest("POST", "/projects", body), token)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", resp.Code, resp.Body.String())
	}

	var created projectResponse
	json.NewDecoder(resp.Body).Decode(&created)

	// Delete it.
	req = authRequest(httptest.NewRequest("DELETE", "/projects/"+created.ID, nil), token)
	resp = httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d: %s", resp.Code, resp.Body.String())
	}

	// Listing should now be empty.
	req = authRequest(httptest.NewRequest("GET", "/projects", nil), token)
	resp = httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	var projects []projectResponse
	json.NewDecoder(resp.Body).Decode(&projects)

	if len(projects) != 0 {
		t.Fatalf("expected 0 projects after delete, got %d", len(projects))
	}
}

func TestDeleteProjectUnauthorized(t *testing.T) {
	srv := newTestServer(t)
	_, token1 := createTestUserWithSession(t, srv, "alice")
	_, token2 := createTestUserWithSession(t, srv, "bob")

	// Alice creates a project.
	body := jsonBody(createProjectRequest{Name: "alice-only", EnvFile: ".env"})
	req := authRequest(httptest.NewRequest("POST", "/projects", body), token1)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", resp.Code, resp.Body.String())
	}

	var created projectResponse
	json.NewDecoder(resp.Body).Decode(&created)

	// Bob tries to delete it — should get 404 (not 403).
	req = authRequest(httptest.NewRequest("DELETE", "/projects/"+created.ID, nil), token2)
	resp = httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unauthorized delete, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestProjectNotFoundReturns404(t *testing.T) {
	srv := newTestServer(t)
	_, token := createTestUserWithSession(t, srv, "alice")

	// Try to delete a non-existent project.
	req := authRequest(httptest.NewRequest("DELETE", "/projects/proj_nonexistent", nil), token)
	resp := httptest.NewRecorder()
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-existent project, got %d: %s", resp.Code, resp.Body.String())
	}
}
