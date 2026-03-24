package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/user/devbox/internal/database"
)

// --- Request / Response types ---

type createProjectRequest struct {
	Name      string `json:"name"`
	RemoteURL string `json:"remote_url"`
	EnvFile   string `json:"env_file"`
}

type projectResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	RemoteURL string `json:"remote_url,omitempty"`
	EnvFile   string `json:"env_file"`
	OwnerID   string `json:"owner_id"`
	CreatedAt string `json:"created_at"`
}

// --- Handlers ---

// handleCreateProject creates a new project and adds the authenticated user as
// an admin member.
// POST /projects
func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	userID := GetUserID(r.Context())

	proj, err := database.CreateProject(s.DB, req.Name, req.RemoteURL, req.EnvFile, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create project")
		return
	}

	// Audit log.
	database.LogEvent(s.DB, database.AuditEntry{
		UserID:    userID,
		ProjectID: proj.ID,
		Action:    "project.create",
		IPAddress: r.RemoteAddr,
		UserAgent: r.UserAgent(),
	})

	writeJSON(w, http.StatusCreated, projectResponse{
		ID:        proj.ID,
		Name:      proj.Name,
		RemoteURL: proj.RemoteURL,
		EnvFile:   proj.EnvFile,
		OwnerID:   proj.OwnerID,
		CreatedAt: proj.CreatedAt,
	})
}

// handleListProjects returns all projects the authenticated user has access to.
// GET /projects
func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r.Context())

	projects, err := database.ListProjectsForUser(s.DB, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list projects")
		return
	}

	resp := make([]projectResponse, len(projects))
	for i, p := range projects {
		resp[i] = projectResponse{
			ID:        p.ID,
			Name:      p.Name,
			RemoteURL: p.RemoteURL,
			EnvFile:   p.EnvFile,
			OwnerID:   p.OwnerID,
			CreatedAt: p.CreatedAt,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleDeleteProject deletes a project. Requires admin role on the project.
// Returns 404 (not 403) for unauthorized users to avoid leaking project existence.
// DELETE /projects/{projectID}
func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	userID := GetUserID(r.Context())

	// Check that the user is an admin of this project.
	if err := database.CheckMembership(s.DB, projectID, userID, "admin"); err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	if err := database.DeleteProject(s.DB, projectID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete project")
		return
	}

	// Audit log.
	database.LogEvent(s.DB, database.AuditEntry{
		UserID:    userID,
		ProjectID: projectID,
		Action:    "project.delete",
		IPAddress: r.RemoteAddr,
		UserAgent: r.UserAgent(),
	})

	w.WriteHeader(http.StatusNoContent)
}
