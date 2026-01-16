# HC: History Collector
**The "Boring" Shell History Sink for Distributed Fleets.**

`hc` is a lightweight service that centralizes shell history from across
your infrastructure into a PostgreSQL backend. 

### Why hc?
If you manage 50+ servers, your shell history is fragmented. Finding
"that one command that worked" usually involves a dozen SSH sessions and
a lot of grep. 

`hc` solves this by providing a centralized, multi-tenant sink that is:
* **Agentless:** Only requires `curl`, `wget`, or `socat` on the client.
   No binaries to install on production nodes.
* **SSL-Native:** Secure ingestion via TLS with support for Client 
  Certificate and API Key authentication.
* **Grep-Friendly:** Fetch your history as plaintext via `HTTPS` and 
  pipe it directly into your local tools.
* **Multi-tenant:** Isolate history by team or environment using API 
  keys.

More on this story on this [article](https://carminatialessandro.blogspot.com/2026/01/hc-agentless-multi-tenant-shell-history.html) from my blog.

## Architecture Overview

```
(shell) -- TCP / TLS --> hc
                         |
                         +-- spool file (append-only, per-tenant)
                         |
                         +-- PostgreSQL (authoritative storage)
                                |
                                +-- HTTP(S) export (text)
```

Key points:

* The **database is authoritative**
* The **spool file** exists as a safety net (temporary DB outages,
  restart recovery)
* No in-memory history representation is kept
* All commands are preserved (no deduplication)

### Ingestion

* **Plain TCP**: trusted network only as the command is cleartext
* **TLS (SSL)**: recommended, your data is protected and you can
  authenticate the server using SSL
* BusyBox / ash supported
* Bash supported

### Export

* **HTTP**: unauthenticated and clear text, can be CIDR-limited. The API key
  can be used, but it is planned to be removed.
* **HTTPS**: Ciphered authentication-capable using API key or /Client side
  certificate (support in progress)
* Output is **plain text**, ANSI colored and optimized for `grep`

## Command Format (Ingestion Line)

Each ingested command must be sent as **one single line**:
```
YYYYMMDD.HHMMSS - SESSIONID - host.example.com [cwd=/path] > command args...
```
Example
```
20240101.120305 - a1b2c3d4 - host.example.com [cwd=/root] > ls -la
```

### Session ID

The session ID:
* groups commands belonging to the same shell session
* is generated client-side
* is not guaranteed globally unique, but collisions are very unlikely

## API Keys

`hc` supports **API key basd tenant resolution**.

An API key:
* identifies a **tenant**
* is embedded directly into the ingested command line
* is **removed before storage** (never stored in DB or spool)

### API key format
```
<key_id>.<secret>
```
Example:
```
hc_9f3a1c2d.QmFzZTY0U2VjcmV0U3RyaW5n
```
### Embedding API keys in ingestion lines

The API key **must appear at the beginning of the command payload**, wrapped
like this:
```
]apikey[<key_id>.<secret>] command...
```

Example:
```
]apikey[hc_9f3a1c2d.QmFzZTY0U2VjcmV0] make build
```

Notes:
* The API key is used **only for authentication**
* It is **stripped from the command** before storing
* If authentication fails, falls back to the next authentication method.
  If none succeeds, the line is dropped

## Creating an API Key

Use the `api_key` verb on the server:
```
./hc.app apy_key \
        -config hc-config.json \
        -loglevel info \
        -api_tenantid 11111111-1111-1111-1111-111111111111 \
        -api_userid 00000000-0000-0000-0000-000000000001
```
Output example:
```
tenant_id: 11111111-1111-1111-1111-111111111111
key_id:    hc_9f3a1c2d
api_key:   hc_9f3a1c2d.QmFzZTY0U2VjcmV0U3RyaW5n
note: api_key is shown only now; store it safely.
```
**Notes:**
* The secret is **never shown again**.
* The current state does not allows to create tenantIDs or userIDs.
  as for v0.3 the operation is still manual on the db

## Client Setup (Bash)

### Basic (plain TCP, no API key)

```
export SESSION_ID_HC=$(date +%Y%m%d.%H%M%S | sha1sum | cut -c1-8)
export PROMPT_COMMAND='echo "$(date +%Y%m%d.%H%M%S) - ${SESSION_ID_HC} - $(hostname --fqdn) [cwd=$(pwd)] > $(history -w /dev/stdout | tail -n1)" | nc hc.example.com 12345'
```
### TLS ingestion with API key (recommended)
```
export SESSION_ID_HC=$(date +%Y%m%d.%H%M%S | sha1sum | cut -c1-8)
export APIKEY_HC="hc_9f3a1c2d.QmFzZTY0U2VjcmV0U3RyaW5n"

export PROMPT_COMMAND='echo "$(date +%Y%m%d.%H%M%S) - ${SESSION_ID_HC} - $(hostname --fqdn) [cwd=$(pwd)] > ]apikey[${APIKEY_HC}] $(history -w /dev/stdout | tail -n1)" | socat - OPENSSL:hc.example.com:1235,verify=0'
```

Notes:
* `socat` is used instead of `nc` to support TLS
* BusyBox `ash` users may need different hooks (see blog [link](https://carminatialessandro.blogspot.com/2025/06/logging-shell-commands-in-busybox-yes.html) )

## Fetching History (Text Export)

History is fetched as **plain text**, designed to be piped to `grep`.

Examples:
* http, default tenant
```
wget "http://hc.example.com:8080/export?grep1=qemu&grep2=aarch64&grep3=centos&session=8f7f1b24&color=always" -O -
```
* https, API Key
```
wget --header="Authorization: Bearer hc_9f3a1c2d.QmFzZTY0U2VjcmV0U3RyaW5n"  "https://hc.example.com:8443/export?grep1=make&grep2=test&color=always" -O - -q

```
* https, client certificate
```
wget --certificate=client.example.com.crt --private-key=client.example.com.key "https://hc.example.com:8443/export?grep1=make&grep2=test&color=always" -O - -q
```

### Supported query parameters

* `grep1`, `grep2`, `grep3`: regex filters (ordered)
* `session`: restrict to a specific session ID
* `color=always|never|auto` ANSI color text
* `limit`
* `order=asc|desc` (ingestion order)

Output format mirrors the ingestion format for familiarity.

## Configuration Highlights
* Ingestion listeners: plain TCP + TLS
* Export over HTTP / HTTPS
* Authentication is **pluggable and ordered**
* Tenants, ACLs, and auth are clearly separated
(Postgres is currently the primary backend; SQLite support is planned as
a lightweight alternative.)

## Configuration File (`hc-config.json`)
`hc` is configured using a single JSON configuration file.
The configuration defines listeners, authentication behavior, database
backend, and operational limits.

At a high level:
* `server` defines all exposed services:
    * `listner_clear` / `listner_tls` control command ingestion over plain
      TCP and TLS.
    * `http` / `https` control text export endpoints.
    * Each service can be independently enabled and bound to a specific
      address.
    * Authentication methods (`auth`) are evaluated in order, and the first
      successful one assigns the tenant.
* `tenants` defines known tenants, their identifiers, and optional ACL rules.
* `db` specifies the database backend (currently PostgreSQL).
* `tls` specifies certificate and key files used by TLS ingestion and HTTPS
  export.
* `limits` defines safety limits (for example, maximum accepted line size).
* `Export` controls global export limits (maximum rows and execution time).

The configuration is intentionally explicit: authentication, authorization,
and transport are configured separately to keep the model understandable and
extensible. A full example configuration is provided in
`hc-config.json` and is meant to be copied and edited rather than generated.

## Database Quick Start (PostgreSQL)

`hc` uses PostgreSQL as its authoritative storage backend.
The schema is intentionally simple and append-only.

To initialize the database:
```
createdb history
psql history < pg_schema.sql
```

The schema (`pg_schema.sql`) creates:

* `tenants`: logical isolation units
* `cmd_events`: all ingested commands (authoritative history)
* `api_keys`: API keys used for authentication
* `app_users`: future-facing user metadata (not required for ingestion)

After schema creation, at least one tenant must exist. You can insert it
manually or let hc create it during bootstrap (depending on configuration).
API keys are generated using the api_key verb and stored hashed in the
database.

The database is designed so that:
* all ingested commands are preserved
* ingestion order is maintained via a sequence number
* old data from text only storage <v0.1.19 can be exported, filtered, and
  reprocessed safely

### Temporary `tenantID` and `userID` creation

Since there's still no function to create tenants and users it must fall
back to manual insertion into the database.

Example

```
insert into tenants values ('11111111-1111-1111-1111-111111111111', 'default');
insert into app_users (id, tenant_id, username, created_at) values ('00000000-0000-0000-0000-000000000001', '11111111-1111-1111-1111-111111111111', 'username', now());
```

## Handling Firewalls & NAT (The SSH Tunnel Method) Hack of the day

Sometimes your target server is behind a restrictive firewall or a VPN and
cannot "see" the hc collector directly.

In the [example.bash](example.bash), there is a bootstrap script that:

* Logs into the remote server via SSH.
* Sets up a Reverse SSH Tunnel so the remote server can "talk back" to 
  your collector through the established SSH connection.
* Installs the `PROMPT_COMMAND` hook automatically.

This allows you to collect history from servers that have zero inbound 
or outbound access to the collector's network, as long as you can SSH into
them from your workstation without leaving a single byte of configuration
on the remote host.

## Status (as for v0.3)

* [OK] Plain + TLS ingestion
* [OK] PostgreSQL storage
* [OK] API key authentication
* [OK] Text export over HTTP
* [OK] HTTPS export auth (client certs, API keys)
* [WIP] SQLite support
* [WIP] Web UI (optional, not a priority)

## Philosophy
`hc` is intentionally **not**:
* a SIEM
* a real-time analytics engine
* a web-heavy UI product

It is:
* a **history sink**
* optimized for **grep**
* reliable, inspectable, and boring

## License

This project is licensed under the GNU General Public License v3.0 (GPL-3.0).
See the LICENSE file.
