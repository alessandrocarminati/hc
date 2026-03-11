-- -----------------------------------------------------
-- tenants
-- -----------------------------------------------------
CREATE TABLE tenants (
    id TEXT NOT NULL,
    name TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE (id),
    UNIQUE (name)
);

-- -----------------------------------------------------
-- app_users
-- -----------------------------------------------------
CREATE TABLE app_users (
    id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    username TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, username),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

-- -----------------------------------------------------
-- api_keys
-- -----------------------------------------------------
CREATE TABLE api_keys (
    id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    user_id TEXT,
    key_id TEXT NOT NULL,
    key_hash TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    revoked_at TEXT,
    PRIMARY KEY (id),
    UNIQUE (tenant_id, key_id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (user_id) REFERENCES app_users(id)
);

-- -----------------------------------------------------
-- cmd_events
-- -----------------------------------------------------
CREATE TABLE cmd_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    seq INTEGER NOT NULL,
    tenant_id TEXT NOT NULL,
    ts_client TEXT,
    session_id TEXT NOT NULL,
    host_fqdn TEXT NOT NULL,
    cwd TEXT,
    cmd TEXT,
    ts_ingested TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    src_ip TEXT,
    transport TEXT NOT NULL DEFAULT 'tcp-clear',
    parse_ok INTEGER NOT NULL DEFAULT 1,
    raw_line TEXT NOT NULL,
    UNIQUE (tenant_id, seq),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);

-- -----------------------------------------------------
-- cmd_event_tags
-- -----------------------------------------------------
CREATE TABLE cmd_event_tags (
    tenant_id TEXT NOT NULL,
    event_id INTEGER NOT NULL,
    tag TEXT NOT NULL,
    PRIMARY KEY (tenant_id, event_id, tag),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (event_id) REFERENCES cmd_events(id) ON DELETE CASCADE
);

-- -----------------------------------------------------
-- indexes
-- -----------------------------------------------------
CREATE INDEX cmd_events_tenant_id_id_desc
    ON cmd_events (tenant_id, id DESC);

-- Optional helper indexes if you query these often
CREATE INDEX api_keys_tenant_id_idx
    ON api_keys (tenant_id);

CREATE INDEX app_users_tenant_id_idx
    ON app_users (tenant_id);

CREATE INDEX cmd_event_tags_event_id_idx
    ON cmd_event_tags (event_id);
