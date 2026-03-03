# Changelog

Milestone changelog for **hc (History Collector)**.

Tags are **baselines**. Between tags, the build can derive a patch number from
"commits since last tag".

---

## Unreleased
### Added
- Tenant history encryption at rest (DB encryption) and query-time decryption (`key=`).
- Documentation updates and operational polish.
- Asymmetric crypto helper commands.

### Fixed
- Terminal/process hangs in some `socat` usages in the helper bash script.
- Various config and logging cleanups.

---

## v0.4 - Project formalization + licensing

### Added
- Licensing and basic community files.

### Changed
- Ongoing stabilization work after the HTTPS exporter milestone:
  configuration refactors, auth/transport refinements, and docs cleanup.

---

## v0.3 - HTTPS exporter milestone

### Added
- HTTPS transport for exporter (secure history querying).

### Context / what this milestone represents
This tag closes the "new architecture is usable end-to-end" loop:
DB-backed ingestion + TLS ingestion + HTTP filtering + tenant auth routing,
now complemented by HTTPS for secure export/querying.

---

## v0.2 - Start of the new DB-backed line

### Changed (major direction change starting around 3a93d60)
- Start of the refactor away from file-backed/grep-only history towards a
  database-backed ingestion + export model.
- Iteration toward a "plain text in / plain text out" workflow that works with `wget`/`socat` queries.

### Added (within this line, before v0.3)
- First working DB-backed ingestion (raw TCP).
- TLS ingestion.
- First HTTP filter/export mechanism.
- BusyBox-compatible client-side forwarding mechanism (no Bash-only features).
- Authentication mechanisms to route queries/filters to the correct tenant.
- API key based multitenancy for ingestion.
- Remote helper script to SSH into servers and inject history forwarding logic.
