# HC - History Collector  

HC collects shell command history from remote machines and stores it centrally.
It's designed to be lightweight, easy to deploy, and easy to query with the same
"grep brain" workflow you already use.

HC is evolving from a text-file based collector into a database-backed service:

- **Ingestion:** accept command lines over **plain TCP** or **TLS** (server cert)
- **Storage:** persist events into **PostgreSQL**
- **Spool:** write a local per-tenant spool on disk to survive temporary DB issues
- **Fetch:** export history as **plain text over HTTP/HTTPS** with grep-like filters
- **Web UI:** work-in-progress (WIP). For now, export is the primary interface.

## Architecture  

### Data path (ingestion)  
This is the intended workflow:
1. Client sends a **single command event** as one newline-terminated record.
2. HC validates and parses the record.
3. HC assigns a **sequence number** (per-tenant) and:
    - appends it to the **spool** (disk)
    - inserts it into **PostgreSQL**
4. The DB is the source of truth. The spool is a durability/bridge mechanism.

### Why the spool exists  

The spool helps when:
- The DB is temporarily unavailable
- You want an append-only trail per tenant
- You need deterministic replay semantics (per-tenant seq)

#### Note  

Import-from-spool tooling is WIP; the spool format and flow are already in place.
The import tool from old history exist and is working under the subcommand `import`.
Example:
```
./hc.app import -config hc-config.json -historyFile history.txt
```

## Message format  

HC expects a single record per connection (or per TLS session), one line:
```
YYYYMMDD.HHMMSS - <session_id> - <host_fqdn> [cwd=<cwd>] > <command>
```
Notes:
- `session_id` is 8 hex chars (client-generated)
- `cwd` is optional but recommended
- `<command>` may include redirects and `>`; the parser anchors on the header portion

Example:
```
20260106.104512 - 8f7f1b24 - myhost.example [cwd=/root] > ls -la
```

## Server configuration

HC uses a JSON configuration file. Example (redacted):

```
{
    "server": {
        "listner_clear": {
            "enabled": true,
            "addr": "0.0.0.0:1234"
        },
        "listner_tls": {
            "enabled": false,
            "addr": "0.0.0.0:1235"
        },
        "http": {
            "enabled": true,
            "addr": "0.0.0.0:8080"
        },
        "https": {
            "enabled": false,
            "addr": "0.0.0.0:8443"
        }
    },
    "db": {
        "postgres_dsn": "host=... port=5432 user=... password=... dbname=history sslmode=disable"
    },
    "tenancy": {
        "default_tenant_id": "9b9b1a6e-3d3e-4b2a-8f2c-3a51b84e4a0a",
        "trusted_sources": [
            {
                "CIDR": "10.1.0.0/16",
                "tenant_id": "9b9b1a6e-3d3e-4b2a-8f2c-3a51b84e4a0a",
                "note": "default"
            },
            {
                "CIDR": "127.0.0.0/8",
                "tenant_id": "9b9b1a6e-3d3e-4b2a-8f2c-3a51b84e4a0a",
                "note": "loopback"
            }
        ]
    },
    "tls": {
        "cert_file": "cert.crt",
        "key_file": "cert.key"
    },
    "limits": {
        "max_line_bytes": 655536
    },
    "Export": {
        "Enabled": true,
        "MaxRows": 200000,
        "MaxSeconds": 30,
        "Unsecure": {
            "Enabled": true,
            "TenantID": "9b9b1a6e-3d3e-4b2a-8f2c-3a51b84e4a0a"
        },
        "SSL": {
            "Enabled": false,
            "TenantID": ""
        }
    }
}
```
## Notes  

- `listner_clear`: plaintext ingestion, intended only for trusted sources (CIDR mapping)
- `listner_tls`: TLS ingestion using `tls.cert_file` / `tls.key_file`
- `http` / `https`: export endpoints
- `Export.Unsecure`: export without auth (intended for controlled environments)
- `Export.SSL`: export over HTTPS (auth/mTLS planned)

## Build  
```  
make
```
Run the server with your config:
```
./hc.app  serve -config hc-config.json -loglevel info
```

## Ingestion (clients)  
In bash based system the setup can be as easy as add a few lines in the `.bashrc` script.
 
### Plain TCP ingestion  

Example:
```
HC_HOST="host.example.com"
HC_PORT=12345
export SESSIONID_=$(date +%Y%m%d.%H%M%S |sha1sum | sed -r 's/^(........).*/\1/')
export PROMPT_COMMAND='echo "$(date +%Y%m%d.%H%M%S) - ${SESSIONID_} - $(hostname --fqdn) [cwd=$(pwd)] > $(history -w /dev/stdout | tail -n1)"|nc ${HC_HOST} ${HC_PORT}'
```
### TLS ingestion (server cert)

Example (no verification in this example; see verify=0 for self signed simplicity):
```
HC_HOST="host.example.com"
HC_PORT=12346
export SESSIONID_=$(date +%Y%m%d.%H%M%S |sha1sum | sed -r 's/^(........).*/\1/')
export PROMPT_COMMAND='echo "$(date +%Y%m%d.%H%M%S) - ${SESSIONID_} - $(hostname --fqdn) [cwd=$(pwd)] > $(history -w /dev/stdout | tail -n1)"|socat - OPENSSL:${HC_HOST}:${HC_PORT},verify=0'
```

TLS ingestion is supported. Client certificate authentication (mTLS) is planned for the HTTPS export and/or TLS ingestion.

#### Note

For non **bash** systems things may vary.
An example on how to have a similar feature in `ash` the **busybox** shell can be seen in the [`busybox_support`](busybox_support) directory.

## Fetch / Export (plain text over HTTP/HTTPS)

HC exports history as text/plain. Filters are grep-like:
- `grep1` is the primary filter and may be pushed down to SQL (when possible)
- `grep2` and `grep3` further filter in-memory and are used for ANSI coloring
- output stays text so it can be piped to `grep`, saved, etc.

Example:
```
wget "http://host.example.com:8080/export_unsecure?grep1=qemu&grep2=aarch64&grep3=centos-stream-9&session=8f7f1b24&color=always" -O - -q
```
Parameters (current):
- `grep1`, `grep2`, `grep3`: regular expressions
- `session`: filter by session_id
- `limit`: maximum number of lines (capped server-side)
- `order`: ordering (ingest/client time) (WIP depending on build)
- `color=always|never`: enable ANSI highlighting

## Web interface (WIP)  

A new web interface is planned but is not the current focus.
The recommended workflow today is:

- ingest commands continuously
- fetch via `/export_unsecure` (or `/export_ssl` when enabled)
- use terminal tools (grep, less -R, etc.) on the exported text

## Security notes  

- Plain TCP ingestion and unsecure export are intended for controlled networks only.
- TLS ingestion is supported via server certificates.
- HTTPS export and mTLS client certificate authentication are planned.
- The system is currently mono-tenant by configuration, but it is designed to evolve into multi-tenant (tenant resolution/auth pipeline is being built).
