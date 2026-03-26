package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/user/devbox/internal/database"
)

// validateEnvFile checks that the env file name is safe and does not contain
// path traversal sequences or path separators.
func validateEnvFile(name string) error {
	if name == "" {
		return nil // will use default
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
		return fmt.Errorf("invalid env_file: must not contain path separators or '..'")
	}
	if !strings.HasPrefix(name, ".env") {
		return fmt.Errorf("invalid env_file: must start with '.env'")
	}
	return nil
}

// maxEnvBlobSize is the maximum size of a decoded env var blob (64KB).
const maxEnvBlobSize = 64 * 1024

// --- Request / Response types ---

type pushEnvRequest struct {
	Data            string `json:"data"`             // base64-encoded plaintext
	ExpectedVersion int    `json:"expected_version"`
	EnvFile         string `json:"env_file"`
}

type pushEnvResponse struct {
	Version int `json:"version"`
}

type pullEnvResponse struct {
	Data    string `json:"data"`     // base64-encoded plaintext
	Version int    `json:"version"`
	EnvFile string `json:"env_file"`
}

type envVersionResponse struct {
	Version int `json:"version"`
}

// --- Handlers ---

// handlePullEnv decrypts and returns the current env var blob for a project.
// GET /projects/{projectID}/env
func (s *Server) handlePullEnv(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	userID := GetUserID(r.Context())

	// Check membership (read access).
	if err := database.CheckMembership(s.DB, projectID, userID, "reader"); err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	// Pull the encrypted blob.
	envData, err := database.PullEnvVars(s.DB, projectID, "default")
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			writeError(w, http.StatusNotFound, "no env vars found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to pull env vars")
		return
	}

	// Decrypt the blob.
	plaintext, err := s.Encryptor.Decrypt(envData.Blob, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decrypt env vars")
		return
	}

	// Get the project's env_file setting.
	proj, err := database.FindProjectByID(s.DB, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read project")
		return
	}

	// Audit log.
	pullMeta, _ := json.Marshal(map[string]int{"version": envData.Version})
	database.LogEvent(s.DB, database.AuditEntry{
		UserID:    userID,
		ProjectID: projectID,
		Action:    "env.pull",
		Metadata:  string(pullMeta),
		IPAddress: r.RemoteAddr,
		UserAgent: r.UserAgent(),
	})

	writeJSON(w, http.StatusOK, pullEnvResponse{
		Data:    base64.StdEncoding.EncodeToString(plaintext),
		Version: envData.Version,
		EnvFile: proj.EnvFile,
	})
}

// handleGetEnvVersion returns the current version of env vars without the blob.
// GET /projects/{projectID}/env/version
func (s *Server) handleGetEnvVersion(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	userID := GetUserID(r.Context())

	// Check membership (read access).
	if err := database.CheckMembership(s.DB, projectID, userID, "reader"); err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	version, err := database.GetEnvVersion(s.DB, projectID, "default")
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			writeJSON(w, http.StatusOK, envVersionResponse{Version: 0})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get env version")
		return
	}

	writeJSON(w, http.StatusOK, envVersionResponse{Version: version})
}

// handlePushEnv encrypts and stores an env var blob with optimistic locking.
// PUT /projects/{projectID}/env
func (s *Server) handlePushEnv(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	userID := GetUserID(r.Context())

	// Check membership (write access).
	if err := database.CheckMembership(s.DB, projectID, userID, "writer"); err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	var req pushEnvRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Data == "" {
		writeError(w, http.StatusBadRequest, "data is required")
		return
	}

	// Validate env_file name.
	if err := validateEnvFile(req.EnvFile); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Decode the base64 data.
	plaintext, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid base64 data")
		return
	}

	// Check size limit.
	if len(plaintext) > maxEnvBlobSize {
		writeError(w, http.StatusRequestEntityTooLarge, "env data exceeds 64KB limit")
		return
	}

	// Encrypt the blob.
	ciphertext, err := s.Encryptor.Encrypt(plaintext, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt env vars")
		return
	}

	// Push with optimistic locking.
	newVersion, err := database.PushEnvVars(s.DB, projectID, "default", ciphertext, req.ExpectedVersion, userID)
	if err != nil {
		if errors.Is(err, database.ErrVersionConflict) {
			// Get the current version to report back.
			currentVersion, verr := database.GetEnvVersion(s.DB, projectID, "default")
			if verr != nil {
				writeError(w, http.StatusConflict, "version conflict")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]any{
				"error":           "version conflict",
				"current_version": currentVersion,
			})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to push env vars")
		return
	}

	// Update project env_file if provided.
	if req.EnvFile != "" {
		if err := database.UpdateProjectEnvFile(s.DB, projectID, req.EnvFile); err != nil {
			// Non-fatal: the push succeeded, but updating env_file failed.
			// Log but don't fail the response.
		}
	}

	// Audit log.
	pushMeta, _ := json.Marshal(map[string]int{
		"old_version": req.ExpectedVersion,
		"new_version": newVersion,
	})
	database.LogEvent(s.DB, database.AuditEntry{
		UserID:    userID,
		ProjectID: projectID,
		Action:    "env.push",
		Metadata:  string(pushMeta),
		IPAddress: r.RemoteAddr,
		UserAgent: r.UserAgent(),
	})

	writeJSON(w, http.StatusOK, pushEnvResponse{Version: newVersion})
}
