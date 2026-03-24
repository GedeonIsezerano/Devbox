CREATE TABLE users (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    is_admin    INTEGER NOT NULL DEFAULT 0,
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
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    remote_url  TEXT UNIQUE,
    env_file    TEXT NOT NULL DEFAULT '.env',
    owner_id    TEXT NOT NULL REFERENCES users(id),
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE project_members (
    project_id  TEXT NOT NULL REFERENCES projects(id),
    user_id     TEXT NOT NULL REFERENCES users(id),
    role        TEXT NOT NULL DEFAULT 'reader',
    PRIMARY KEY (project_id, user_id)
);

CREATE TABLE env_vars (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects(id),
    environment TEXT NOT NULL DEFAULT 'default',
    blob        BLOB NOT NULL,
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
    hash        TEXT NOT NULL UNIQUE,
    type        TEXT NOT NULL,
    scope       TEXT NOT NULL,
    expires_at  TEXT,
    single_use  INTEGER NOT NULL DEFAULT 0,
    last_used   TEXT,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE sessions (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id),
    token_hash  TEXT NOT NULL UNIQUE,
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
    action      TEXT NOT NULL,
    metadata    TEXT,
    ip_address  TEXT,
    user_agent  TEXT
);
