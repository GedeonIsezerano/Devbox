# Devbox Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `dbx` CLI and `dbx-server` — a self-hosted tool for pulling env files into any environment with a single authenticated command.

**Architecture:** Go monorepo with two binaries (`cmd/dbx`, `cmd/dbx-server`) sharing internal packages. Server uses SQLite + age encryption. CLI auto-detects project from git remote and env file from project type. Auth via SSH challenge-response or PAT tokens.

**Tech Stack:** Go 1.22+, SQLite (modernc.org/sqlite — pure Go, no CGO), filippo.io/age, cobra (CLI), chi (HTTP router)

**Spec:** `docs/superpowers/specs/2026-03-23-devbox-design.md`

---

## File Structure

```
cmd/
  dbx/
    main.go                     # CLI entrypoint, cobra root command
  dbx-server/
    main.go                     # Server entrypoint, cobra root command

internal/
  database/
    db.go                       # SQLite connection, pragmas, migration runner
    db_test.go
    migrations/
      001_initial.sql           # Full v1 schema
    users.go                    # User + SSH key CRUD
    users_test.go
    projects.go                 # Project + member CRUD
    projects_test.go
    envvars.go                  # Env var blob CRUD + history + version
    envvars_test.go
    tokens.go                   # Token CRUD + atomic consume
    tokens_test.go
    sessions.go                 # Session CRUD + cleanup
    sessions_test.go
    audit.go                    # Audit log append + query
    audit_test.go

  crypto/
    encrypt.go                  # Encryptor interface + age implementation
    encrypt_test.go
    hkdf.go                     # Per-project key derivation
    hkdf_test.go
    tokens.go                   # Token generation (crypto/rand) + SHA-256 hashing
    tokens_test.go
    nonce.go                    # In-memory nonce store with TTL
    nonce_test.go
    ssh.go                      # SSH signature creation + verification
    ssh_test.go

  server/
    server.go                   # HTTP server setup, graceful shutdown
    server_test.go
    middleware.go               # Auth middleware, rate limiter, request size limit
    middleware_test.go
    auth_handlers.go            # /auth/* handlers
    auth_handlers_test.go
    project_handlers.go         # /projects/* handlers
    project_handlers_test.go
    env_handlers.go             # /projects/:id/env handlers
    env_handlers_test.go
    token_handlers.go           # /tokens/* handlers
    token_handlers_test.go
    health_handler.go           # /health handler

  cli/
    config.go                   # Config file read/write (~/.config/dbx/config.toml)
    config_test.go
    client.go                   # HTTP client for server API
    client_test.go
    git.go                      # Git remote parsing + normalization
    git_test.go
    detect.go                   # Env file detection from project markers
    detect_test.go
    output.go                   # Stderr/stdout, color, JSON, quiet/verbose
    output_test.go
    auth_cmd.go                 # auth login/status/logout commands
    auth_cmd_test.go
    project_cmd.go              # init/project list/project delete commands
    project_cmd_test.go
    env_cmd.go                  # push/pull/diff commands
    env_cmd_test.go
    token_cmd.go                # token create/list/revoke commands
    token_cmd_test.go
    whoami_cmd.go               # whoami command
    completion_cmd.go           # shell completions

go.mod
go.sum
Dockerfile
.goreleaser.yaml
install.sh
Makefile
```

---

## Task 1: Project Scaffolding

**Files:**
- Create: `go.mod`, `Makefile`, `cmd/dbx/main.go`, `cmd/dbx-server/main.go`

- [ ] **Step 1: Initialize Go module**

```bash
cd /Users/home/Code/Devbox
go mod init github.com/user/devbox
```

- [ ] **Step 2: Create Makefile**

Create `Makefile`:

```makefile
.PHONY: build test lint clean

build:
	go build -o bin/dbx ./cmd/dbx
	go build -o bin/dbx-server ./cmd/dbx-server

test:
	go test ./... -v -race

lint:
	go vet ./...

clean:
	rm -rf bin/
```

- [ ] **Step 3: Create CLI entrypoint**

Create `cmd/dbx/main.go`:

```go
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "dbx v0.0.1-dev")
	os.Exit(0)
}
```

- [ ] **Step 4: Create server entrypoint**

Create `cmd/dbx-server/main.go`:

```go
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "dbx-server v0.0.1-dev")
	os.Exit(0)
}
```

- [ ] **Step 5: Verify both binaries build**

```bash
make build
./bin/dbx
./bin/dbx-server
```

Expected: Both print version and exit 0.

- [ ] **Step 6: Add .gitignore**

Add to `.gitignore`:

```
bin/
*.db
*.db-wal
*.db-shm
.env
.env.local
.dbx/
```

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "feat: scaffold Go project with CLI and server entrypoints"
```

---

## Task 2: Database Layer — Connection & Migrations

**Files:**
- Create: `internal/database/db.go`, `internal/database/db_test.go`, `internal/database/migrations/001_initial.sql`

- [ ] **Step 1: Install SQLite dependency**

```bash
go get modernc.org/sqlite
```

Using pure-Go SQLite (no CGO required) for easy cross-compilation.

- [ ] **Step 2: Write test for database initialization**

Create `internal/database/db_test.go`:

```go
package database

import (
	"testing"
)

func TestOpenCreatesDBWithPragmas(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Verify WAL mode
	var journalMode string
	err = db.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if err != nil {
		t.Fatal(err)
	}
	// In-memory DBs use "memory" journal mode, so we just verify no error
	if journalMode == "" {
		t.Fatal("expected journal_mode to be set")
	}

	// Verify foreign keys are on
	var fk int
	err = db.QueryRow("PRAGMA foreign_keys").Scan(&fk)
	if err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Fatalf("expected foreign_keys=1, got %d", fk)
	}
}

func TestMigrationsApply(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Verify schema version
	var version int
	err = db.QueryRow("PRAGMA user_version").Scan(&version)
	if err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("expected user_version=1, got %d", version)
	}

	// Verify tables exist
	tables := []string{"users", "ssh_keys", "projects", "project_members",
		"env_vars", "env_var_history", "tokens", "sessions", "audit_log"}
	for _, table := range tables {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Fatalf("table %s not found: %v", table, err)
		}
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
go test ./internal/database/ -v -run TestOpen
```

Expected: FAIL — `Open` not defined.

- [ ] **Step 4: Create migration SQL**

Create `internal/database/migrations/001_initial.sql` with the full schema from the spec (all 9 CREATE TABLE statements). Copy directly from spec Section 5.

- [ ] **Step 5: Implement database connection and migration runner**

Create `internal/database/db.go`:

```go
package database

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(1)

	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA secure_delete=ON",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("exec %s: %w", p, err)
		}
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return db, nil
}

func migrate(db *sql.DB) error {
	var currentVersion int
	if err := db.QueryRow("PRAGMA user_version").Scan(&currentVersion); err != nil {
		return err
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	latestVersion := len(entries)

	if currentVersion > latestVersion {
		return fmt.Errorf("database schema version %d is newer than binary (max %d) — refusing to start", currentVersion, latestVersion)
	}

	for i, entry := range entries {
		version := i + 1
		if version <= currentVersion {
			continue
		}

		content, err := migrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}

		tx, err := db.Begin()
		if err != nil {
			return err
		}

		statements := strings.Split(string(content), ";")
		for _, stmt := range statements {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			if _, err := tx.Exec(stmt); err != nil {
				tx.Rollback()
				return fmt.Errorf("migration %s: %w", entry.Name(), err)
			}
		}

		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
			tx.Rollback()
			return err
		}

		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

```bash
go test ./internal/database/ -v
```

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/database/ go.mod go.sum
git commit -m "feat: database layer with SQLite connection, pragmas, and migration runner"
```

---

## Task 3: Database Layer — User & SSH Key Queries

**Files:**
- Create: `internal/database/users.go`, `internal/database/users_test.go`

- [ ] **Step 1: Write tests for user CRUD**

Create `internal/database/users_test.go` with tests for:
- `TestCreateUser` — creates user, returns usr_ prefixed ID
- `TestCreateUserFirstIsAdmin` — first user gets `is_admin=1`
- `TestAddSSHKey` — adds SSH key to user
- `TestFindUserByFingerprint` — looks up user by SSH key fingerprint
- `TestAddDuplicateFingerprint` — returns error on duplicate fingerprint

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/database/ -v -run TestCreate
```

- [ ] **Step 3: Implement user queries**

Create `internal/database/users.go` with:
- `CreateUser(db, name string) (User, error)` — generates `usr_` + uuid, checks if first user for `is_admin`
- `AddSSHKey(db, userID, fingerprint, publicKey, name string) error`
- `FindUserByFingerprint(db, fingerprint string) (User, error)`

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/database/ -v -run TestCreate
```

- [ ] **Step 5: Commit**

```bash
git add internal/database/users.go internal/database/users_test.go
git commit -m "feat: user and SSH key database queries"
```

---

## Task 4: Database Layer — Project Queries

**Files:**
- Create: `internal/database/projects.go`, `internal/database/projects_test.go`

- [ ] **Step 1: Write tests for project CRUD**

Tests for:
- `TestCreateProject` — creates project with `proj_` prefix, auto-adds creator as admin member
- `TestCreateProjectWithRemoteURL` — stores normalized remote
- `TestFindProjectByRemoteURL` — lookup by remote
- `TestFindProjectByName` — lookup by name (case-sensitive exact match)
- `TestListProjectsForUser` — returns only projects user is a member of
- `TestDeleteProject` — cascades to members, env_vars, history
- `TestCheckMembership` — verifies user has required role

- [ ] **Step 2: Run tests, verify fail**

- [ ] **Step 3: Implement project queries**

Create `internal/database/projects.go` with:
- `CreateProject(db, name, remoteURL, envFile, ownerID string) (Project, error)`
- `FindProjectByRemoteURL(db, remoteURL string) (Project, error)`
- `FindProjectByName(db, name string) (Project, error)`
- `ListProjectsForUser(db, userID string) ([]Project, error)`
- `DeleteProject(db, projectID string) error`
- `CheckMembership(db, projectID, userID, requiredRole string) error`
- `UpdateProjectEnvFile(db, projectID, envFile string) error`

- [ ] **Step 4: Run tests, verify pass**

- [ ] **Step 5: Commit**

```bash
git add internal/database/projects.go internal/database/projects_test.go
git commit -m "feat: project database queries with membership checks"
```

---

## Task 5: Database Layer — Env Var Queries

**Files:**
- Create: `internal/database/envvars.go`, `internal/database/envvars_test.go`

- [ ] **Step 1: Write tests for env var queries**

Tests for:
- `TestPushEnvVars` — stores blob, returns version=1
- `TestPushEnvVarsOptimisticLock` — rejects push if version mismatch
- `TestPullEnvVars` — returns blob + version
- `TestGetEnvVersion` — returns version without blob
- `TestEnvVarHistory` — push creates history entry
- `TestEnvVarHistoryPruning` — only last 10 versions retained, older pruned on push

- [ ] **Step 2: Run tests, verify fail**

```bash
go test ./internal/database/ -v -run TestPush
```

- [ ] **Step 3: Implement env var queries**

`PushEnvVars(db, projectID, environment string, blob []byte, expectedVersion int, userID string) (newVersion int, error)`
`PullEnvVars(db, projectID, environment string) (blob []byte, version int, error)`
`GetEnvVersion(db, projectID, environment string) (version int, error)`

Push includes: insert into `env_var_history`, then `DELETE FROM env_var_history WHERE project_id=? AND environment=? AND version <= (SELECT MAX(version) - 10 FROM env_var_history WHERE project_id=? AND environment=?)` to prune.

- [ ] **Step 4: Run tests, verify pass**

```bash
go test ./internal/database/ -v -run TestPush
go test ./internal/database/ -v -run TestPull
go test ./internal/database/ -v -run TestGetEnv
go test ./internal/database/ -v -run TestEnvVar
```

- [ ] **Step 5: Commit**

```bash
git add internal/database/envvars.go internal/database/envvars_test.go
git commit -m "feat: env var database queries with optimistic locking and history pruning"
```

---

## Task 5b: Database Layer — Token Queries

**Files:**
- Create: `internal/database/tokens.go`, `internal/database/tokens_test.go`

- [ ] **Step 1: Write tests for token queries**

Tests for:
- `TestCreateToken` — creates token with hash, returns ID
- `TestFindTokenByHash` — looks up token, checks expiry
- `TestFindTokenByHashExpired` — returns error for expired token
- `TestConsumeProvisionToken` — atomic single-use via `DELETE ... RETURNING`, second lookup fails
- `TestListTokensForUser` — returns tokens without hash, with last_used
- `TestRevokeToken` — deletes token
- `TestUpdateLastUsed` — updates last_used timestamp

- [ ] **Step 2: Run tests, verify fail**

```bash
go test ./internal/database/ -v -run TestCreateToken
```

- [ ] **Step 3: Implement token queries**

- [ ] **Step 4: Run tests, verify pass**

```bash
go test ./internal/database/ -v -run TestToken
go test ./internal/database/ -v -run TestConsume
go test ./internal/database/ -v -run TestRevoke
go test ./internal/database/ -v -run TestUpdateLast
```

- [ ] **Step 5: Commit**

```bash
git add internal/database/tokens.go internal/database/tokens_test.go
git commit -m "feat: token database queries with atomic provision token consumption"
```

---

## Task 5c: Database Layer — Session & Audit Queries

**Files:**
- Create: `internal/database/sessions.go`, `internal/database/sessions_test.go`, `internal/database/audit.go`, `internal/database/audit_test.go`

- [ ] **Step 1: Write tests for session queries**

Tests for:
- `TestCreateSession` — creates session with 15-min expiry
- `TestFindSession` — finds valid session by token hash
- `TestFindExpiredSession` — returns error for expired session
- `TestDeleteSession` — invalidates session
- `TestCleanupExpiredSessions` — removes all expired

- [ ] **Step 2: Run tests, verify fail**

- [ ] **Step 3: Implement session queries**

- [ ] **Step 4: Run tests, verify pass**

- [ ] **Step 5: Write tests for audit log**

Tests for:
- `TestLogEvent` — inserts audit entry with all fields
- `TestLogEventMetadata` — stores JSON metadata (e.g., `{"version": 3}` for env.pull)
- `TestLogEventWithoutOptionalFields` — user_id, project_id can be nil (e.g., auth.failure)

- [ ] **Step 6: Run tests, verify fail**

- [ ] **Step 7: Implement audit log**

- [ ] **Step 8: Run all database tests**

```bash
go test ./internal/database/ -v -race
```

- [ ] **Step 9: Commit**

```bash
git add internal/database/sessions.go internal/database/sessions_test.go \
        internal/database/audit.go internal/database/audit_test.go
git commit -m "feat: session and audit log database queries"
```

---

## Task 6: Crypto Layer — Encryption Interface & Age Implementation

**Files:**
- Create: `internal/crypto/encrypt.go`, `internal/crypto/encrypt_test.go`, `internal/crypto/hkdf.go`, `internal/crypto/hkdf_test.go`

- [ ] **Step 1: Install age dependency**

```bash
go get filippo.io/age
```

- [ ] **Step 2: Write tests for encryption**

Tests for:
- `TestEncryptDecryptRoundtrip` — encrypt then decrypt returns original data
- `TestDecryptWithWrongKey` — returns error
- `TestEncryptorInterface` — verify the interface works with age impl

- [ ] **Step 3: Implement encryption interface + age implementation**

Create `internal/crypto/encrypt.go`:

```go
package crypto

// Encryptor is the interface for encrypting/decrypting env var blobs.
// Implementations can be swapped (server-side age now, client-side later).
type Encryptor interface {
	Encrypt(plaintext []byte, projectID string) ([]byte, error)
	Decrypt(ciphertext []byte, projectID string) ([]byte, error)
}
```

Implement `AgeEncryptor` that:
- Loads age identity from file or `DBX_AGE_KEY` env var
- Derives per-project key via HKDF
- Encrypts/decrypts using derived key

- [ ] **Step 4: Write tests for HKDF key derivation**

Tests for:
- `TestDeriveKey` — same master + project always produces same derived key
- `TestDeriveKeyDifferentProjects` — different projects produce different keys
- `TestDeriveKeyDeterministic` — multiple calls return same result

- [ ] **Step 5: Implement HKDF**

Create `internal/crypto/hkdf.go` using `golang.org/x/crypto/hkdf` with `HKDF-SHA256(master_key, project_id, "devbox-project-encryption")`.

- [ ] **Step 6: Add memory protection (best-effort)**

Add to `AgeEncryptor` initialization:
- Attempt `mlock` on loaded key material (log warning if `RLIMIT_MEMLOCK` insufficient)
- Set `RLIMIT_CORE=0` to disable core dumps
- Zero buffers after use in Decrypt/Encrypt methods
- Document platform-specific limitations in code comments

- [ ] **Step 7: Run tests**

```bash
go test ./internal/crypto/ -v -race
```

- [ ] **Step 8: Commit**

```bash
git add internal/crypto/ go.mod go.sum
git commit -m "feat: encryption layer with age + HKDF per-project key derivation"
```

---

## Task 7: Crypto Layer — Token Generation, Nonce Store, SSH Signing

**Files:**
- Create: `internal/crypto/tokens.go`, `internal/crypto/tokens_test.go`, `internal/crypto/nonce.go`, `internal/crypto/nonce_test.go`, `internal/crypto/ssh.go`, `internal/crypto/ssh_test.go`

- [ ] **Step 1: Write tests for token generation**

Tests for:
- `TestGenerateToken` — returns 256-bit entropy token with prefix
- `TestGenerateTokenPAT` — has `dbx_pat_` prefix
- `TestGenerateTokenProvision` — has `dbx_prov_` prefix
- `TestHashToken` — SHA-256 hash is deterministic
- `TestTokensAreUnique` — two generated tokens differ

- [ ] **Step 2: Implement token generation**

- [ ] **Step 3: Write tests for nonce store**

Tests for:
- `TestNonceCreateAndConsume` — create nonce, consume it, returns true
- `TestNonceDoubleConsume` — second consume returns false
- `TestNonceExpiry` — expired nonce returns false
- `TestNonceCleanup` — expired nonces are cleaned up

- [ ] **Step 4: Implement nonce store**

In-memory map with TTL. Background cleanup goroutine.

- [ ] **Step 5: Write tests for SSH signing/verification**

Tests for:
- `TestSignAndVerify` — generate ephemeral key, sign nonce with namespace `devbox-auth@v1`, verify signature
- `TestVerifyWrongNamespace` — signature with different namespace fails
- `TestVerifyWrongKey` — signature from different key fails

- [ ] **Step 6: Implement SSH signing/verification**

Use `golang.org/x/crypto/ssh` for signature creation and verification. Use SSH signature namespace `devbox-auth@v1`.

- [ ] **Step 7: Run tests**

```bash
go test ./internal/crypto/ -v -race
```

- [ ] **Step 8: Commit**

```bash
git add internal/crypto/
git commit -m "feat: token generation, nonce store, and SSH signature verification"
```

---

## Task 8: Server Core — HTTP Server, Middleware, Health

**Files:**
- Create: `internal/server/server.go`, `internal/server/server_test.go`, `internal/server/middleware.go`, `internal/server/middleware_test.go`, `internal/server/health_handler.go`

- [ ] **Step 1: Install chi router**

```bash
go get github.com/go-chi/chi/v5
```

- [ ] **Step 2: Write test for health endpoint**

```go
func TestHealthEndpoint(t *testing.T) {
	srv := newTestServer(t)
	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)
	srv.Handler.ServeHTTP(resp, req)

	if resp.Code != 200 {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
}
```

- [ ] **Step 3: Implement server, router, and health handler**

Create `internal/server/server.go` with `NewServer(db, encryptor, nonceStore, config)` that sets up the chi router with all middleware and routes. Implement graceful shutdown.

Create `internal/server/health_handler.go` with GET `/health`.

- [ ] **Step 4: Write tests for middleware**

Tests for:
- `TestRateLimiter` — 11th request in 1 minute returns 429
- `TestRateLimiterDifferentIPs` — different IPs have separate limits
- `TestRequestSizeLimit` — body over 1MB returns 413
- `TestAuthMiddleware` — request without session/token returns 401
- `TestAuthMiddlewareValidSession` — request with valid session passes

- [ ] **Step 5: Implement middleware**

Create `internal/server/middleware.go`:
- `RateLimiter(maxPerMinute int)` — in-memory per-IP rate limiter
- `RequireAuth(db)` — extracts session token from `Authorization: Bearer` header, validates against sessions table, sets user in context
- `MaxBodySize(n int64)` — wraps request body with `http.MaxBytesReader`

- [ ] **Step 6: Run tests**

```bash
go test ./internal/server/ -v -race
```

- [ ] **Step 7: Commit**

```bash
git add internal/server/ go.mod go.sum
git commit -m "feat: HTTP server core with health endpoint, rate limiter, auth middleware"
```

---

## Task 9: Server Auth Handlers

**Files:**
- Create: `internal/server/auth_handlers.go`, `internal/server/auth_handlers_test.go`

- [ ] **Step 1: Write tests for registration**

Tests for:
- `TestRegister` — POST `/auth/register` with SSH public key creates user, returns user info
- `TestRegisterFirstUserIsAdmin` — first registered user has is_admin=true
- `TestRegisterDuplicateKey` — returns 409 Conflict

- [ ] **Step 2: Implement registration handler**

- [ ] **Step 3: Write tests for SSH challenge-response**

Tests for:
- `TestChallenge` — POST `/auth/challenge` returns nonce with expires_at
- `TestVerify` — POST `/auth/verify` with valid signature returns session token
- `TestVerifyInvalidSignature` — returns 401
- `TestVerifyExpiredNonce` — returns 401
- `TestVerifyConsumedNonce` — second verify with same nonce returns 401

- [ ] **Step 4: Implement challenge + verify handlers**

- [ ] **Step 5: Write tests for PAT/provision token auth**

Tests for:
- `TestTokenAuth` — POST `/auth/token` with valid PAT returns session token
- `TestTokenAuthExpired` — expired token returns 401
- `TestTokenAuthProvisionSingleUse` — provision token works once, second attempt returns 401

- [ ] **Step 6: Implement token auth handler**

- [ ] **Step 7: Write test for logout**

- `TestLogout` — POST `/auth/logout` invalidates session, subsequent requests with that session return 401

- [ ] **Step 8: Implement logout handler**

- [ ] **Step 9: Wire audit logging into all auth handlers**

Every auth handler must call `audit.LogEvent()`:
- Register success → `user.create`
- Challenge → no log (unauthenticated)
- Verify success → `auth.success` with `{"method": "ssh", "fingerprint": "..."}`
- Verify failure → `auth.failure` with `{"reason": "...", "fingerprint": "..."}`
- Token auth success → `auth.success` with `{"method": "pat|provision", "token_name": "..."}`
- Token auth failure → `auth.failure` with `{"reason": "..."}`
- Logout → `auth.logout`

- [ ] **Step 10: Run tests**

```bash
go test ./internal/server/ -v -race
```

- [ ] **Step 11: Commit**

```bash
git add internal/server/auth_handlers.go internal/server/auth_handlers_test.go
git commit -m "feat: server auth handlers — register, challenge, verify, token auth, logout"
```

---

## Task 10: Server Project & Env Handlers

**Files:**
- Create: `internal/server/project_handlers.go`, `internal/server/project_handlers_test.go`, `internal/server/env_handlers.go`, `internal/server/env_handlers_test.go`

- [ ] **Step 1: Write tests for project endpoints**

Tests for:
- `TestCreateProject` — POST `/projects` creates project, returns it
- `TestListProjects` — GET `/projects` returns only user's projects
- `TestDeleteProject` — DELETE `/projects/:id` removes project
- `TestDeleteProjectUnauthorized` — non-admin member gets 404
- `TestProjectNotFoundReturns404` — not 403

- [ ] **Step 2: Implement project handlers**

- [ ] **Step 3: Write tests for env endpoints**

Tests for:
- `TestPushEnv` — PUT `/projects/:id/env` with blob and version, returns new version
- `TestPullEnv` — GET `/projects/:id/env` returns encrypted blob
- `TestPushEnvVersionConflict` — push with stale version returns 409
- `TestGetEnvVersion` — GET `/projects/:id/env/version` returns version without blob
- `TestEnvUnauthorized` — non-member gets 404
- `TestPushEnvUpdatesEnvFile` — push with `env_file` field updates project record

- [ ] **Step 4: Implement env handlers**

Server encrypts/decrypts using the `Encryptor` interface. Push encrypts, stores blob. Pull retrieves blob, decrypts, returns plaintext. Env file name stored on project.

Server-side blob size limit: reject PUT requests with `Content-Length > 64KB` (return 413).

- [ ] **Step 5: Wire audit logging**

- Project create → `project.create`
- Project delete → `project.delete`
- Env pull → `env.pull` with `{"version": N}`
- Env push → `env.push` with `{"old_version": N, "new_version": N}`
- Env force push → `env.force_push`

- [ ] **Step 6: Run tests**

```bash
go test ./internal/server/ -v -race
```

- [ ] **Step 7: Commit**

```bash
git add internal/server/project_handlers.go internal/server/project_handlers_test.go \
        internal/server/env_handlers.go internal/server/env_handlers_test.go
git commit -m "feat: server project and env var handlers with optimistic locking"
```

---

## Task 11: Server Token Handlers

**Files:**
- Create: `internal/server/token_handlers.go`, `internal/server/token_handlers_test.go`

- [ ] **Step 1: Write tests for token endpoints**

Tests for:
- `TestCreatePAT` — POST `/tokens` with type=pat returns prefixed token
- `TestCreateProvision` — POST `/tokens` with type=provision, project-scoped
- `TestListTokens` — GET `/tokens` returns tokens without hashes, with last_used
- `TestRevokeToken` — DELETE `/tokens/:id` removes token
- `TestRevokeOtherUserToken` — returns 404
- `TestTokenMandatoryExpiry` — token without TTL gets default 90d, max 365d

- [ ] **Step 2: Implement token handlers**

- [ ] **Step 3: Run tests**

```bash
go test ./internal/server/ -v -race
```

- [ ] **Step 4: Commit**

```bash
git add internal/server/token_handlers.go internal/server/token_handlers_test.go
git commit -m "feat: server token handlers — create, list, revoke with scoped permissions"
```

---

## Task 12: Server Binary — Cobra Commands

**Files:**
- Modify: `cmd/dbx-server/main.go`

- [ ] **Step 1: Install cobra**

```bash
go get github.com/spf13/cobra
```

- [ ] **Step 2: Implement server CLI with cobra**

Implement `cmd/dbx-server/main.go` with subcommands:
- `serve` (default) — start the HTTP server with flags: `--data`, `--age-key`, `--listen`, `--tls-cert`, `--tls-key`, `--allow-root`, `--no-tls` (for local dev / reverse proxy setups)
- `backup --output <path>` — SQLite online backup via `sqlite3.Backup` API
- `emergency-revoke-all` — delete all sessions and tokens, log to audit

Server startup checks:
- Refuse to start if running as root (unless `--allow-root`)
- Default listen address: `127.0.0.1:8443`
- TLS 1.2 minimum when TLS enabled
- `--no-tls` flag for local dev and reverse proxy deployments (binds HTTP, not HTTPS)
- Auto-generate age identity if `--age-key` path doesn't exist

- [ ] **Step 3: Write tests for backup command**

Test that `dbx-server backup` creates a valid SQLite copy with all data intact.

- [ ] **Step 4: Write tests for emergency-revoke-all**

Test that `dbx-server emergency-revoke-all` deletes all rows from `sessions` and `tokens` tables, logs an audit event.

- [ ] **Step 5: Verify server starts and serves health endpoint**

```bash
make build
./bin/dbx-server --data /tmp/test-dbx.db --age-key /tmp/test-age.key --listen 127.0.0.1:9999 --no-tls
# In another terminal:
curl http://127.0.0.1:9999/health
```

- [ ] **Step 6: Commit**

```bash
git add cmd/dbx-server/ go.mod go.sum
git commit -m "feat: server binary with serve, backup, and emergency-revoke-all commands"
```

---

## Task 13: CLI Core — Config, Git Remote, Env Detection

**Files:**
- Create: `internal/cli/config.go`, `internal/cli/config_test.go`, `internal/cli/git.go`, `internal/cli/git_test.go`, `internal/cli/detect.go`, `internal/cli/detect_test.go`

- [ ] **Step 1: Write tests for git remote parsing**

Tests for:
- `TestNormalizeRemoteSSH` — `git@github.com:user/repo.git` → `github.com/user/repo`
- `TestNormalizeRemoteHTTPS` — `https://github.com/user/repo` → `github.com/user/repo`
- `TestNormalizeRemoteHTTPSWithGit` — `https://github.com/user/repo.git` → `github.com/user/repo`
- `TestGetRemoteURL` — reads origin remote from a real git repo (create temp repo in test)
- `TestGetRepoRoot` — returns correct root in a worktree

- [ ] **Step 2: Run tests, verify fail**

- [ ] **Step 3: Implement git remote parsing**

Create `internal/cli/git.go` with:
- `NormalizeRemoteURL(rawURL string) string`
- `GetRemoteURL(repoPath string) (string, error)` — runs `git remote get-url origin`
- `GetRepoRoot(path string) (string, error)` — runs `git rev-parse --show-toplevel`

- [ ] **Step 4: Run tests, verify pass**

- [ ] **Step 5: Write tests for env file detection**

Tests for:
- `TestDetectNodeJS` — `package.json` present → `.env.local`
- `TestDetectTypeScript` — `tsconfig.json` present → `.env.local`
- `TestDetectNextJS` — `next.config.js` present → `.env.local`
- `TestDetectPython` — `pyproject.toml` present → `.env`
- `TestDetectGo` — `go.mod` present → `.env`
- `TestDetectRust` — `Cargo.toml` present → `.env`
- `TestDetectUnknown` — no markers → `.env`
- `TestDetectPriority` — both `package.json` and `go.mod` → `.env.local` (Node wins)

- [ ] **Step 6: Run tests, verify fail**

- [ ] **Step 7: Implement env file detection**

Create `internal/cli/detect.go` with:
- `DetectEnvFile(repoRoot string) (filename string, projectType string)`

- [ ] **Step 8: Run tests, verify pass**

- [ ] **Step 9: Write tests for config**

Tests for:
- `TestLoadConfigMissing` — returns empty config, no error
- `TestSaveAndLoadConfig` — round-trip config.toml
- `TestConfigServerURL` — reads server URL
- `TestConfigCustomCA` — reads tls_ca path

- [ ] **Step 10: Run tests, verify fail**

- [ ] **Step 11: Implement config**

Create `internal/cli/config.go` with:
- `LoadConfig() (Config, error)` — reads `~/.config/dbx/config.toml`
- `SaveConfig(Config) error` — writes config.toml
- `Config` struct with `Server`, `SSHKey`, `TLSCA` fields

- [ ] **Step 12: Run tests, verify pass**

- [ ] **Step 13: Commit**

```bash
git add internal/cli/config.go internal/cli/config_test.go \
        internal/cli/git.go internal/cli/git_test.go \
        internal/cli/detect.go internal/cli/detect_test.go
git commit -m "feat: CLI core — config, git remote parsing, env file detection"
```

---

## Task 13b: CLI Core — Output Helpers & HTTP Client

**Files:**
- Create: `internal/cli/output.go`, `internal/cli/output_test.go`, `internal/cli/client.go`, `internal/cli/client_test.go`

- [ ] **Step 1: Write tests for output helpers**

Tests for:
- `TestPrinterInfo` — writes to stderr
- `TestPrinterQuiet` — suppresses info in quiet mode
- `TestPrinterJSON` — `Data()` outputs JSON to stdout
- `TestPrinterNoColor` — respects `NO_COLOR` env var
- `TestPrinterCI` — detects `CI=true`, disables color and spinners

- [ ] **Step 2: Run tests, verify fail**

- [ ] **Step 3: Implement output helpers**

Create `internal/cli/output.go` with:
- `Printer` struct with `Quiet`, `Verbose`, `JSON`, `NoColor`, `IsCI` fields
- `NewPrinter()` — auto-detects from env/isatty
- `Info(msg)`, `Error(msg)`, `Success(msg)` — to stderr
- `Data(v any)` — to stdout (JSON if format=json)

- [ ] **Step 4: Run tests, verify pass**

- [ ] **Step 5: Write tests for HTTP client**

Tests for:
- `TestClientCustomCA` — loads custom CA cert for self-signed servers
- `TestClientAuthHeader` — sets `Authorization: Bearer` header
- `TestClientRoundtrip` — mock server, verify request/response

- [ ] **Step 6: Run tests, verify fail**

- [ ] **Step 7: Implement HTTP client**

Create `internal/cli/client.go` with:
- `Client` struct wrapping `http.Client` with base URL, auth token, custom CA support
- Methods matching each API endpoint
- Auto-sets `Authorization: Bearer <token>` header

- [ ] **Step 8: Run tests, verify pass**

- [ ] **Step 9: Commit**

```bash
git add internal/cli/output.go internal/cli/output_test.go \
        internal/cli/client.go internal/cli/client_test.go
git commit -m "feat: CLI output helpers (CI-aware) and HTTP client with custom CA support"
```

---

## Task 14: CLI Auth Commands

**Files:**
- Create: `internal/cli/auth_cmd.go`, `internal/cli/auth_cmd_test.go`

- [ ] **Step 1: Write tests for auth commands**

Tests for (use a mock HTTP server):
- `TestAuthLoginNewUser` — registers SSH key, saves config
- `TestAuthLoginExistingUser` — challenge/verify flow, gets session
- `TestAuthLoginNoSSHKey` — returns meaningful error with discovery chain
- `TestAuthStatusAuthenticated` — shows method, identity, server
- `TestAuthStatusNotAuthenticated` — shows resolution chain with what was checked
- `TestAuthStatusJSON` — outputs JSON when `--format json`
- `TestAuthLogout` — calls logout endpoint, clears config

- [ ] **Step 2: Run tests, verify fail**

- [ ] **Step 3: Implement auth login command**

`dbx auth login --server <url>`:
1. Discover SSH key (agent → `id_ed25519` → `id_ed25519_sk` → `id_ecdsa` → `id_rsa`)
2. Try `/auth/challenge` + `/auth/verify` first (existing user)
3. If fingerprint not found, call `/auth/register` to create user
4. Save server URL to config
5. Print success message with identity info

- [ ] **Step 4: Implement auth status command**

- [ ] **Step 5: Implement auth logout command**

- [ ] **Step 6: Run tests, verify pass**

- [ ] **Step 7: Commit**

```bash
git add internal/cli/auth_cmd.go internal/cli/auth_cmd_test.go
git commit -m "feat: CLI auth commands — login, status, logout"
```

---

## Task 15: CLI Project Commands — init, list, delete

**Files:**
- Create: `internal/cli/project_cmd.go`, `internal/cli/project_cmd_test.go`

- [ ] **Step 1: Write tests for project commands**

Tests for:
- `TestInitFromGitRemote` — reads remote, normalizes, creates project, detects env file
- `TestInitWithExplicitName` — `--name` overrides git remote
- `TestInitNoGitRepo` — error with instructions
- `TestInitMergedLogin` — detects missing auth, prompts for server URL
- `TestInitDetectsEnvFile` — Node.js repo gets `.env.local`
- `TestProjectList` — lists projects with name, remote, env_file
- `TestProjectListJSON` — JSON output
- `TestProjectDelete` — deletes project
- `TestProjectDeleteRequiresConfirmation` — without `--yes`, prompts

- [ ] **Step 2: Run tests, verify fail**

- [ ] **Step 3: Implement init command**

`dbx init [--name <name>]`:
1. If no config exists, run interactive login flow (prompt for server URL)
2. Read git remote origin, normalize
3. Detect env file type
4. Create project on server with remote URL, name (default from dir name), env_file
5. Print success with next steps

- [ ] **Step 4: Implement project list and delete commands**

- [ ] **Step 5: Run tests, verify pass**

- [ ] **Step 6: Commit**

```bash
git add internal/cli/project_cmd.go internal/cli/project_cmd_test.go
git commit -m "feat: CLI project commands — init, list, delete"
```

---

## Task 16: CLI Env Commands — push, pull, diff

**Files:**
- Create: `internal/cli/env_cmd.go`, `internal/cli/env_cmd_test.go`

- [ ] **Step 1: Write tests for env commands**

Tests for:
- `TestPullWritesEnvFile` — pulls and writes file with 0600 permissions
- `TestPullAutoDetectsEnvFile` — Node.js project writes `.env.local`, Python writes `.env`
- `TestPullFallbackToServerEnvFile` — when no markers, uses server's `env_file` field
- `TestPullEnvFileOverride` — `--env-file .env` overrides detection
- `TestPullPromptOnExisting` — existing file triggers confirmation prompt
- `TestPullForceOverwrites` — `--force` skips prompt
- `TestPullCIAutoForce` — `CI=true` auto-forces
- `TestPullDiffOnly` — `--diff` shows diff, doesn't write
- `TestPullBackup` — `--backup` saves old file as `.backup`
- `TestPullCached` — `--cached` uses cached blob when server unreachable
- `TestPullChecksGitignore` — warns if env file not in `.gitignore`
- `TestPullWarnsSymlink` — warns if target is a symlink
- `TestPushReadsEnvFile` — pushes file content to server
- `TestPushValidatesUTF8` — rejects binary content
- `TestPushRejectsTooLarge` — rejects files over 64KB
- `TestPushOptimisticLock` — sends version, handles 409 conflict
- `TestPushUpdatesEnvFile` — updates server's env_file field
- `TestDiff` — shows added/removed/changed keys (values masked)
- `TestPullExitCodes` — exit 0=success, 2=auth error, 3=not found

- [ ] **Step 2: Run tests, verify fail**

- [ ] **Step 3: Implement pull command**

`dbx pull [--force] [--diff] [--backup] [--project <name>] [--env-file <name>]`:
1. Resolve project (git remote → server lookup, or `--project`)
2. Determine env filename (auto-detect → server record → `--env-file`)
3. Pull env blob from server
4. If env file exists and differs:
   - Show diff summary (N changed, N added, N removed)
   - If `--force` or `CI=true`: overwrite
   - If `--diff`: show diff and exit
   - If `--backup`: save old as `<file>.backup`
   - Else: prompt y/N/d(iff)
5. Write env file with `0600` permissions
6. Check `.gitignore` — warn if env file not listed
7. Warn if env file is a symlink
8. Cache blob in `.dbx/cache/` with version in `state.toml`
9. Print what was written
10. If server unreachable and `--cached` flag: use cached blob from `.dbx/cache/`, print warning with cache timestamp

- [ ] **Step 2: Implement push command**

`dbx push [--force] [--project <name>] [--env-file <name>]`:
1. Resolve project
2. Determine env filename
3. Read env file from disk
4. Validate: UTF-8, not binary, under 64KB
5. Read `last_pulled_version` from `.dbx/cache/state.toml`
6. Push to server with expected version
7. Handle 409 Conflict (show error, suggest pull first or `--force`)
8. Update `state.toml` with new version
9. Update server's `env_file` field if detected type changed
10. Print success

- [ ] **Step 3: Implement diff command**

`dbx diff [--project <name>]`:
1. Resolve project
2. Pull env blob from server (don't write)
3. Read local env file
4. Compare and show diff (added, removed, changed keys — values masked)

- [ ] **Step 4: Commit**

```bash
git add internal/cli/env_cmd.go internal/cli/env_cmd_test.go
git commit -m "feat: CLI env commands — pull, push, diff with overwrite protection"
```

---

## Task 17: CLI Token & Utility Commands

**Files:**
- Create: `internal/cli/token_cmd.go`, `internal/cli/token_cmd_test.go`, `internal/cli/whoami_cmd.go`, `internal/cli/completion_cmd.go`

- [ ] **Step 1: Write tests for token commands**

Tests for:
- `TestTokenCreatePAT` — creates PAT with `dbx_pat_` prefix, prints raw token once
- `TestTokenCreateProvision` — creates provision token with `dbx_prov_` prefix
- `TestTokenCreateDefaultTTL` — defaults to 90d
- `TestTokenCreateMaxTTL` — rejects TTL > 365d
- `TestTokenList` — lists tokens with name, type, scope, last_used
- `TestTokenListJSON` — JSON output
- `TestTokenRevoke` — revokes token by name
- `TestWhoami` — shows identity, auth method, server
- `TestWhoamiJSON` — JSON output
- `TestWhoamiNotAuthenticated` — shows error with exit code 2

- [ ] **Step 2: Run tests, verify fail**

- [ ] **Step 3: Implement token commands**

`dbx token create --name <n> [--type pat|provision] [--scope <s>] [--ttl <d>]`:
- Create token on server, print the raw token (only shown once)
- Default type: `pat`, default scope: `project:read`, default TTL: `90d`
- Validate max TTL: 365d

`dbx token list [--format json]`:
- List tokens with name, type, scope, created, expires, last_used

`dbx token revoke <name>`:
- Revoke token by name

- [ ] **Step 4: Implement whoami command**

`dbx whoami [--format json]`:
- Show current identity (user name, auth method, server URL)

- [ ] **Step 5: Implement completion command**

`dbx completion {bash,zsh,fish}`:
- Print shell completion script (cobra built-in)

- [ ] **Step 6: Run tests, verify pass**

- [ ] **Step 7: Commit**

```bash
git add internal/cli/token_cmd.go internal/cli/token_cmd_test.go \
        internal/cli/whoami_cmd.go internal/cli/completion_cmd.go
git commit -m "feat: CLI token, whoami, and completion commands"
```

---

## Task 18: CLI Binary — Wire Everything Together

**Files:**
- Modify: `cmd/dbx/main.go`

- [ ] **Step 1: Wire all commands into cobra root**

Update `cmd/dbx/main.go`:
- Root command with `--verbose`, `--quiet` flags
- `--version` flag
- Add all subcommands: `auth`, `init`, `push`, `pull`, `diff`, `project`, `token`, `whoami`, `completion`
- Auth resolution: check `DEVBOX_TOKEN` env var first, then SSH key
- Exit codes: 0=success, 1=general error, 2=auth error, 3=not found
- CI detection: check `CI=true` env var, pass to Printer for auto-force and suppressed prompts

- [ ] **Step 2: Build and smoke test**

```bash
make build
./bin/dbx --help
./bin/dbx auth status
./bin/dbx --version
```

- [ ] **Step 3: Commit**

```bash
git add cmd/dbx/
git commit -m "feat: wire all CLI commands into dbx binary"
```

---

## Task 19: Integration Test — Full Push/Pull Cycle

**Files:**
- Create: `tests/integration_test.go`

- [ ] **Step 1: Write integration test**

End-to-end test that:
1. Starts `dbx-server` in-process with a temp SQLite DB and temp age key
2. Registers a user via the API
3. Creates a project with a remote URL
4. Pushes a `.env.local` file
5. Pulls it back and verifies content matches
6. Pushes again and verifies version increments
7. Verifies optimistic locking (push with stale version → 409)
8. Creates a PAT, authenticates with it, pulls successfully
9. Creates a provision token, uses it once, verifies second use fails

- [ ] **Step 2: Run integration test**

```bash
go test ./tests/ -v -race
```

- [ ] **Step 3: Commit**

```bash
git add tests/
git commit -m "test: end-to-end integration test for full push/pull cycle"
```

---

## Task 20: Distribution — Dockerfile, GoReleaser, Install Script

**Files:**
- Create: `Dockerfile`, `.goreleaser.yaml`, `install.sh`

- [ ] **Step 1: Create Dockerfile**

```dockerfile
FROM alpine:3.19 AS certs
RUN apk add --no-cache ca-certificates

FROM scratch
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY dbx-server /usr/local/bin/dbx-server
USER 65534:65534
ENTRYPOINT ["dbx-server"]
CMD ["serve"]
```

- [ ] **Step 2: Create GoReleaser config**

Create `.goreleaser.yaml` with:
- Two builds: `dbx` and `dbx-server`
- Cross-compile: darwin/amd64, darwin/arm64, linux/amd64, linux/arm64
- GitHub releases with checksums
- Homebrew tap auto-update
- Docker image build and push

- [ ] **Step 3: Create install script**

Create `install.sh` that:
- Detects OS and architecture
- Downloads correct binary from GitHub releases
- Verifies SHA-256 checksum
- Installs to `/usr/local/bin` (or `~/.local/bin` if no root)

- [ ] **Step 4: Verify Docker build**

```bash
docker build -t dbx-server-test .
```

- [ ] **Step 5: Commit**

```bash
git add Dockerfile .goreleaser.yaml install.sh
git commit -m "feat: distribution — Dockerfile, GoReleaser config, curl install script"
```

---

## Task 21: Final Verification

- [ ] **Step 1: Run full test suite**

```bash
make test
```

All tests pass, no race conditions.

- [ ] **Step 2: Build both binaries**

```bash
make build
```

Both compile without errors.

- [ ] **Step 3: Manual smoke test**

Start server, register, init, push, pull in a real repo. Verify the env file is written correctly.

- [ ] **Step 4: Verify env file detection**

Test in a Node.js project (writes `.env.local`) and a Python project (writes `.env`).

- [ ] **Step 5: Final commit**

```bash
git add -A
git commit -m "chore: final verification pass"
```
