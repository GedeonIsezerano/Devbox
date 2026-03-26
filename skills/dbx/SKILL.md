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
  This skill handles the full flow: installing dbx, authenticating, and pulling
  env vars — every time, in every environment.
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

Always run all three steps, in order. Don't skip any step even if you think it's already done — the checks are fast and confirming state is better than assuming.

### Step 1: Ensure dbx is installed

Check if `dbx` is on the PATH:

```bash
which dbx
```

If not found, install it:

```bash
curl -fsSL https://raw.githubusercontent.com/GedeonIsezerano/Devbox/main/install.sh | sh
```

After install, verify it worked:

```bash
dbx --version
```

If `dbx` installs to `~/.local/bin/` and that's not in PATH, add it for the current session:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

### Step 2: Ensure authenticated

Check auth status:

```bash
dbx auth status
```

There are three possible outcomes:

**Already authenticated** — the output shows a server URL and auth method. Proceed to Step 3.

**DEVBOX_TOKEN is set** — if the `DEVBOX_TOKEN` environment variable is present, `dbx` will use it automatically. No further action needed. Proceed to Step 3.

**Not authenticated** — the output shows "not configured" or no server URL. You need to help the user authenticate:

1. Ask the user for their Devbox server URL
2. If they're on a local machine with SSH keys:
   ```bash
   dbx auth login --server <url>
   ```
3. If they're in a cloud environment without SSH keys, tell them:
   > "You need a Personal Access Token. On a machine where you're already logged into dbx, run:
   > ```
   > dbx token create --name "cloud-env" --scope project:read --ttl 90d
   > ```
   > Then set `DEVBOX_TOKEN=<the token>` in your environment settings."

Do not proceed to Step 3 until auth is confirmed. Run `dbx auth status` again to verify.

### Step 3: Pull environment variables

Pull the env vars for the current project:

```bash
dbx pull --force
```

The `--force` flag is used because in automated setups (worktree creation, cloud init), there's no interactive terminal to confirm overwrites. `dbx` auto-detects:
- **Which project** — from the git remote URL
- **Which file to write** — `.env.local` for Node/TS, `.env` for Python/Go/Rust (based on project markers)

After pulling, verify the file was created:

```bash
ls -la .env.local .env 2>/dev/null
```

If the pull fails with "not found" (exit code 3), the project hasn't been registered on the server yet. Tell the user:

> "This project isn't registered on your Devbox server yet. On a machine where you're logged in, run:
> ```
> cd /path/to/project
> dbx init
> dbx push
> ```
> Then try `dbx pull` again in this environment."

## Troubleshooting

| Error | Cause | Fix |
|---|---|---|
| `command not found: dbx` | Not installed | Run the curl installer (Step 1) |
| `no server configured` | Not authenticated | Run `dbx auth login` or set `DEVBOX_TOKEN` |
| `rate limit exceeded (429)` | Too many auth attempts in 1 minute | Wait 60 seconds, retry. Sessions are cached after first auth. |
| `not found (404)` | Project not registered on server | Run `dbx init && dbx push` from an authenticated machine |
| `version conflict (409)` | Someone pushed since your last pull | Run `dbx pull` first to get latest, then `dbx push` |
| `authentication failed` | Expired token or invalid SSH key | Create a new token or re-run `dbx auth login` |

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
