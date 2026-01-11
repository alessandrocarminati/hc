package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"unicode/utf8"
	"os"
	"io"
	"net/http"
	"strings"
	"time"
	"log"
	_ "github.com/lib/pq"
	"github.com/google/uuid"
)

type DB struct {
	SQL *sql.DB
}

type Event struct {
	TenantID  string
	TSClient  *time.Time
	SessionID string
	HostFQDN  string
	CWD       *string
	Cmd       *string
	RawLine   string
	Transport string
	SrcIP     *string
	ParseOK   bool
}

type StreamExportOptions struct {
	TenantID  string
	Limit     int
	BatchSize int
}

func OpenDB(ctx context.Context, dsn string) (*DB, error) {
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

	return &DB{SQL: db}, nil
}

func (d *DB) Close() error {
	debugPrint(log.Printf, levelCrazy, "Args=none\n")
	if d == nil || d.SQL == nil {
		return nil
	}
	return d.SQL.Close()
}

func sanitizeUTF8(s string) string {
	debugPrint(log.Printf, levelCrazy, "Args=%s\n", s)
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, "�")
}

func (d *DB) EnsureSchema(ctx context.Context) error {
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

func shortSQL(s string) string {
	debugPrint(log.Printf, levelCrazy, "Args=%s\n", s)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}

func (d *DB) EnsureTenant(ctx context.Context, tenantID, name string) error {
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

func (d *DB) GetTenantName(ctx context.Context, tenantID string) (string, bool, error) {
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

func (d *DB) ImportHistoryFile(ctx context.Context, tenantID, path string) (inserted int, skipped int, err error) {
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
		seq = seq +1
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

func parseTS(s string) (time.Time, bool) {
	debugPrint(log.Printf, levelCrazy, "Args=%s\n", s)
	t, err := time.ParseInLocation("20060102.150405", s, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func strPtr(s string) *string { return &s }

func (d *DB) StreamExport(ctx context.Context, w io.Writer, flusher http.Flusher, opt StreamExportOptions) error {
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

func formatExportLine(tsClient sql.NullTime, tsIngested time.Time, sessionID sql.NullString, host sql.NullString, cwd sql.NullString, cmd sql.NullString, raw string) string {
	debugPrint(log.Printf, levelCrazy, "Args=%v, %v, %v, %v, %v, %s, %s\n", tsClient, tsIngested, sessionID, host, cwd, cmd, raw, )
	t := tsIngested
	if tsClient.Valid {
		t = tsClient.Time
	}
	tsStr := t.Format("20060102.150405")

	sess := "-"
	if sessionID.Valid {
		v := strings.TrimSpace(sessionID.String)
		if v != "" && v != "unknown" {
			sess = v
		}
	}

	h := "-"
	if host.Valid {
		v := strings.TrimSpace(host.String)
		if v != "" && v != "unknown" {
			h = v
		}
	}

	payload := ""
	if cmd.Valid {
		v := strings.TrimSpace(cmd.String)
		if v != "" {
			payload = v
		}
	}
	if payload == "" {
		payload = strings.TrimSpace(raw)
		if payload == "" {
			payload = "-"
		}
	}
	payload = sanitizeForOneLine(payload)

	if cwd.Valid {
		c := strings.TrimSpace(cwd.String)
		if c != "" {
			return fmt.Sprintf("%s - %s - %s [cwd=%s] > %s", tsStr, sess, h, sanitizeForOneLine(c), payload)
		}
	}

	return fmt.Sprintf("%s - %s - %s > %s", tsStr, sess, h, payload)
}

func sanitizeForOneLine(s string) string {
	debugPrint(log.Printf, levelCrazy, "Args=%s\n", s)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return strings.TrimRight(s, " \t")
}

func (db *DB) MaxSeq(ctx context.Context, tenantID string) (int64, error) {
	var seq sql.NullInt64
	debugPrint(log.Printf, levelDebug, "Args: %v, %s\n", ctx, tenantID )

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

func (db *DB) InsertEventWithSeq(ctx context.Context, ev Event, seq int64) error {
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

func nullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{Valid: false}
	}
	if *s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{
		String: *s,
		Valid:  true,
	}
}

func nullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{Valid: false}
	}
	if t.IsZero() {
		return sql.NullTime{Valid: false}
	}
	return sql.NullTime{
		Time:  *t,
		Valid: true,
	}
}

func (db *DB) lookupTenantByUsername(username string) (string, bool) {
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
