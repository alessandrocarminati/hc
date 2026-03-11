package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
	"context"
)

type SQLiteDB struct {
	SQL *sql.DB
}

func OpenSQLiteDB(path string) (*SQLiteDB, error) {
	debugPrint(log.Printf, levelCrazy, "Args=%v, %s\n", path)

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("sql open: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

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

func (d *SQLiteDB) StreamExport(ctx context.Context, w io.Writer, flusher http.Flusher, opt StreamExportOptions) error {
	debugPrint(log.Printf, levelCrazy, "Args=%v, %v, %v, %v\n", ctx, w, flusher, opt)
	if d == nil || d.SQL == nil {
		return fmt.Errorf("db not initialized")
	}
	tenantID := strings.TrimSpace(opt.TenantID)
	if tenantID == "" {
		return fmt.Errorf("tenant_id is required")
	}

	limit := opt.Limit
	if limit <= 0 {
		limit = 200000
	}
	batch := opt.BatchSize
	if batch <= 0 {
		batch = 5000
	}
	if batch > limit {
		batch = limit
	}

	var (
		lastID  *int64
		written int
	)

	for written < limit {
		rows, err := d.SQL.QueryContext(ctx, `
			select id, ts_client, ts_ingested, session_id, host_fqdn, cwd, cmd, raw_line
			from cmd_events
			where tenant_id = $1
			  and ($2::bigint is null or id < $2)
			order by id desc
			limit $3
		`, tenantID, lastID, batch)
		if err != nil {
			return fmt.Errorf("export query: %w", err)
		}

		n := 0
		for rows.Next() {
			n++

			var (
				id         int64
				tsClient   sql.NullTime
				tsIngested time.Time
				sessionID  sql.NullString
				host       sql.NullString
				cwd        sql.NullString
				cmd        sql.NullString
				raw        string
			)

			if err := rows.Scan(&id, &tsClient, &tsIngested, &sessionID, &host, &cwd, &cmd, &raw); err != nil {
				_ = rows.Close()
				return fmt.Errorf("export scan: %w", err)
			}

			line := formatExportLine(tsClient, tsIngested, sessionID, host, cwd, cmd, raw)
			if _, err := io.WriteString(w, line+"\n"); err != nil {
				_ = rows.Close()
				return fmt.Errorf("export write: %w", err)
			}

			lastID = &id
			written++
			if written >= limit {
				break
			}
		}

		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("export rows: %w", err)
		}
		_ = rows.Close()

		if flusher != nil {
			flusher.Flush()
		}

		if n == 0 {
			return nil
		}

		remaining := limit - written
		if remaining < batch {
			batch = remaining
		}
	}

	return nil
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
	u, err := uuid.Parse(ev.TenantID)
	if err != nil {
		return err
	}

	_, err = d.SQL.ExecContext(ctx, `
		insert into cmd_events
			(tenant_id, ts_client, session_id, host_fqdn, cwd, cmd, raw_line, src_ip, transport, parse_ok)
		values
			($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
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
