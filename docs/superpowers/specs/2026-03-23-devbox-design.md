# Devbox Design Spec

**Project name**: Devbox
**CLI binary**: `dbx`
**Server binary**: `dbx-server`
**Language**: Go
**Date**: 2026-03-23

---

## 1. Problem

Every time a new worktree or cloud environment (Claude Code web, Codex, CI) is created, environment variables must be manually recreated. Locally this means copying `.env.local`; in cloud environments there's no file to copy from. Existing tools either require heavy infrastructure (Infisical, Phase), are SaaS-only (Doppler, 1Password), or lack a push/pull model (SOPS, dotenvx).

Devbox is a self-hosted CLI tool + server that lets you pull your project's `.env.local` from any environment with a single authenticated command.

## 2. Architecture

Two components communicating over HTTPS:

```
┌─────────────────────┐         HTTPS           ┌──────────────────────────┐
│      dbx CLI        │◄────────────────────────►│      dbx-server          │
│                     │  SSH challenge / PAT     │                          │
│  (Go static binary) │  + JSON API             │  (Go static binary)      │
└─────────────────────┘                          ├──────────────────────────┤
                                                 │  SQLite (WAL mode)       │
                                                 │  age encryption (v1.3+)  │
                                                 │  age identity (separate) │
                                                 └──────────────────────────┘
```

### Server

- Single Go binary, no runtime dependencies.
- SQLite database for all persistent state.
- Env var values encrypted at rest using `age` (`filippo.io/age` v1.3+, post-quantum capable).
- Per-project encryption keys derived from master key via HKDF.
- Age identity file stored separately from database, mode `0600`.
- Supports env var injection for containerized deployments (`DBX_AGE_KEY` env var).

### CLI

- Single Go static binary, cross-compiled for darwin/linux, amd64/arm64.
- Auto-detects project from git remote origin; `--project` flag overrides.
- Auto-detects env file name from project type; `--env-file` flag overrides.
- Writes env file with `0600` permissions at the git repo root.
- Never silently overwrites — requires `--force` or interactive confirmation.

### Env File Detection

The CLI detects the correct env filename by checking for project markers at the repo root:

| Marker file(s) | Detected type | Env filename |
|---|---|---|
| `package.json` or `tsconfig.json` | Node.js / TypeScript | `.env.local` |
| `next.config.*` or `nuxt.config.*` | Next.js / Nuxt | `.env.local` |
| `pyproject.toml` or `requirements.txt` or `Pipfile` | Python | `.env` |
| `go.mod` | Go | `.env` |
| `Cargo.toml` | Rust | `.env` |
| `Gemfile` | Ruby | `.env` |
| `composer.json` | PHP | `.env` |
| (none matched) | Unknown | `.env` |

Detection runs on every `push` and `pull`. The detected filename is shown in output:

```
$ dbx pull
Pulling "coined" (github.com/user/coined) from my-server.example.com
Detected Node.js project → writing .env.local
Wrote .env.local (14 variables, 1.2 KB)
```

**Override:** `--env-file <name>` on any command:

```
dbx pull --env-file .env          # force .env even in a Node project
dbx push --env-file .env.local    # force .env.local even in a Python project
```

The env filename is stored server-side on the project record so that cloud environments (which may not have project markers yet at pull time) know which file to write. Updated on each push.

### Project Identification

Projects are identified by the normalized git remote URL:

```
git@github.com:user/coined.git   →  github.com/user/coined
https://github.com/user/coined   →  github.com/user/coined
```

- `dbx pull` reads `origin` remote, normalizes, and looks up on server.
- `dbx pull --project coined` overrides with an explicit name.
- `dbx init` registers the current repo's remote as a project on the server.
- No config files committed to the repo. A `.dbx/cache/` directory may exist locally (gitignored) for caching and local state; see Section 7.

## 3. Authentication

Three methods, auto-detected in order:

| Priority | Method | Use case | Lifespan |
|----------|--------|----------|----------|
| 1 | `DEVBOX_TOKEN` env var | Cloud environments, CI | Long-lived (PAT) or single-use |
| 2 | SSH key | Local machines | Permanent |
| 3 | Error with instructions | — | — |

### SSH Key Challenge-Response (local machines)

```
Client                                 Server
  │                                       │
  ├── POST /auth/challenge ──────────────►│
  │◄── { nonce, expires_at } ─────────────┤   (nonce: 32 bytes, 60s TTL)
  │                                       │
  │  [signs with SSH key using namespace  │
  │   "devbox-auth@v1" for domain         │
  │   separation]                         │
  │                                       │
  ├── POST /auth/verify ─────────────────►│
  │   { fingerprint, signature, nonce }   │
  │◄── { session_token } ────────────────►│   (15-min, server-side storage)
  │                                       │
```

**Security properties:**
- Nonces are single-use — consumed on first verification attempt (success or failure).
- Nonces expire in 60 seconds.
- Signed payload uses SSH signature namespace `devbox-auth@v1` for domain separation, preventing cross-protocol replay.
- Rate-limited: 10 requests/min per IP on `/auth/challenge` and `/auth/verify`.
- Session tokens stored server-side in `sessions` table (not stateless JWTs) — revocable via `/auth/logout`.
- Nonces stored in-memory (lost on server restart, which is acceptable — client retries the challenge).

**SSH key discovery order:** `ssh-agent` first, then `~/.ssh/id_ed25519`, `id_ed25519_sk`, `id_ecdsa`, `id_rsa`. Does not consult `~/.ssh/config`.

### Personal Access Tokens (cloud environments)

Created from an SSH-authenticated session:

```
$ dbx token create --name "claude-code-web" --scope project:read --ttl 90d
→ dbx_pat_x9k2m7f3...
```

**Properties:**
- Prefixed: `dbx_pat_` (PATs), `dbx_prov_` (provision tokens) — enables secret scanning.
- Mandatory expiry: default 90 days, max 1 year.
- Scoped permissions: `project:read`, `project:write`, `tokens:create`, `admin`.
- `last_used` timestamp tracked, surfaced in `dbx token list`.
- Revocable instantly: `dbx token revoke "claude-code-web"`.

### Provision Tokens (one-off)

```
$ dbx token create --type provision --project coined --ttl 1h
→ dbx_prov_a8f3e2...
```

- Single-use — atomically consumed via `DELETE ... RETURNING` in SQLite.
- Time-limited and project-scoped.

### Token Storage

- Generated with 256 bits of entropy from `crypto/rand`, encoded as base62.
- Stored as `SHA-256(token)` in the database (high-entropy tokens don't need bcrypt).
- Never use `math/rand`.

## 4. Encryption

### Threat Model

Server-side encryption. The server operator is trusted. This is documented as an explicit design decision. Acceptable for single-developer and small-team self-hosted deployments where the server operator and the users are the same person/team.

### Implementation

- Master key: `age` identity file, stored separately from the database at a configurable path, mode `0600`.
- Per-project keys: derived from master key via `HKDF-SHA256(master_key, project_id, "devbox-project-encryption")`. Limits blast radius if a derived key is exposed.
- Env vars stored as a single encrypted blob per project (atomic replacement on push).
- Version number included in authenticated encryption data to prevent rollback attacks.
- Encryption/decryption is behind a Go interface to enable future client-side encryption without architectural changes.

### Key Rotation (v2)

Schema and encryption interface designed in v1 to support rotation. The `dbx-server rotate-key` command is implemented in v2:
1. Generates new age identity.
2. Re-encrypts all blobs in a transaction (decrypt with old, encrypt with new).
3. Logs rotation event to audit log.

### Memory Protection

Best-effort. Platform-specific limitations are documented.

- `mlock` on key material to prevent swapping to disk (requires `RLIMIT_MEMLOCK` on Linux).
- Disable core dumps (`RLIMIT_CORE=0`).
- Use `memguard` or similar library for sensitive buffers (Go's GC may copy key material).

## 5. Database

### Engine

SQLite with mandatory startup pragmas:

```sql
PRAGMA journal_mode=WAL;
PRAGMA busy_timeout=5000;
PRAGMA foreign_keys=ON;
PRAGMA secure_delete=ON;
```

`SetMaxOpenConns(1)` in Go for write serialization.

Database file created with mode `0600`, containing directory with mode `0700`.

### Schema

```sql
-- Schema version tracking
PRAGMA user_version = 1;

CREATE TABLE users (
    id          TEXT PRIMARY KEY,  -- uuid v4, prefixed: usr_
    name        TEXT NOT NULL,
    is_admin    INTEGER NOT NULL DEFAULT 0,  -- first registered user = 1
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE ssh_keys (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id),
    fingerprint TEXT NOT NULL UNIQUE,
    public_key  TEXT NOT NULL,
    name        TEXT,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE projects (
    id          TEXT PRIMARY KEY,  -- uuid v4, prefixed: proj_
    name        TEXT NOT NULL,      -- human-readable name
    remote_url  TEXT UNIQUE,        -- normalized git remote, nullable for manually named projects
    env_file    TEXT NOT NULL DEFAULT '.env',  -- detected or overridden env filename
    owner_id    TEXT NOT NULL REFERENCES users(id),
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE project_members (
    project_id  TEXT NOT NULL REFERENCES projects(id),
    user_id     TEXT NOT NULL REFERENCES users(id),
    role        TEXT NOT NULL DEFAULT 'reader',  -- reader, writer, admin
    PRIMARY KEY (project_id, user_id)
);

CREATE TABLE env_vars (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects(id),
    environment TEXT NOT NULL DEFAULT 'default',  -- future: dev, staging, prod
    blob        BLOB NOT NULL,      -- age-encrypted .env content
    version     INTEGER NOT NULL DEFAULT 1,
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_by  TEXT NOT NULL REFERENCES users(id),
    UNIQUE(project_id, environment)
);

CREATE TABLE env_var_history (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects(id),
    environment TEXT NOT NULL,
    blob        BLOB NOT NULL,
    version     INTEGER NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    created_by  TEXT NOT NULL REFERENCES users(id)
);

CREATE TABLE tokens (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id),
    name        TEXT,
    hash        TEXT NOT NULL UNIQUE,  -- SHA-256(token)
    type        TEXT NOT NULL,          -- pat, provision
    scope       TEXT NOT NULL,          -- JSON: {"permissions":["project:read"],"project_id":"proj_..."}
    expires_at  TEXT,
    single_use  INTEGER NOT NULL DEFAULT 0,
    last_used   TEXT,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE sessions (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id),
    token_hash  TEXT NOT NULL UNIQUE,  -- SHA-256(session_token)
    expires_at  TEXT NOT NULL,
    ip_address  TEXT,
    user_agent  TEXT,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE audit_log (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    user_id     TEXT,
    project_id  TEXT,
    environment TEXT,
    action      TEXT NOT NULL,  -- auth.success, auth.failure, env.pull, env.push, env.force_push,
                                -- token.create, token.revoke, token.use, project.create, project.delete,
                                -- user.create, key.rotate
    metadata    TEXT,            -- JSON blob for action-specific data:
                                -- env.pull: {"version": N}
                                -- env.push: {"old_version": N, "new_version": N}
                                -- token.create: {"token_name": "...", "scope": "...", "ttl": "..."}
                                -- auth.failure: {"reason": "...", "fingerprint": "..."}
    ip_address  TEXT,
    user_agent  TEXT
);
```

### Migrations

- Embedded SQL files via `//go:embed migrations/*.sql`.
- Tracked via `PRAGMA user_version`.
- Migrations are append-only — never modify a released migration.
- Run inside a transaction on server startup.
- Server refuses to start if DB schema version is higher than binary's latest migration (prevents accidental downgrade).

### Backups

- Built-in: `dbx-server backup --output /path/to/backup.db` using SQLite online backup API.
- Documented cron-based backup strategy.
- Backups inherit the same encryption (env blobs are encrypted at rest).

## 6. API

### Endpoints

**Auth:**
| Method | Path | Description | Auth required |
|--------|------|-------------|---------------|
| POST | `/auth/challenge` | Request nonce for SSH auth | No (rate-limited) |
| POST | `/auth/verify` | Submit SSH signature, get session token | No (rate-limited) |
| POST | `/auth/token` | Authenticate with PAT/provision token | No |
| POST | `/auth/register` | Register user + SSH public key | No (rate-limited) |
| POST | `/auth/logout` | Invalidate session token | Yes |

**Projects:**
| Method | Path | Description | Permission |
|--------|------|-------------|------------|
| GET | `/projects` | List projects user has access to | Authenticated |
| POST | `/projects` | Create a project | Authenticated |
| DELETE | `/projects/:id` | Delete a project | `admin` on project |

**Env vars:**
| Method | Path | Description | Permission |
|--------|------|-------------|------------|
| GET | `/projects/:id/env` | Pull encrypted blob | `project:read` |
| GET | `/projects/:id/env/version` | Check current version (no blob) | `project:read` |
| PUT | `/projects/:id/env` | Push encrypted blob (with version for optimistic lock) | `project:write` |

**Tokens:**
| Method | Path | Description | Permission |
|--------|------|-------------|------------|
| POST | `/tokens` | Create PAT or provision token | Authenticated |
| GET | `/tokens` | List user's tokens | Authenticated |
| DELETE | `/tokens/:id` | Revoke a token | Owner of token |

**Operational:**
| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Health check |

### User Onboarding

- v1 uses open registration: `POST /auth/register` creates a user and associates the SSH public key. The first registered user becomes the server admin.
- `dbx auth login --server <url>` calls `/auth/register` if the key is unknown, or `/auth/challenge` + `/auth/verify` if already registered.
- The `--project` flag on CLI commands matches the `name` column in the `projects` table (case-sensitive, exact match).

### Authorization

- v1 is single-user but implements authorization checks. Project creator is auto-added to `project_members` with `admin` role. All project-scoped endpoints verify membership. This ensures the authz middleware exists and is tested before multi-user is added in v2.
- Unauthorized project access returns 404 (not 403) to avoid confirming project existence.
- Project IDs are UUIDs (not sequential integers) as defense-in-depth.
- Optimistic locking on env push: client sends expected version, server rejects with 409 Conflict on mismatch.

### Rate Limiting

- In-memory rate limiter (acceptable for single-instance server; lost on restart).
- `/auth/challenge`, `/auth/verify`, `/auth/register`: 10 requests/min per IP.
- All other endpoints: 60 requests/min per IP.
- Returns HTTP 429 with `Retry-After` header when exceeded.

### Request Limits

- Max request body: 1 MB (env blobs should never be larger).
- Enforced via `http.MaxBytesReader`.

## 7. CLI Commands

```
dbx init [--name <name>]              # Register project (from git remote or explicit name)
dbx push [--force] [--project <name>] # Upload .env.local to server
dbx pull [--force] [--diff] [--backup] [--project <name>]
                                       # Download .env.local from server
dbx diff [--project <name>]           # Show changes without writing

dbx auth login --server <url>         # Register SSH key with server
dbx auth status                       # Show auth state and resolution chain
dbx auth logout                       # Revoke local auth

dbx token create --name <n> [--type pat|provision] [--scope <s>] [--ttl <d>]
dbx token list [--format json]        # List tokens with last_used
dbx token revoke <name>               # Revoke a token

dbx project list [--format json]      # List all accessible projects
dbx project delete <name> [--yes]     # Delete a project

dbx whoami [--format json]            # Current identity + server

dbx completion {bash,zsh,fish}        # Shell completions
```

### UX Principles

**Auto-detection:** `dbx pull` reads `origin` remote, normalizes, looks up project. `--project` overrides.

**Never silently overwrite:** `dbx pull` when `.env.local` already exists and differs:
```
.env.local already exists and differs from server version.
3 variables changed, 1 added, 0 removed.
Overwrite? [y/N/d(iff)]:
```
`--force` skips confirmation. In non-interactive mode (CI, `!isatty`), refuses without `--force`.

**Merged first-time setup:** `dbx init` detects missing auth, walks through login interactively:
```
$ dbx init
→ No configuration found. Let's set up dbx.
→ Server URL: https://my-server.example.com
→ Registering SSH key... done.
→ Project name (coined):
→ Created project "coined"
→ Run `dbx push` to upload your .env.local.
```

**Always show what was resolved:**
```
$ dbx pull
Pulling .env.local for "coined" (github.com/user/coined) from my-server.example.com
Wrote .env.local (14 variables, 1.2 KB)
```

**Error messages show resolution chain:**
```
Error: No authentication method found.

  dbx checks the following (in order):
    1. DEVBOX_TOKEN env var       — not set
    2. SSH key (~/.ssh/id_ed25519) — file not found

  To set up authentication:
    $ dbx auth login --server https://my-server.example.com
```

**CI detection:** When `CI=true`, suppress prompts/spinners/color. `dbx pull` behaves as if `--force` is passed (overwrites without prompting). Exit with meaningful codes (0=success, 1=error, 2=auth error, 3=not found).

**File safety:**
- Write env file with `0600` permissions.
- On `dbx init` or first `dbx pull`, check `.gitignore` includes the env file (`.env.local`, `.env`, etc.). Warn if not.
- Detect and warn if the env file is a symlink.
- Validate content is UTF-8 text on push. Reject binary.
- Reject blobs over 64 KB on push (env files should not be this large).

**Git worktrees:** Use `git rev-parse --show-toplevel` to find repo root, not `cwd`.

**Output:**
- Success/status output to stderr, data to stdout.
- `--format json` on all list commands for scripting.
- `--verbose` / `--quiet` flags.
- Color respects `NO_COLOR` env var and `isatty` check.
- Spinners for operations >500ms (interactive only).

### Local State

`~/.config/dbx/config.toml`:
```toml
server = "https://my-server.example.com"
ssh_key = "~/.ssh/id_ed25519"  # optional, auto-detected
tls_ca = "/path/to/ca.pem"    # optional, for self-signed certs
```

`.dbx/cache/` (gitignored, at repo root):
- Cached pull blob for offline fallback via `dbx pull --cached`.
- `state.toml` with `last_pulled_version` for optimistic locking on push.

## 8. Server Configuration & Deployment

### Server Startup

```
dbx-server \
  --data /var/lib/dbx/data.db \
  --age-key /etc/dbx/age.key \
  --listen 127.0.0.1:8443 \
  --tls-cert /etc/dbx/cert.pem \
  --tls-key /etc/dbx/key.pem
```

**Defaults:**
- Bind to `127.0.0.1` (localhost only). Explicit flag required for external access.
- Refuse to start if running as root (override: `--allow-root`).
- TLS 1.2 minimum enforced.

**Server subcommands:**
- `dbx-server serve` — run the server (default).
- `dbx-server backup --output <path>` — consistent SQLite backup.
- `dbx-server rotate-key` — rotate master encryption key (v2, subcommand reserved).
- `dbx-server emergency-revoke-all` — invalidate all sessions and tokens.

### Deployment Options

| Option | Setup | Notes |
|--------|-------|-------|
| VPS (Hetzner, DigitalOcean) | curl install + systemd unit | ~$5/mo |
| Fly.io | `fly launch` + persistent volume | Free tier possible |
| Docker | `docker run ghcr.io/user/dbx-server` | Multi-arch image |
| Raspberry Pi | curl install + systemd | $0 |

Recommended TLS: reverse proxy (Caddy or nginx) in front. Autocert supported but opt-in.

## 9. Distribution

### CLI (`dbx`)

Cross-compiled via GoReleaser:

```
dbx-darwin-arm64    (macOS Apple Silicon)
dbx-darwin-amd64    (macOS Intel)
dbx-linux-amd64     (Linux x86_64)
dbx-linux-arm64     (Linux ARM)
```

**Channels:**
- `brew install user/tap/dbx` — Homebrew tap, auto-updated on release.
- `curl -fsSL https://raw.githubusercontent.com/user/devbox/main/install.sh | sh` — detects OS/arch, verifies SHA-256 checksum, installs binary.
- `go install github.com/user/devbox/cmd/dbx@latest` — for Go developers.

### Server (`dbx-server`)

- Same binary cross-compilation via GoReleaser.
- Docker multi-arch image published to GitHub Container Registry.
- Minimal base image (distroless or alpine), runs as non-root.

### Release Security

- All binaries signed with cosign (Sigstore).
- SHA-256 checksums published with every release.
- Curl installer verifies checksum before executing.
- Homebrew formula includes checksums natively.

## 10. Scope

### v1 (MVP)

- Single-user (no team sharing, but schema and authz middleware support it).
- Single environment per project (`default`), but `environment` column exists in schema.
- `env_var_history` retains last 10 versions per project. Older versions are pruned on push. Rollback commands are v2, but the data is preserved.
- SSH key auth + PAT auth + provision tokens.
- `dbx init`, `push`, `pull`, `diff`, `auth login/status/logout`, `token create/list/revoke`, `whoami`.
- Server with SQLite, age encryption, HKDF per-project keys.
- Homebrew tap + curl installer + go install.
- Docker image for server.
- Backup command.

### v2 (Future)

- Multi-user with project membership and roles.
- Multiple environments per project (`dev`, `staging`, `prod`).
- Client-side encryption option (swap via encryption interface).
- `dbx run -- <cmd>` (inject env vars without writing to disk).
- Version history and rollback (`dbx env history`, `dbx env rollback`).
- Audit log export (webhook, syslog).
- Key rotation via `dbx-server rotate-key`.
- TOFU certificate pinning.
- `dbx-server audit` deployment checker.
- Incident response runbook.

## 11. Non-Goals

- Web UI — CLI-only.
- SaaS hosting — self-hosted only.
- Secret rotation automation — out of scope.
- Per-key-value storage — blob model only.
- Merge conflict resolution — overwrite or abort.
