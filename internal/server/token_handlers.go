package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/user/devbox/internal/crypto"
	"github.com/user/devbox/internal/database"
)

// --- Request / Response types ---

type createTokenRequest struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Scope     string `json:"scope"`
	TTL       string `json:"ttl"`
	ProjectID string `json:"project_id"`
}

type createTokenResp struct {
	Token     string `json:"token"`
	ID        string `json:"id"`
	ExpiresAt string `json:"expires_at"`
}

type tokenListItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Scope     string `json:"scope"`
	ExpiresAt string `json:"expires_at,omitempty"`
	LastUsed  string `json:"last_used,omitempty"`
	CreatedAt string `json:"created_at"`
}

type tokenListResp struct {
	Tokens []tokenListItem `json:"tokens"`
}

// --- TTL parsing ---

// maxTTL is the maximum allowed token lifetime.
var maxTTL = 365 * 24 * time.Hour

// defaultTTL is used when no TTL is specified.
var defaultTTL = 90 * 24 * time.Hour

// parseTTL parses a duration string like "90d", "1h", "365d" into a time.Duration.
// Supported suffixes: "d" (days), "h" (hours), "m" (minutes), "s" (seconds).
func parseTTL(s string) (time.Duration, error) {
	if s == "" {
		return defaultTTL, nil
	}

	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid ttl: %s", s)
	}

	suffix := s[len(s)-1]
	numStr := s[:len(s)-1]

	num, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("invalid ttl: %s", s)
	}

	if num <= 0 {
		return 0, fmt.Errorf("ttl must be positive: %s", s)
	}

	var d time.Duration
	switch suffix {
	case 'd':
		d = time.Duration(num) * 24 * time.Hour
	case 'h':
		d = time.Duration(num) * time.Hour
	case 'm':
		d = time.Duration(num) * time.Minute
	case 's':
		d = time.Duration(num) * time.Second
	default:
		return 0, fmt.Errorf("invalid ttl suffix: %c", suffix)
	}

	return d, nil
}

// --- Handlers ---

// handleCreateToken creates a new PAT or provision token.
// POST /tokens
func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	var req createTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	// Default type to "pat".
	tokenType := req.Type
	if tokenType == "" {
		tokenType = "pat"
	}

	if tokenType != "pat" && tokenType != "provision" {
		writeError(w, http.StatusBadRequest, "type must be pat or provision")
		return
	}

	// Provision tokens require a project_id.
	if tokenType == "provision" && req.ProjectID == "" {
		writeError(w, http.StatusBadRequest, "project_id is required for provision tokens")
		return
	}

	// Parse TTL.
	ttlDuration, err := parseTTL(req.TTL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if ttlDuration > maxTTL {
		writeError(w, http.StatusBadRequest, "ttl exceeds maximum of 365d")
		return
	}

	// Build scope JSON.
	type scopeJSON struct {
		Permissions []string `json:"permissions"`
		ProjectID   string   `json:"project_id,omitempty"`
	}
	scopeObj := scopeJSON{Permissions: []string{req.Scope}}
	if tokenType == "provision" && req.ProjectID != "" {
		scopeObj.ProjectID = req.ProjectID
	}
	scopeBytes, err := json.Marshal(scopeObj)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build scope")
		return
	}
	scope := string(scopeBytes)

	// Generate the raw token.
	var prefix string
	if tokenType == "pat" {
		prefix = "dbx_pat_"
	} else {
		prefix = "dbx_prov_"
	}

	rawToken, err := crypto.GenerateToken(prefix)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	tokenHash := crypto.HashToken(rawToken)
	singleUse := tokenType == "provision"

	expiresAt := time.Now().Add(ttlDuration).UTC().Format(time.RFC3339Nano)

	userID := GetUserID(r.Context())
	tok, err := database.CreateToken(s.DB, userID, req.Name, tokenHash, tokenType, scope, expiresAt, singleUse)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create token")
		return
	}

	// Determine the TTL string for audit metadata.
	ttlStr := req.TTL
	if ttlStr == "" {
		ttlStr = "90d"
	}

	// Audit log.
	auditMeta, _ := json.Marshal(map[string]string{
		"token_name": req.Name,
		"scope":      req.Scope,
		"ttl":        ttlStr,
	})
	database.LogEvent(s.DB, database.AuditEntry{
		UserID:    userID,
		Action:    "token.create",
		Metadata:  string(auditMeta),
		IPAddress: r.RemoteAddr,
		UserAgent: r.UserAgent(),
	})

	writeJSON(w, http.StatusCreated, createTokenResp{
		Token:     rawToken,
		ID:        tok.ID,
		ExpiresAt: tok.ExpiresAt,
	})
}

// handleListTokens returns all tokens for the authenticated user.
// GET /tokens
func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r.Context())

	tokens, err := database.ListTokensForUser(s.DB, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list tokens")
		return
	}

	items := make([]tokenListItem, len(tokens))
	for i, tok := range tokens {
		items[i] = tokenListItem{
			ID:        tok.ID,
			Name:      tok.Name,
			Type:      tok.Type,
			Scope:     tok.Scope,
			ExpiresAt: tok.ExpiresAt,
			LastUsed:  tok.LastUsed,
			CreatedAt: tok.CreatedAt,
		}
	}

	writeJSON(w, http.StatusOK, tokenListResp{Tokens: items})
}

// handleRevokeToken deletes a token belonging to the authenticated user.
// DELETE /tokens/{tokenID}
func (s *Server) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	tokenID := chi.URLParam(r, "tokenID")
	userID := GetUserID(r.Context())

	// List the user's tokens and check that the target token belongs to them.
	tokens, err := database.ListTokensForUser(s.DB, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to verify token ownership")
		return
	}

	found := false
	for _, tok := range tokens {
		if tok.ID == tokenID {
			found = true
			break
		}
	}

	if !found {
		writeError(w, http.StatusNotFound, "token not found")
		return
	}

	if err := database.RevokeToken(s.DB, tokenID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke token")
		return
	}

	// Audit log.
	database.LogEvent(s.DB, database.AuditEntry{
		UserID:    userID,
		Action:    "token.revoke",
		IPAddress: r.RemoteAddr,
		UserAgent: r.UserAgent(),
	})

	w.WriteHeader(http.StatusNoContent)
}
