---
name: dbx
description: >
  Set up environment variables for a project using dbx (Devbox CLI).
  Use this skill whenever: a new git worktree is created, a cloud environment
  is initialized, the user mentions env vars or .env files, you notice a project
  references environment variables but has no .env.local or .env file, the user
  asks to "pull env vars", "set up the environment", or mentions dbx/devbox.
  Also trigger when you see process.env references in code but no env file exists,
  or when a .superset/config.json setup script references dbx.
  This skill handles the full flow: installing dbx, setting up a server if needed,
  authenticating, and pulling env vars — every time, in every environment.
---

# dbx — Environment Variable Setup

Pull your project's environment variables from a self-hosted Devbox server into any environment: local worktrees, Claude Code cloud, Codex, or CI.

**Repository:** https://github.com/GedeonIsezerano/Devbox
**Docs:** https://github.com/GedeonIsezerano/Devbox#readme

## When This Runs

This skill should run whenever:
- A new **git worktree** is created
- A **cloud environment** starts (Claude Code web, Codex)
- The user asks to **set up env vars**, pull `.env`, or mentions `dbx`
- You notice the project **references env vars** (e.g., `process.env.DATABASE_URL`, `os.environ`) but there's no `.env.local` or `.env` file
- A setup script (e.g., `.superset/config.json`) references `dbx`

## The Flow

Always run all steps in order. Don't skip any — the checks are fast and confirming state is better than assuming.

### Step 1: Install dbx

```bash
which dbx || (curl -fsSL https://raw.githubusercontent.com/GedeonIsezerano/Devbox/main/install.sh | sh)
```

If it installs to `~/.local/bin/` and that's not in PATH:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

Verify:

```bash
dbx --version
```

### Step 2: Check for a server

```bash
dbx auth status
```

There are three scenarios. Follow the one that matches.

---

#### Scenario A: Already authenticated

The output shows a server URL and auth method (e.g., `Server: https://dbx.example.com`). Skip to **Step 3**.

#### Scenario B: DEVBOX_TOKEN is set

If the `DEVBOX_TOKEN` environment variable is present, `dbx` uses it automatically. Skip to **Step 3**.

#### Scenario C: No server configured

The output shows `(not configured)`. The user needs a Devbox server. Ask them:

> "You need a Devbox server to store your env vars. Do you already have one running, or should we set one up?"

**If they have a server URL:**

```bash
dbx auth login --server <their-url>
```

**If they need to set one up**, walk them through one of these options (recommend the simplest one that fits):

**Option 1: Local server (recommended if you only work on one machine)**

If all your worktrees are on the same machine, this is all you need — no domain, no TLS, no cloud setup. Just start the server and leave it running.

```bash
# Install the server binary
curl -fsSL https://raw.githubusercontent.com/GedeonIsezerano/Devbox/main/install.sh | sh
# Create data directory
mkdir -p ~/.dbx-server
# Start it (runs on localhost, no TLS needed)
dbx-server serve --data ~/.dbx-server/data.db --age-key ~/.dbx-server/age.key --listen 127.0.0.1:8443 --no-tls &
# Log in once
dbx auth login --server http://127.0.0.1:8443
```

From then on, `dbx pull` works from any worktree on this machine. The user can add the server to their shell startup (`.zshrc` / `.bashrc`) or create a launchd/systemd service so it starts automatically.

This only works from the same machine. For cloud environments or multiple machines, they need Option 2 or 3.

**Option 2: Expose local server with ngrok (quick, no VPS needed)**

Good for trying dbx across machines without setting up a VPS:

```bash
# Start the server locally
dbx-server serve --data ~/.dbx-server/data.db --age-key ~/.dbx-server/age.key --listen 127.0.0.1:8443 --no-tls &
# Expose it via ngrok (install ngrok first: https://ngrok.com)
ngrok http 8443
# ngrok gives you a URL like https://abc123.ngrok-free.app
# Log in using that URL
dbx auth login --server https://abc123.ngrok-free.app
```

Note: ngrok URLs change every time you restart unless you have a paid plan. For a permanent setup, use Option 3.

**Option 3: VPS (recommended for production)**

The most reliable setup. A $5/month VPS gives you a permanent, always-on server.

1. **Get a VPS** — Hetzner, DigitalOcean, or Fly.io all work. Any Linux box with a public IP.

2. **Install and start the server:**

```bash
# On the VPS:
curl -fsSL https://raw.githubusercontent.com/GedeonIsezerano/Devbox/main/install.sh | sh
# Create a directory for data
mkdir -p /opt/dbx
# Start with a reverse proxy in front (Caddy handles TLS automatically)
dbx-server serve --data /opt/dbx/data.db --age-key /opt/dbx/age.key --listen 127.0.0.1:8443 --no-tls
```

3. **Set up Caddy as a reverse proxy** (auto-TLS with Let's Encrypt):

```
# /etc/caddy/Caddyfile
dbx.yourdomain.com {
    reverse_proxy 127.0.0.1:8443
}
```

4. **Create a systemd service** so the server starts on boot:

```ini
# /etc/systemd/system/dbx-server.service
[Unit]
Description=Devbox Server
After=network.target

[Service]
Type=simple
User=dbx
ExecStart=/usr/local/bin/dbx-server serve --data /opt/dbx/data.db --age-key /opt/dbx/age.key --listen 127.0.0.1:8443 --no-tls
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable --now dbx-server
```

5. **Log in from your local machine:**

```bash
dbx auth login --server https://dbx.yourdomain.com
```

**Option 4: Docker**

```bash
docker run -d \
  --name dbx-server \
  -p 8443:8443 \
  -v dbx-data:/data \
  ghcr.io/gedeonisezerano/dbx-server \
  serve --data /data/dbx.db --age-key /data/age.key --listen 0.0.0.0:8443 --no-tls
```

Put Caddy/nginx with TLS in front for production.

---

**After the server is running and the user has logged in**, they need to register their project and push their env vars before they can pull from other environments:

```bash
cd /path/to/project
dbx init
dbx push
```

Then create a token for cloud environments:

```bash
dbx token create --name "cloud" --scope project:read --ttl 90d
# Output: dbx_pat_abc123...
# Set DEVBOX_TOKEN=dbx_pat_abc123... in cloud environment settings
```

### Step 3: Pull environment variables

```bash
dbx pull --force
```

`--force` is used in automated setups (worktrees, cloud init) since there's no interactive terminal. `dbx` auto-detects:
- **Which project** — from the git remote URL
- **Which file to write** — `.env.local` for Node/TS, `.env` for Python/Go/Rust

Verify:

```bash
ls -la .env.local .env 2>/dev/null
```

**If pull fails with "not found" (exit code 3):**

The project hasn't been registered. Tell the user:

> "This project isn't registered on your Devbox server yet. From a machine where you're logged in, run:
> ```
> cd /path/to/project
> dbx init
> dbx push
> ```
> Then try `dbx pull` again."

**If pull fails with "connection refused":**

The server isn't running or the URL is wrong. Check:
1. Is the server process running? (`ps aux | grep dbx-server`)
2. Is the URL correct? (`dbx auth status` shows the configured URL)
3. Can you reach it? (`curl <server-url>/health`)

## Troubleshooting

| Error | Cause | Fix |
|---|---|---|
| `command not found: dbx` | Not installed | `curl -fsSL https://raw.githubusercontent.com/GedeonIsezerano/Devbox/main/install.sh \| sh` |
| `no server configured` | No server set up | Follow Step 2 Scenario C above |
| `connection refused` | Server not running or wrong URL | Check server process and URL |
| `rate limit exceeded (429)` | Too many auth attempts/min | Wait 60 seconds. Sessions are cached after first auth. |
| `not found (404)` | Project not registered | Run `dbx init && dbx push` from an authenticated machine |
| `version conflict (409)` | Stale local version | Run `dbx pull` first, then `dbx push` |
| `authentication failed` | Expired token or invalid key | Create a new token or re-run `dbx auth login` |

## Quick Reference

```
dbx auth login --server <url>   # authenticate with SSH key
dbx auth status                 # check authentication
dbx init                        # register project from git remote
dbx push                        # upload env file to server
dbx pull                        # download env file from server
dbx pull --force                # overwrite without prompting
dbx token create --name <n>     # create a PAT for cloud environments
dbx whoami                      # show current identity
```
