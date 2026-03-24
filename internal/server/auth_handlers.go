package server

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/user/devbox/internal/crypto"
	"github.com/user/devbox/internal/database"
)

// --- Request / Response types ---

type registerRequest struct {
	Name        string `json:"name"`
	PublicKey   string `json:"public_key"`
	Fingerprint string `json:"fingerprint"`
}

type registerResponse struct {
	UserID  string `json:"user_id"`
	Name    string `json:"name"`
	IsAdmin bool   `json:"is_admin"`
}

type challengeResponse struct {
	Nonce     string `json:"nonce"`
	ExpiresAt string `json:"expires_at"`
}

type verifyRequest struct {
	Fingerprint string `json:"fingerprint"`
	Signature   string `json:"signature"`
	Nonce       string `json:"nonce"`
}

type verifyResponse struct {
	SessionToken string `json:"session_token"`
	UserID       string `json:"user_id"`
	ExpiresIn    int    `json:"expires_in"`
}

type tokenAuthRequest struct {
	Token string `json:"token"`
}

type tokenAuthResponse struct {
	SessionToken string `json:"session_token"`
	UserID       string `json:"user_id"`
	ExpiresIn    int    `json:"expires_in"`
}

// --- Helpers ---

// writeJSON encodes v as JSON and writes it to w with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// --- Handlers ---

// handleRegister creates a new user with an SSH key.
// POST /auth/register
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || req.PublicKey == "" || req.Fingerprint == "" {
		writeError(w, http.StatusBadRequest, "name, public_key, and fingerprint are required")
		return
	}

	// Create the user.
	user, err := database.CreateUser(s.DB, req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	// Add the SSH key.
	err = database.AddSSHKey(s.DB, user.ID, req.Fingerprint, req.PublicKey, req.Name)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			writeError(w, http.StatusConflict, "fingerprint already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to add ssh key")
		return
	}

	// Audit log.
	database.LogEvent(s.DB, database.AuditEntry{
		UserID:    user.ID,
		Action:    "user.create",
		IPAddress: r.RemoteAddr,
		UserAgent: r.UserAgent(),
	})

	writeJSON(w, http.StatusCreated, registerResponse{
		UserID:  user.ID,
		Name:    user.Name,
		IsAdmin: user.IsAdmin,
	})
}

// handleChallenge creates a nonce for SSH signature verification.
// POST /auth/challenge
func (s *Server) handleChallenge(w http.ResponseWriter, r *http.Request) {
	nonce := s.Nonces.Create()

	expiresAt := time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339)

	writeJSON(w, http.StatusOK, challengeResponse{
		Nonce:     nonce,
		ExpiresAt: expiresAt,
	})
}

// handleVerify verifies an SSH signature against a challenge nonce and
// creates a session.
// POST /auth/verify
func (s *Server) handleVerify(w http.ResponseWriter, r *http.Request) {
	var req verifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Fingerprint == "" || req.Signature == "" || req.Nonce == "" {
		writeError(w, http.StatusBadRequest, "fingerprint, signature, and nonce are required")
		return
	}

	// Look up the user by fingerprint.
	user, err := database.FindUserByFingerprint(s.DB, req.Fingerprint)
	if err != nil {
		database.LogEvent(s.DB, database.AuditEntry{
			Action:    "auth.failure",
			Metadata:  `{"reason":"unknown_fingerprint"}`,
			IPAddress: r.RemoteAddr,
			UserAgent: r.UserAgent(),
		})
		writeError(w, http.StatusUnauthorized, "authentication failed")
		return
	}

	// Consume the nonce (single use).
	if !s.Nonces.Consume(req.Nonce) {
		database.LogEvent(s.DB, database.AuditEntry{
			UserID:    user.ID,
			Action:    "auth.failure",
			Metadata:  `{"reason":"invalid_nonce"}`,
			IPAddress: r.RemoteAddr,
			UserAgent: r.UserAgent(),
		})
		writeError(w, http.StatusUnauthorized, "authentication failed")
		return
	}

	// Find the SSH public key for this user to verify the signature.
	pubKeyStr, err := database.FindPublicKeyByFingerprint(s.DB, req.Fingerprint)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication failed")
		return
	}

	// Parse the authorized key format.
	pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pubKeyStr))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid stored public key")
		return
	}

	// Decode the nonce from hex.
	nonceBytes, err := hex.DecodeString(req.Nonce)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid nonce format")
		return
	}

	// Decode signature from base64.
	sigBytes, err := base64.StdEncoding.DecodeString(req.Signature)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid signature format")
		return
	}

	// Verify the SSH signature.
	if err := crypto.VerifySSH(pubKey, nonceBytes, sigBytes); err != nil {
		database.LogEvent(s.DB, database.AuditEntry{
			UserID:    user.ID,
			Action:    "auth.failure",
			Metadata:  `{"reason":"invalid_signature"}`,
			IPAddress: r.RemoteAddr,
			UserAgent: r.UserAgent(),
		})
		writeError(w, http.StatusUnauthorized, "authentication failed")
		return
	}

	// Create session.
	rawToken, err := crypto.GenerateToken("dbx_ses_")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	tokenHash := crypto.HashToken(rawToken)
	_, err = database.CreateSession(s.DB, user.ID, tokenHash, r.RemoteAddr, r.UserAgent())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	// Audit log.
	database.LogEvent(s.DB, database.AuditEntry{
		UserID:    user.ID,
		Action:    "auth.success",
		Metadata:  `{"method":"ssh"}`,
		IPAddress: r.RemoteAddr,
		UserAgent: r.UserAgent(),
	})

	writeJSON(w, http.StatusOK, verifyResponse{
		SessionToken: rawToken,
		UserID:       user.ID,
		ExpiresIn:    900,
	})
}

// handleTokenAuth authenticates using a PAT or provision token and creates a session.
// POST /auth/token
func (s *Server) handleTokenAuth(w http.ResponseWriter, r *http.Request) {
	var req tokenAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}

	tokenHash := crypto.HashToken(req.Token)
	var userID string

	if strings.HasPrefix(req.Token, "dbx_pat_") {
		// Personal access token.
		tok, err := database.FindTokenByHash(s.DB, tokenHash)
		if err != nil {
			database.LogEvent(s.DB, database.AuditEntry{
				Action:    "auth.failure",
				Metadata:  `{"reason":"invalid_pat"}`,
				IPAddress: r.RemoteAddr,
				UserAgent: r.UserAgent(),
			})
			writeError(w, http.StatusUnauthorized, "authentication failed")
			return
		}
		userID = tok.UserID

		// Update last used.
		database.UpdateLastUsed(s.DB, tok.ID)

	} else if strings.HasPrefix(req.Token, "dbx_prov_") {
		// Provision token (single use).
		tok, err := database.ConsumeProvisionToken(s.DB, tokenHash)
		if err != nil {
			database.LogEvent(s.DB, database.AuditEntry{
				Action:    "auth.failure",
				Metadata:  `{"reason":"invalid_provision_token"}`,
				IPAddress: r.RemoteAddr,
				UserAgent: r.UserAgent(),
			})
			writeError(w, http.StatusUnauthorized, "authentication failed")
			return
		}
		userID = tok.UserID
	} else {
		writeError(w, http.StatusBadRequest, "unknown token type")
		return
	}

	// Create session.
	rawToken, err := crypto.GenerateToken("dbx_ses_")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	sessionHash := crypto.HashToken(rawToken)
	_, err = database.CreateSession(s.DB, userID, sessionHash, r.RemoteAddr, r.UserAgent())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	// Audit log.
	database.LogEvent(s.DB, database.AuditEntry{
		UserID:    userID,
		Action:    "auth.success",
		Metadata:  `{"method":"token"}`,
		IPAddress: r.RemoteAddr,
		UserAgent: r.UserAgent(),
	})

	writeJSON(w, http.StatusOK, tokenAuthResponse{
		SessionToken: rawToken,
		UserID:       userID,
		ExpiresIn:    900,
	})
}

// handleLogout deletes the current session.
// POST /auth/logout (requires auth middleware)
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	sessionID := GetSessionID(r.Context())
	if sessionID == "" {
		writeError(w, http.StatusUnauthorized, "no session")
		return
	}

	if err := database.DeleteSession(s.DB, sessionID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete session")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
