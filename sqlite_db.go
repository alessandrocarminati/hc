package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"log"
	"os"
	"regexp"
	"strings"
	"time"
)

type SQLiteDB struct {
	SQL *sql.DB
}

func OpenSQLiteDB(ctx context.Context, path string) (*SQLiteDB, error) {
	debugPrint(log.Printf, levelCrazy, "Args=%v, %s\n", path)

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("sql open: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("db ping: %w", err)
	}

	return &SQLiteDB{SQL: db}, nil
}

func (d *SQLiteDB) Close() error {
	debugPrint(log.Printf, levelCrazy, "Args=none\n")
	if d == nil || d.SQL == nil {
		return nil
	}
	return d.SQL.Close()
}

func (d *SQLiteDB) EnsureSchema(ctx context.Context) error {
	debugPrint(log.Printf, levelCrazy, "Args=%v\n", ctx)
	stmts := []string{
		`create table if not exists tenants (
			id uuid primary key,
			name text not null unique,
			created_at timestamptz not null default now()
		);`,

		`create table if not exists cmd_events (
			id integer primary key autoincrement,
			tenant_id uuid not null references tenants(id),

			ts_client timestamptz,
			session_id text not null,
			host_fqdn text not null,
			cwd text,
			cmd text,

			ts_ingested timestamptz not null default now(),
			src_ip inet,
			transport text not null default 'import',
			parse_ok boolean not null default true,

			raw_line text not null
		);`,

		`create index if not exists cmd_events_tenant_id_id_desc
			on cmd_events (tenant_id, id desc);`,

		`create index if not exists cmd_events_raw_trgm
			on cmd_events using gin (raw_line gin_trgm_ops);`,

		`create index if not exists cmd_events_cmd_trgm
			on cmd_events using gin (cmd gin_trgm_ops);`,
	}

	for _, s := range stmts {
		if _, err := d.SQL.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("ensure schema failed on %q: %w", shortSQL(s), err)
		}
	}
	return nil
}

func (d *SQLiteDB) EnsureTenant(ctx context.Context, tenantID, name string) error {
	debugPrint(log.Printf, levelCrazy, "Args=%v, %s, %s\n", ctx, tenantID, name)
	_, err := d.SQL.ExecContext(ctx,
		`insert into tenants(id, name) values ($1, $2)
		 on conflict (id) do nothing;`,
		tenantID, name)
	if err != nil {
		return fmt.Errorf("ensure tenant: %w", err)
	}
	return nil
}

func (d *SQLiteDB) GetTenantName(ctx context.Context, tenantID string) (string, bool, error) {
	debugPrint(log.Printf, levelCrazy, "Args=%v, %s\n", ctx, tenantID)
	var name string
	err := d.SQL.QueryRowContext(
		ctx,
		`select name from tenants where id = $1`,
		tenantID,
	).Scan(&name)

	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get tenant: %w", err)
	}
	return name, true, nil
}

func (d *SQLiteDB) ImportHistoryFile(ctx context.Context, tenantID, path string) (inserted int, skipped int, err error) {
	debugPrint(log.Printf, levelCrazy, "Args=%v, %s, %s\n", ctx, tenantID, path)
	transport := "import"
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, fmt.Errorf("open history file: %w", err)
	}
	defer f.Close()

	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	stmt, err := tx.PrepareContext(ctx, `
		insert into cmd_events(
			tenant_id, ts_client, session_id, host_fqdn, cwd, cmd, transport, raw_line, parse_ok
		) values (
			$1, $2, $3, $4, $5, $6, $7, $8, $9
		);
	`)
	if err != nil {
		return 0, 0, fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()

	sc := bufio.NewScanner(f)

	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 2*1024*1024)

	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r\n")
		if strings.TrimSpace(line) == "" {
			continue
		}

		ev, _ := ParseIngestLine(tenantID, line)
		tmp := "unknown"
		ev.Transport = transport
		ev.SrcIP = &tmp

		res, e := stmt.ExecContext(ctx,
			tenantID,
			ev.TSClient,
			ev.SessionID,
			ev.HostFQDN,
			ev.CWD,
			ev.Cmd,
			transport,
			ev.RawLine,
			ev.ParseOK,
		)
		if e != nil {
			err = fmt.Errorf("insert line failed: %w", e)
			return inserted, skipped, err
		}

		ra, _ := res.RowsAffected()
		if ra == 1 {
			inserted++
		} else {
			skipped++
		}
	}
	if e := sc.Err(); e != nil {
		err = fmt.Errorf("scan history file: %w", e)
		return inserted, skipped, err
	}

	if err = tx.Commit(); err != nil {
		return inserted, skipped, fmt.Errorf("commit: %w", err)
	}
	return inserted, skipped, nil
}

func (d *SQLiteDB) MaxSeq(ctx context.Context, tenantID string) (int64, error) {
	var seq sql.NullInt64
	debugPrint(log.Printf, levelDebug, "Args: %v, %s\n", ctx, tenantID)

	err := d.SQL.QueryRowContext(ctx, `
		select max(id) from cmd_events where tenant_id = $1
	`, tenantID).Scan(&seq)
	if err != nil {
		return 0, err
	}
	if !seq.Valid {
		return 0, nil
	}
	return seq.Int64, nil
}

func (d *SQLiteDB) InsertEventWithSeq(ctx context.Context, ev Event, seq int64) error {
	debugPrint(log.Printf, levelDebug, "Args: %v, %v, %d\n", ctx, ev, seq)

	TSClient := nullTime(ev.TSClient)
	CWD := nullString(ev.CWD)
	Cmd := nullString(ev.Cmd)
	SrcIP := nullString(ev.SrcIP)
	_, err := uuid.Parse(ev.TenantID)
	if err != nil {
		return err
	}

//	debugPrint(log.Printf, levelDebug, "insert into cmd_events (tenant_id, ts_client, session_id, host_fqdn, cwd, cmd, raw_line, src_ip, transport, parse_ok) values (%v. %d, 
	_, err = d.SQL.ExecContext(ctx, `
		insert into cmd_events
			(tenant_id, seq, ts_client, session_id, host_fqdn, cwd, cmd, raw_line, src_ip, transport, parse_ok)
		values
			($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		on conflict (tenant_id, seq) do nothing
	`,
		ev.TenantID,
		seq,
		&TSClient,
		ev.SessionID,
		ev.HostFQDN,
		&CWD,
		&Cmd,
		ev.RawLine,
		&SrcIP,
		ev.Transport,
		ev.ParseOK,
	)
	return err
}

func (d *SQLiteDB) lookupTenantByUsername(username string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var tenantID string

	err := d.SQL.QueryRowContext(ctx, `
		select tenant_id::text
		from app_users
		where username = $1
		limit 1
	`, username).Scan(&tenantID)

	if err != nil {
		return "", false
	}

	return strings.TrimSpace(tenantID), tenantID != ""
}

func (d *SQLiteDB) insertAPIKey(ctx context.Context, id uuid.UUID, tenant uuid.UUID, user *uuid.UUID, keyID, keyHash string) error {
	debugPrint(log.Printf, levelDebug, "insert into api_keys values ('%s', '%s', '%s', '%s', '%s', %s));\n", id.String(), tenant.String(), user.String(), keyID, keyHash, "1234")
	_, err := d.SQL.ExecContext(ctx, `
		insert into api_keys (id, tenant_id, user_id, key_id, key_hash)
		values ($1,$2,$3,$4,$5)
	`, id, tenant, user, keyID, keyHash)
	return err
}

func (db *SQLiteDB) RequireTenantExists(ctx context.Context, tenantID uuid.UUID) error {
	var tmp string
	err := db.SQL.QueryRowContext(
		ctx,
		`select id from tenants where id = ?`,
		tenantID.String(),
	).Scan(&tmp)

	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("tenant %s not found in tenants table", tenantID.String())
	}
	return err
}

func (db *SQLiteDB) GetAPIKeyByKeyID(ctx context.Context, keyID string) (APIKeyRecord, bool, error) {
	var info APIKeyRecord
	var tenantText string

	debugPrint(log.Printf, levelDebug, "Args: %v, %s\n", ctx, keyID)
	err := db.SQL.QueryRowContext(ctx, `
		select tenant_id, key_hash, revoked_at
		from api_keys
		where key_id = ?
	`, keyID).Scan(&tenantText, &info.KeyHash, &info.Revoked)

	debugPrint(log.Printf, levelDebug, "info: %v\n", info)

	if errors.Is(err, sql.ErrNoRows) {
		return APIKeyRecord{}, false, nil
	}
	if err != nil {
		return APIKeyRecord{}, false, err
	}

	_, err = uuid.Parse(tenantText)
	if err != nil {
		return APIKeyRecord{}, false, fmt.Errorf("parse tenant_id: %w", err)
	}
	info.TenantID = tenantText

	return info, true, nil
}

func (db *SQLiteDB) ExportLines(ctx context.Context, tenantID string, q exportQuery) ([]string, error) {
	orderSQL, err := exportOrderSQLSQLite(q.Order)
	if err != nil {
		return nil, err
	}

	var grep1Re *regexp.Regexp
	useRegex1 := false

	g1 := strings.TrimSpace(q.Grep1)
	if g1 != "" && !IsPlainSubstring(g1) {
		grep1Re, err = regexp.Compile("(?i)" + g1)
		if err != nil {
			return nil, err
		}
		useRegex1 = true
	}

	var sb strings.Builder
	args := make([]any, 0, 4)

	// Select cmd too, so substring filtering can work on both raw_line and cmd
	// and regex fallback can still inspect raw_line only.
	sb.WriteString(`
		select raw_line, cmd
		from cmd_events
		where tenant_id = ?
	`)
	args = append(args, tenantID)

	if q.Session != "" {
		sb.WriteString(` and session_id = ?`)
		args = append(args, q.Session)
	}

	// Plain substring grep can be done in SQL.
	if g1 != "" && !useRegex1 {
		sb.WriteString(` and (lower(raw_line) like lower(?) or lower(cmd) like lower(?))`)
		pat := "%" + g1 + "%"
		args = append(args, pat, pat)
	}

	sb.WriteString(` order by `)
	sb.WriteString(orderSQL)

	// Only push LIMIT into SQL when all filtering is done there.
	// If regex filtering happens in Go, applying SQL LIMIT first would change semantics.
	if !useRegex1 && q.Limit > 0 {
		sb.WriteString(` limit ?`)
		args = append(args, q.Limit)
	}

	debugPrint(log.Printf, levelDebug, "sqlite export sql=%s args=%s\n", sb.String(), safeJSON(args))

	rows, err := db.SQL.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]string, 0, minInt(q.Limit, 256))
	for rows.Next() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		var rawLine string
		var cmd sql.NullString

		if err := rows.Scan(&rawLine, &cmd); err != nil {
			return nil, err
		}

		// Regex fallback in Go.
		if useRegex1 && !grep1Re.MatchString(rawLine) {
			continue
		}

		out = append(out, rawLine)

		// When regex filtering is done in Go, enforce the effective limit here.
		if useRegex1 && q.Limit > 0 && len(out) >= q.Limit {
			break
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}

func exportOrderSQLSQLite(order string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(order)) {
	case "ingest_asc":
		return "id asc", nil
	case "ingest_desc":
		return "id desc", nil
	case "client_asc":
		return "ts_client asc", nil
	case "client_desc":
		return "ts_client desc", nil

	default:
		return "", fmt.Errorf("invalid order %q", order)
	}
}
