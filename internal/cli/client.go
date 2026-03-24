package cli

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// APIError represents a non-2xx response from the server.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("api error %d: %s", e.StatusCode, e.Message)
}

// --- Response types matching the server API ---

// RegisterResponse is the response from POST /auth/register.
type RegisterResponse struct {
	UserID  string `json:"user_id"`
	Name    string `json:"name"`
	IsAdmin bool   `json:"is_admin"`
}

// ChallengeResponse is the response from POST /auth/challenge.
type ChallengeResponse struct {
	Nonce     string `json:"nonce"`
	ExpiresAt string `json:"expires_at"`
}

// VerifyResponse is the response from POST /auth/verify and POST /auth/token.
type VerifyResponse struct {
	SessionToken string `json:"session_token"`
	UserID       string `json:"user_id"`
	ExpiresIn    int    `json:"expires_in"`
}

// ProjectResponse is the response from project endpoints.
type ProjectResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	RemoteURL string `json:"remote_url,omitempty"`
	EnvFile   string `json:"env_file"`
	OwnerID   string `json:"owner_id"`
	CreatedAt string `json:"created_at"`
}

// EnvResponse is the response from GET /projects/{id}/env.
type EnvResponse struct {
	Data    string `json:"data"`
	Version int    `json:"version"`
	EnvFile string `json:"env_file"`
}

// PushEnvResponse is the response from PUT /projects/{id}/env.
type PushEnvResponse struct {
	Version int `json:"version"`
}

// EnvVersionResponse is the response from GET /projects/{id}/env/version.
type EnvVersionResponse struct {
	Version int `json:"version"`
}

// CreateTokenResponse is the response from POST /tokens.
type CreateTokenResponse struct {
	Token     string `json:"token"`
	ID        string `json:"id"`
	ExpiresAt string `json:"expires_at"`
}

// TokenResponse represents a single token in a list.
type TokenResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Scope     string `json:"scope"`
	ExpiresAt string `json:"expires_at,omitempty"`
	LastUsed  string `json:"last_used,omitempty"`
	CreatedAt string `json:"created_at"`
}

// TokenListResponse wraps the list of tokens.
type TokenListResponse struct {
	Tokens []TokenResponse `json:"tokens"`
}

// --- Request body types (internal) ---

type registerRequestBody struct {
	Name        string `json:"name"`
	PublicKey   string `json:"public_key"`
	Fingerprint string `json:"fingerprint"`
}

type verifyRequestBody struct {
	Fingerprint string `json:"fingerprint"`
	Signature   string `json:"signature"`
	Nonce       string `json:"nonce"`
}

type tokenAuthRequestBody struct {
	Token string `json:"token"`
}

type createProjectRequestBody struct {
	Name      string `json:"name"`
	RemoteURL string `json:"remote_url"`
	EnvFile   string `json:"env_file"`
}

type pushEnvRequestBody struct {
	Data            string `json:"data"`
	ExpectedVersion int    `json:"expected_version"`
	EnvFile         string `json:"env_file"`
}

type createTokenRequestBody struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Scope     string `json:"scope"`
	TTL       string `json:"ttl"`
	ProjectID string `json:"project_id,omitempty"`
}

// --- Client ---

// Client communicates with the devbox server API.
type Client struct {
	BaseURL    string
	AuthToken  string
	HTTPClient *http.Client
}

// NewClient creates a new API client. If tlsCAPath is non-empty, its contents
// are loaded and added to the TLS root CA pool.
func NewClient(baseURL, authToken string, tlsCAPath string) (*Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()

	if tlsCAPath != "" {
		caPEM, err := os.ReadFile(tlsCAPath)
		if err != nil {
			return nil, fmt.Errorf("reading CA file: %w", err)
		}

		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("failed to parse CA certificate from %s", tlsCAPath)
		}

		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{}
		}
		transport.TLSClientConfig.RootCAs = pool
	}

	return &Client{
		BaseURL:   baseURL,
		AuthToken: authToken,
		HTTPClient: &http.Client{
			Transport: transport,
		},
	}, nil
}

// do executes an HTTP request, setting auth headers and checking the response.
func (c *Client) do(method, path string, body any, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshaling request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.BaseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if c.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	// For 204 No Content, there's no body to parse.
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}

	// Read the full body for error reporting.
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	// Check for non-2xx status codes.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Try to extract error message from JSON response.
		var errResp struct {
			Error string `json:"error"`
		}
		msg := string(respBody)
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			msg = errResp.Error
		}
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    msg,
		}
	}

	// Parse the response body if a result target is provided.
	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("parsing response: %w", err)
		}
	}

	return nil
}

// --- Auth methods ---

// Register creates a new user account.
func (c *Client) Register(name, publicKey, fingerprint string) (*RegisterResponse, error) {
	body := registerRequestBody{
		Name:        name,
		PublicKey:   publicKey,
		Fingerprint: fingerprint,
	}
	var resp RegisterResponse
	if err := c.do("POST", "/auth/register", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Challenge requests a nonce for SSH signature verification.
func (c *Client) Challenge() (*ChallengeResponse, error) {
	var resp ChallengeResponse
	if err := c.do("POST", "/auth/challenge", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Verify verifies an SSH signature and creates a session.
func (c *Client) Verify(fingerprint string, signature []byte, nonce string) (*VerifyResponse, error) {
	body := verifyRequestBody{
		Fingerprint: fingerprint,
		Signature:   base64.StdEncoding.EncodeToString(signature),
		Nonce:       nonce,
	}
	var resp VerifyResponse
	if err := c.do("POST", "/auth/verify", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// TokenAuth authenticates using a PAT or provision token.
func (c *Client) TokenAuth(token string) (*VerifyResponse, error) {
	body := tokenAuthRequestBody{Token: token}
	var resp VerifyResponse
	if err := c.do("POST", "/auth/token", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Logout deletes the current session.
func (c *Client) Logout() error {
	return c.do("POST", "/auth/logout", nil, nil)
}

// --- Project methods ---

// CreateProject creates a new project.
func (c *Client) CreateProject(name, remoteURL, envFile string) (*ProjectResponse, error) {
	body := createProjectRequestBody{
		Name:      name,
		RemoteURL: remoteURL,
		EnvFile:   envFile,
	}
	var resp ProjectResponse
	if err := c.do("POST", "/projects", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListProjects returns all projects accessible to the authenticated user.
func (c *Client) ListProjects() ([]ProjectResponse, error) {
	var resp []ProjectResponse
	if err := c.do("GET", "/projects", nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// DeleteProject deletes a project.
func (c *Client) DeleteProject(projectID string) error {
	return c.do("DELETE", "/projects/"+projectID, nil, nil)
}

// --- Env methods ---

// PullEnv retrieves the encrypted env var blob for a project.
func (c *Client) PullEnv(projectID string) (*EnvResponse, error) {
	var resp EnvResponse
	if err := c.do("GET", "/projects/"+projectID+"/env", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// PushEnv uploads an env var blob with optimistic locking.
func (c *Client) PushEnv(projectID string, data string, expectedVersion int, envFile string) (*PushEnvResponse, error) {
	body := pushEnvRequestBody{
		Data:            data,
		ExpectedVersion: expectedVersion,
		EnvFile:         envFile,
	}
	var resp PushEnvResponse
	if err := c.do("PUT", "/projects/"+projectID+"/env", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetEnvVersion returns the current env var version without the blob.
func (c *Client) GetEnvVersion(projectID string) (int, error) {
	var resp EnvVersionResponse
	if err := c.do("GET", "/projects/"+projectID+"/env/version", nil, &resp); err != nil {
		return 0, err
	}
	return resp.Version, nil
}

// --- Token methods ---

// CreateToken creates a new PAT or provision token.
func (c *Client) CreateToken(name, tokenType, scope, ttl, projectID string) (*CreateTokenResponse, error) {
	body := createTokenRequestBody{
		Name:      name,
		Type:      tokenType,
		Scope:     scope,
		TTL:       ttl,
		ProjectID: projectID,
	}
	var resp CreateTokenResponse
	if err := c.do("POST", "/tokens", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListTokens returns all tokens for the authenticated user.
func (c *Client) ListTokens() ([]TokenResponse, error) {
	var resp TokenListResponse
	if err := c.do("GET", "/tokens", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Tokens, nil
}

// RevokeToken deletes a token.
func (c *Client) RevokeToken(tokenID string) error {
	return c.do("DELETE", "/tokens/"+tokenID, nil, nil)
}
