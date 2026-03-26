# Devbox

Self-hosted environment variable management. Push and pull `.env` files across local worktrees, cloud environments (Claude Code, Codex), and CI — with a single authenticated command.

```
$ dbx pull
Pulling "my-project" (github.com/user/my-project) from https://dbx.example.com
Detected Node.js project -> writing .env.local
Wrote .env.local (14 variables, 1.2 KB)
```

## Why

Every time you spin up a new worktree or cloud environment, you need your env vars. Locally you copy `.env.local`. In the cloud, there's nothing to copy from. Devbox fixes this:

- **One command** to pull your env vars into any environment
- **Auto-detects** the right filename (`.env.local` for Node/TS, `.env` for Python/Go/Rust)
- **SSH key auth** locally, **PAT tokens** for cloud environments
- **Self-hosted** — your secrets never touch a third-party server
- **Encrypted at rest** with [age](https://age-encryption.org/) (post-quantum capable)

## Install

### CLI (`dbx`)

**macOS (Homebrew):**

```sh
brew install GedeonIsezerano/tap/dbx
```

**Linux / Cloud environments (curl):**

```sh
curl -fsSL https://raw.githubusercontent.com/GedeonIsezerano/Devbox/main/install.sh | sh
```

**Go:**

```sh
go install github.com/GedeonIsezerano/Devbox/cmd/dbx@latest
```

### Server (`dbx-server`)

**Docker (recommended):**

```sh
docker run -d \
  --name dbx-server \
  -p 8443:8443 \
  -v dbx-data:/data \
  ghcr.io/gedeonisezerano/dbx-server \
  serve --data /data/dbx.db --age-key /data/age.key --listen 0.0.0.0:8443 --no-tls
```

Put a reverse proxy (Caddy, nginx) with TLS in front for production.

**Binary:**

```sh
curl -fsSL https://raw.githubusercontent.com/GedeonIsezerano/Devbox/main/install.sh | sh -s -- --binary dbx-server
dbx-server serve --data ./dbx.db --age-key ./age.key --listen 127.0.0.1:8443 --no-tls
```

**From source:**

```sh
git clone https://github.com/GedeonIsezerano/Devbox.git
cd devbox
make build
./bin/dbx-server serve --data ./dbx.db --age-key ./age.key --no-tls
```

The server auto-generates an age encryption key on first start if the key file doesn't exist.

## Quick Start

### 1. Start the server

```sh
dbx-server serve --data ./dbx.db --age-key ./age.key --listen 127.0.0.1:8443 --no-tls
```

### 2. Log in from your machine

```sh
dbx auth login --server http://localhost:8443
```

This registers your SSH public key with the server. First user becomes admin.

### 3. Initialize a project

```sh
cd ~/Code/my-project
dbx init
```

Reads your git remote, detects the project type, and registers it on the server.

### 4. Push your env vars

```sh
dbx push
```

Reads your `.env.local` (or `.env`), encrypts it, and uploads to the server.

### 5. Pull from anywhere

```sh
dbx pull
```

Downloads, decrypts, and writes the env file. Works from any clone of the repo.

## Cloud Environments (Claude Code, Codex, CI)

For environments where you don't have your SSH key, use a Personal Access Token:

```sh
# On your laptop (authenticated via SSH):
dbx token create --name "claude-code" --scope project:read --ttl 90d
# Output: dbx_pat_x9k2m7f3...
```

Set `DEVBOX_TOKEN` in your cloud environment's settings. Then `dbx pull` works automatically — no SSH key needed.

### One-off access (CI, ephemeral)

```sh
# Create a single-use token:
dbx token create --name "ci-deploy" --type provision --project-id proj_abc --ttl 1h
# Output: dbx_prov_a8f3e2...
```

Provision tokens burn after one use.

## Commands

```
dbx init [--name <name>]              Register project from git remote
dbx push [--force] [--env-file <f>] [--project <name>] Upload env file to server
dbx pull [--force] [--diff] [--backup] [--cached] [--project <name>]
                                       Download env file from server
dbx diff [--project <name>]           Show changes without writing

dbx auth login --server <url>         Register SSH key with server
dbx auth status                       Show auth state
dbx auth logout                       Clear local auth

dbx token create --name <n> [--type pat|provision] [--scope <s>] [--ttl <d>]
dbx token list [--format json]        List active tokens
dbx token revoke <name>               Revoke a token

dbx project list [--format json]      List all projects
dbx project delete <name> [--yes]     Delete a project

dbx whoami [--format json]            Show current identity
dbx completion {bash,zsh,fish}        Shell completions
```

### Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | General error |
| 2 | Authentication error |
| 3 | Resource not found |

## Env File Detection

The CLI auto-detects which file to write based on your project:

| Project type | Marker file | Env file written |
|---|---|---|
| Node.js / TypeScript | `package.json`, `tsconfig.json` | `.env.local` |
| Next.js / Nuxt | `next.config.*`, `nuxt.config.*` | `.env.local` |
| Python | `pyproject.toml`, `requirements.txt` | `.env` |
| Go | `go.mod` | `.env` |
| Rust | `Cargo.toml` | `.env` |
| Ruby | `Gemfile` | `.env` |
| PHP | `composer.json` | `.env` |
| Unknown | — | `.env` |

Override with `--env-file`:

```sh
dbx pull --env-file .env.local    # Force .env.local regardless of project type
```

## Authentication

`dbx` resolves auth automatically in this order:

| Priority | Method | When to use |
|---|---|---|
| 1 | `DEVBOX_TOKEN` env var | Cloud environments, CI |
| 2 | SSH key | Local machines |

**SSH key discovery order:** ssh-agent, then `~/.ssh/id_ed25519`, `id_ed25519_sk`, `id_ecdsa`, `id_rsa`.

### Token types

| Type | Prefix | Lifespan | Use case |
|---|---|---|---|
| PAT | `dbx_pat_` | Up to 365 days (default 90d) | Cloud environments |
| Provision | `dbx_prov_` | Single-use, time-limited | CI, one-off access |

## Server Administration

### Backup

```sh
dbx-server backup --data ./dbx.db --output ./backup.db
```

### Emergency: Revoke all sessions and tokens

```sh
dbx-server emergency-revoke-all --data ./dbx.db
```

### Server flags

```
--data <path>       SQLite database path (default: ./dbx.db)
--age-key <path>    Age encryption key path (default: ./age.key)
--listen <addr>     Listen address (default: 127.0.0.1:8443)
--tls-cert <path>   TLS certificate
--tls-key <path>    TLS private key
--no-tls            Run without TLS (for local dev or behind a reverse proxy)
--allow-root        Allow running as root
```

### Deployment options

| Option | Setup | Cost |
|---|---|---|
| VPS (Hetzner, DigitalOcean) | Binary + systemd + Caddy | ~$5/mo |
| Fly.io | Docker + persistent volume | Free tier possible |
| Docker | `docker run ghcr.io/gedeonisezerano/dbx-server` | Anywhere |
| Raspberry Pi | Binary + systemd | $0 |

## Security

- Env vars encrypted at rest with [age](https://age-encryption.org/) (v1.3+, post-quantum)
- Per-project encryption keys derived via HKDF-SHA256
- SSH challenge-response auth with namespace domain separation (`devbox-auth@v1`)
- Nonces are single-use with 60s TTL
- Tokens are SHA-256 hashed in the database (generated with 256-bit entropy from `crypto/rand`)
- Session tokens are server-side (revocable), 15-minute expiry
- Rate limiting on auth endpoints (10/min per IP)
- SQLite with WAL mode, `secure_delete=ON`, file permissions `0600`
- Env files written with `0600` permissions
- All audit events logged (auth, push, pull, token create/revoke)

**Threat model:** Server-side encryption. The server operator is trusted. Designed for single-developer or small-team self-hosted deployments where you control the server.

## Configuration

**Client config** (`~/.config/dbx/config.toml`):

```toml
server = "https://dbx.example.com"
ssh_key = "~/.ssh/id_ed25519"    # optional, auto-detected
tls_ca = "/path/to/ca.pem"      # optional, for self-signed certs
```

**Local cache** (`.dbx/cache/` in your repo, gitignored):
- Cached env blob for offline fallback (`dbx pull --cached`)
- Version tracking for optimistic locking on push

## Development

```sh
git clone https://github.com/GedeonIsezerano/Devbox.git
cd devbox
make build        # Build both binaries to ./bin/
make test         # Run all 155 tests with race detection
make lint         # Run go vet
```

## Troubleshooting

### Rate limited (429)
Auth endpoints are limited to 10 requests/minute per IP. Wait 60 seconds and retry.
Sessions are cached locally after first auth, so this typically only happens during setup.

### "No SSH key found"
dbx checks: ssh-agent, then ~/.ssh/id_ed25519, id_ed25519_sk, id_ecdsa, id_rsa.
Generate one with: `ssh-keygen -t ed25519`

### "No server configured"
Run `dbx auth login --server <url>` first. The server URL is saved to ~/.config/dbx/config.toml.

### Session expired
Sessions last 15 minutes. dbx automatically re-authenticates when a cached session expires.
If using DEVBOX_TOKEN, the token itself may have expired -- create a new one.

### Version conflict on push (409)
Someone else pushed since your last pull. Run `dbx pull` first, then `dbx push`.
Use `dbx push --force` to overwrite (use with caution).

### .env.local not in .gitignore
dbx warns about this. Add your env file to .gitignore to prevent accidental commits:
```sh
echo ".env.local" >> .gitignore
```

## License

MIT
