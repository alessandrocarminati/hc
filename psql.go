package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

type PgsqlDB struct {
	SQL *sql.DB
}

func OpenPgsqlDB(ctx context.Context, dsn string) (*PgsqlDB, error) {
	debugPrint(log.Printf, levelCrazy, "Args=%v, %s\n", ctx, dsn)

	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("invalid postgres dsn")
	}
	db, err := sql.Open("postgres", dsn)
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

	return &PgsqlDB{SQL: db}, nil
}

func (d *PgsqlDB) Close() error {
	debugPrint(log.Printf, levelCrazy, "Args=none\n")
	if d == nil || d.SQL == nil {
		return nil
	}
	return d.SQL.Close()
}

func (d *PgsqlDB) EnsureSchema(ctx context.Context) error {
	debugPrint(log.Printf, levelCrazy, "Args=%v\n", ctx)
	stmts := []string{
		`create extension if not exists pg_trgm;`,

		`create table if not exists tenants (
			id uuid primary key,
			name text not null unique,
			created_at timestamptz not null default now()
		);`,

		`create table if not exists cmd_events (
			id bigserial primary key,
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

func (d *PgsqlDB) EnsureTenant(ctx context.Context, tenantID, name string) error {
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

func (d *PgsqlDB) GetTenantName(ctx context.Context, tenantID string) (string, bool, error) {
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

func (d *PgsqlDB) ImportHistoryFile(ctx context.Context, tenantID, path string) (inserted int, skipped int, err error) {
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
			seq, tenant_id, ts_client, session_id, host_fqdn, cwd, cmd, transport, raw_line, parse_ok
		) values (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10
		);
	`)
	if err != nil {
		return 0, 0, fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()

	sc := bufio.NewScanner(f)

	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 2*1024*1024)

	seq, err := d.MaxSeq(ctx, tenantID)
	if err != nil {
		return 0, 0, fmt.Errorf("cant recover seq (%v)", err)
	}

	for sc.Scan() {
		seq = seq + 1
		line := strings.TrimRight(sc.Text(), "\r\n")
		if strings.TrimSpace(line) == "" {
			continue
		}

		ev, _ := ParseIngestLine(tenantID, line)
		tmp := "unknown"
		ev.Transport = transport
		ev.SrcIP = &tmp

		res, e := stmt.ExecContext(ctx,
			seq,
			ev.TenantID,
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

func (db *PgsqlDB) MaxSeq(ctx context.Context, tenantID string) (int64, error) {
	var seq sql.NullInt64
	debugPrint(log.Printf, levelDebug, "Args: %v, %s\n", ctx, tenantID)

	err := db.SQL.QueryRowContext(ctx, `
		select max(seq) from cmd_events where tenant_id = $1
	`, tenantID).Scan(&seq)
	if err != nil {
		return 0, err
	}
	if !seq.Valid {
		return 0, nil
	}
	return seq.Int64, nil
}

func (db *PgsqlDB) InsertEventWithSeq(ctx context.Context, ev Event, seq int64) error {
	debugPrint(log.Printf, levelDebug, "Args: %v, %v, %d\n", ctx, ev, seq)

	TSClient := nullTime(ev.TSClient)
	CWD := nullString(ev.CWD)
	Cmd := nullString(ev.Cmd)
	SrcIP := nullString(ev.SrcIP)
	u, err := uuid.Parse(ev.TenantID)
	if err != nil {
		return err
	}

	_, err = db.SQL.ExecContext(ctx, `
		insert into cmd_events
			(tenant_id, seq, ts_client, session_id, host_fqdn, cwd, cmd, raw_line, src_ip, transport, parse_ok)
		values
			($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		on conflict (tenant_id, seq) do nothing
	`,
		u,
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

func (db *PgsqlDB) lookupTenantByUsername(username string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var tenantID string

	err := db.SQL.QueryRowContext(ctx, `
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

func (db *PgsqlDB) insertAPIKey(ctx context.Context, id uuid.UUID, tenant uuid.UUID, user *uuid.UUID, keyID, keyHash string) error {
	debugPrint(log.Printf, levelDebug, "insert into api_keys values ('%s', '%s', '%s', '%s', '%s', %s));\n", id.String(), tenant.String(), user.String(), keyID, keyHash, "1234")
	_, err := db.SQL.ExecContext(ctx, `
		insert into api_keys (id, tenant_id, user_id, key_id, key_hash)
		values ($1,$2,$3,$4,$5)
	`, id, tenant, user, keyID, keyHash)
	return err
}

func (db *PgsqlDB) RequireTenantExists(ctx context.Context, tenantID uuid.UUID) error {
	var tmp uuid.UUID
	err := db.SQL.QueryRowContext(
		ctx,
		`select id from tenants where id = $1`,
		tenantID,
	).Scan(&tmp)

	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("tenant %s not found in tenants table", tenantID.String())
	}
	return err
}

func (db *PgsqlDB) GetAPIKeyByKeyID(ctx context.Context, keyID string) (APIKeyRecord, bool, error) {
	var info APIKeyRecord

	err := db.SQL.QueryRowContext(ctx, `
		select tenant_id, key_hash, revoked_at
		from api_keys
		where key_id = $1
	`, keyID).Scan(&info.TenantID, &info.KeyHash, &info.Revoked)

	if errors.Is(err, sql.ErrNoRows) {
		return APIKeyRecord{}, false, nil
	}
	if err != nil {
		return APIKeyRecord{}, false, err
	}
	return info, true, nil
}

func (db *PgsqlDB) ExportLines(ctx context.Context, tenantID string, q exportQuery) ([]string, error) {
	orderSQL, err := exportOrderSQL(q.Order)
	if err != nil {
		return nil, err
	}

	sb := strings.Builder{}
	args := []any{}
	argN := 1

	sb.WriteString(`select raw_line
		from cmd_events
		where tenant_id = $`)
	sb.WriteString(strconv.Itoa(argN))
	args = append(args, tenantID)
	argN++

	if q.Session != "" {
		sb.WriteString(` and session_id = $`)
		sb.WriteString(strconv.Itoa(argN))
		args = append(args, q.Session)
		argN++
	}

	g1 := strings.TrimSpace(q.Grep1)
	if g1 != "" {
		if IsPlainSubstring(g1) {
			sb.WriteString(` and (raw_line ilike $`)
			sb.WriteString(strconv.Itoa(argN))
			sb.WriteString(` or cmd ilike $`)
			sb.WriteString(strconv.Itoa(argN))
			sb.WriteString(`)`)
			args = append(args, "%"+g1+"%")
			argN++
		} else {
			sb.WriteString(` and raw_line ~* $`)
			sb.WriteString(strconv.Itoa(argN))
			args = append(args, g1)
			argN++
		}
	}

	sb.WriteString(` order by `)
	sb.WriteString(orderSQL)
	sb.WriteString(` limit $`)
	sb.WriteString(strconv.Itoa(argN))
	args = append(args, q.Limit)

	rows, err := db.SQL.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var rawLine string
		if err := rows.Scan(&rawLine); err != nil {
			return nil, err
		}
		out = append(out, rawLine)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
