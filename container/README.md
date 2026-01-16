# Demo setup steps

## 1. Clone the repository

```
git clone https://github.com/alessandrocarminati/hc cd hc/container
```

## 2. Create the PostgreSQL data directory

```
mkdir -p hc/pgdata
```
This directory is used to persist the database across restarts.

## 3. Build the containers

Build the `hc` application image:
```
podman build --no-cache -f Dockerfile.hc -t hc:dev .
```
Build the PostgreSQL image (schema fetched from GitHub):
```
podman build --no-cache -f Dockerfile.db -t hc-postgres:dev --build-arg HC_REF=master .
```

### 4. Provide TLS certificates

Create a `certs/` directory containing:
```
certs/
  ca.crt
  host.crt
  host.key
```
These files are mounted read-only into the `hc` container.

## 5. Run the demo

```
podman-compose up -d
```

## Access

Default exposed ports:
* TCP ingest (clear): `1234`
* TCP ingest (TLS): `1235`
* HTTP: `8080`
* HTTPS: `8443`

Logs:
```
podman-compose logs -f
```

## Notes
* Database initialization runs **only on first startup**
* To reset the database:
  ```
  podman-compose down rm -rf hc/pgdata/*
  podman-compose up -d` 
  ```
* This setup is intended for **local testing and demonstrations only**
