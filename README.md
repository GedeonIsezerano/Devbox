<p align="center">
  <h1 align="center">Devbox</h1>
  <p align="center">Self-hosted environment variable management for developers.</p>
  <p align="center">
    <a href="#install">Install</a> &middot;
    <a href="#quick-start">Quick Start</a> &middot;
    <a href="#cloud-environments">Cloud</a> &middot;
    <a href="#commands">Commands</a> &middot;
    <a href="#security">Security</a>
  </p>
</p>

---

Push and pull `.env` files across local worktrees, cloud environments, and CI — with one command.

```
$ dbx pull
Pulling "my-project" (github.com/user/my-project) from https://dbx.example.com
Detected Node.js project -> writing .env.local
Wrote .env.local (14 variables, 1.2 KB)
```

### The problem

Every new worktree or cloud environment needs your env vars. Locally you copy `.env.local`. In the cloud, there's nothing to copy from. Existing tools are either SaaS-only (Doppler, 1Password), require heavy infrastructure (Infisical, Phase), or lack a push/pull model (SOPS).

### The solution

Devbox gives you a single `dbx pull` that works everywhere — self-hosted, encrypted, and zero-config after initial setup.

- **One command** to pull env vars into any environment
- **Auto-detects** the right file (`.env.local` for Node/TS, `.env` for Python/Go/Rust)
- **SSH key auth** locally, **PAT tokens** for cloud
- **Self-hosted** — secrets never leave your infrastructure
- **Encrypted at rest** with [age](https://age-encryption.org/) (post-quantum)

---

## Install

### CLI (`dbx`)

**Linux / macOS / Cloud:**

```sh
curl -fsSL https://raw.githubusercontent.com/GedeonIsezerano/Devbox/main/install.sh | sh
```

**Homebrew:**

```sh
brew install GedeonIsezerano/tap/dbx
```

**Go:**

```sh
go install github.com/GedeonIsezerano/Devbox/cmd/dbx@latest
```

### Server (`dbx-server`)

**Docker:**

```sh
docker run -d \
  --name dbx-server \
  -p 8443:8443 \
  -v dbx-data:/data \
  ghcr.io/gedeonisezerano/dbx-server \
  serve --data /data/dbx.db --age-key /data/age.key --listen 0.0.0.0:8443 --no-tls
```

> Put a reverse proxy (Caddy, nginx) with TLS in front for production.

**From source:**

```sh
git clone https://github.com/GedeonIsezerano/Devbox.git
cd Devbox
make build
./bin/dbx-server serve --data ./dbx.db --age-key ./age.key --no-tls
```

The server generates an encryption key on first start if one doesn't exist.

---

## Quick Start

```sh
# 1. Start the server
dbx-server serve --data ./dbx.db --age-key ./age.key --listen 127.0.0.1:8443 --no-tls

# 2. Log in (registers your SSH key — first user becomes admin)
dbx auth login --server http://localhost:8443

# 3. Initialize your project
cd ~/Code/my-project
dbx init

# 4. Push your env vars
dbx push

# 5. Pull from anywhere (new worktree, cloud, CI)
dbx pull
```

---

## Cloud Environments

For Claude Code, Codex, CI, or any environment without your SSH key:

**Create a token on your laptop:**

```sh
dbx token create --name "claude-code" --scope project:read --ttl 90d
# => dbx_pat_x9k2m7f3...
```

**Set it in the cloud environment's settings:**

```
DEVBOX_TOKEN=dbx_pat_x9k2m7f3...
```

Now `dbx pull` works automatically — no SSH key needed.

**Single-use tokens for CI:**

```sh
dbx token create --name "ci-deploy" --type provision --project-id proj_abc --ttl 1h
# => dbx_prov_a8f3e2... (burns after one use)
```

### AI Agent Setup (Claude Code, Codex, etc.)

Add the install to your project's setup script, set `DEVBOX_TOKEN` as a persistent env var, and add a line to your `CLAUDE.md` / `AGENTS.md`:

```markdown
# CLAUDE.md
If .env.local or .env is missing, run `dbx pull --force` to fetch environment variables.
```

That's it — when the agent starts a worktree or cloud session, it installs `dbx`, authenticates via the token, and pulls your env vars automatically.

---

## Commands

| Command | Description |
|---|---|
| `dbx init [--name <name>]` | Register project from git remote |
| `dbx push [--force] [--env-file <f>] [--project <name>]` | Upload env file to server |
| `dbx pull [--force] [--diff] [--backup] [--cached] [--project <name>]` | Download env file from server |
| `dbx diff [--project <name>]` | Show changes without writing |
| `dbx auth login --server <url>` | Register SSH key with server |
| `dbx auth status` | Show auth state |
| `dbx auth logout` | Clear local auth |
| `dbx token create --name <n> [--type] [--scope] [--ttl]` | Create API token |
| `dbx token list [--format json]` | List tokens |
| `dbx token revoke <name>` | Revoke a token |
| `dbx project list [--format json]` | List projects |
| `dbx project delete <name> [--yes]` | Delete a project |
| `dbx whoami [--format json]` | Show current identity |
| `dbx completion {bash,zsh,fish}` | Shell completions |

### Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | General error |
| 2 | Authentication error |
| 3 | Not found |

---

## Env File Detection

`dbx` auto-detects which file to write:

| Project | Marker | File |
|---|---|---|
| Node.js / TypeScript | `package.json`, `tsconfig.json` | `.env.local` |
| Next.js / Nuxt | `next.config.*`, `nuxt.config.*` | `.env.local` |
| Python | `pyproject.toml`, `requirements.txt` | `.env` |
| Go | `go.mod` | `.env` |
| Rust | `Cargo.toml` | `.env` |
| Ruby | `Gemfile` | `.env` |
| PHP | `composer.json` | `.env` |
| Unknown | — | `.env` |

Override: `dbx pull --env-file .env.local`

---

## Authentication

Auth resolves automatically:

| Priority | Method | Use case |
|---|---|---|
| 1 | `DEVBOX_TOKEN` env var | Cloud, CI |
| 2 | Cached session | Repeat commands within 15 min |
| 3 | SSH key | Local machines |

**SSH key discovery:** ssh-agent, then `~/.ssh/id_ed25519`, `id_ed25519_sk`, `id_ecdsa`, `id_rsa`.

**Token types:**

| Type | Prefix | Lifespan | Use case |
|---|---|---|---|
| PAT | `dbx_pat_` | Up to 365d (default 90d) | Cloud environments |
| Provision | `dbx_prov_` | Single-use | CI, one-off access |

---

## Server Administration

```sh
# Backup
dbx-server backup --data ./dbx.db --output ./backup.db

# Emergency: revoke all sessions and tokens
dbx-server emergency-revoke-all --data ./dbx.db
```

**Server flags:**

| Flag | Default | Description |
|---|---|---|
| `--data` | `./dbx.db` | SQLite database path |
| `--age-key` | `./age.key` | Age encryption key path |
| `--listen` | `127.0.0.1:8443` | Listen address |
| `--tls-cert` | — | TLS certificate |
| `--tls-key` | — | TLS private key |
| `--no-tls` | `false` | Run without TLS (local dev / reverse proxy) |
| `--allow-root` | `false` | Allow running as root |

**Deployment:**

| Option | Setup | Cost |
|---|---|---|
| VPS (Hetzner, DO) | Binary + systemd + Caddy | ~$5/mo |
| Fly.io | Docker + persistent volume | Free tier |
| Docker | `docker run ghcr.io/gedeonisezerano/dbx-server` | Any host |
| Raspberry Pi | Binary + systemd | $0 |

---

## Security

- Encrypted at rest with [age](https://age-encryption.org/) v1.3+ (post-quantum)
- Per-project keys via HKDF-SHA256
- SSH challenge-response with domain separation (`devbox-auth@v1`)
- Single-use nonces (60s TTL)
- Tokens hashed with SHA-256 (256-bit entropy, `crypto/rand`)
- Server-side sessions (revocable, 15-min expiry)
- Rate limiting on auth endpoints (10/min per IP)
- HTTP server timeouts (slowloris protection)
- SQLite WAL mode, `secure_delete=ON`, `0600` file permissions
- Env files written with `0600` permissions
- Full audit trail (auth, push, pull, token operations)

**Threat model:** Server-side encryption — the server operator is trusted. Designed for single-developer or small-team self-hosted use.

---

## Configuration

**Client** (`~/.config/dbx/config.toml`):

```toml
server = "https://dbx.example.com"
ssh_key = "~/.ssh/id_ed25519"   # optional, auto-detected
tls_ca = "/path/to/ca.pem"     # optional, for self-signed certs
```

**Cache** (`.dbx/cache/` per repo, gitignored):
- Offline fallback (`dbx pull --cached`)
- Version tracking for optimistic locking

---

## Troubleshooting

**Rate limited (429)** — Auth is limited to 10 req/min per IP. Wait 60s. Sessions are cached after first auth.

**"No SSH key found"** — Generate one: `ssh-keygen -t ed25519`

**"No server configured"** — Run `dbx auth login --server <url>` first.

**Session expired** — Auto-re-authenticates. If using `DEVBOX_TOKEN`, the token may have expired.

**Version conflict (409)** — Run `dbx pull` first, then `dbx push`. Or `dbx push --force`.

**`.env.local` not in `.gitignore`** — `echo ".env.local" >> .gitignore`

---

## Development

```sh
git clone https://github.com/GedeonIsezerano/Devbox.git
cd Devbox
make build    # Build both binaries to ./bin/
make test     # Run all 155 tests with race detection
make lint     # Run go vet
```

---

## License

MIT License. See [LICENSE](LICENSE) for details.
