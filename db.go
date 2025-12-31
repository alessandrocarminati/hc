package main

import (
	"bufio"
	"context"
//	"crypto/sha256"
	"database/sql"
//	"encoding/hex"
	"fmt"
	"regexp"
//	"strings"
	"unicode/utf8"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
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
}

func OpenDB(ctx context.Context, dsn string) (*DB, error) {
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
	if d == nil || d.SQL == nil {
		return nil
	}
	return d.SQL.Close()
}

func sanitizeUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, "�")
}

func (d *DB) VerifySchema(ctx context.Context) error {
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
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}

func (d *DB) EnsureTenant(ctx context.Context, tenantID, name string) error {
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
			$1, $2, $3, $4, $5, $6, 'import', $7, $8
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

		ev := ParseLegacyLineBestEffort(tenantID, line)

		res, e := stmt.ExecContext(ctx,
			ev.TenantID,
			ev.TSClient,
			ev.SessionID,
			ev.HostFQDN,
			ev.CWD,
			ev.Cmd,
			ev.RawLine,
			ev.TSClient != nil,
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

func ParseLegacyLineBestEffort(tenantID, line string) Event {
	ev := Event{
		TenantID:   tenantID,
		RawLine:   line,
		Transport: "import",
	}

	s := strings.TrimRight(line, "\r\n")
	s = strings.TrimSpace(s)
	if s == "" {
		ev.SessionID = "unknown"
		ev.HostFQDN = "unknown"
		return ev
	}

	// timestamp + session + host + payload
	reSess := regexp.MustCompile(`^(\d{8}\.\d{6})\s*-\s*([0-9a-fA-F]{8})\s*-\s*(.+?)\s{2,}(.*)$`)
	reSessLoose := regexp.MustCompile(`^(\d{8}\.\d{6})\s*-\s*([0-9a-fA-F]{8})\s*-\s*(.+?)\s+(.*)$`)

	// timestamp + host + payload (no session)
	reNoSess := regexp.MustCompile(`^(\d{8}\.\d{6})\s*-\s*(.+?)\s{2,}(.*)$`)
	reNoSessLoose := regexp.MustCompile(`^(\d{8}\.\d{6})\s*-\s*(.+?)\s+(.*)$`)

	// timestamp only fallback
	reTSOnly := regexp.MustCompile(`^(\d{8}\.\d{6})\s+(.*)$`)

	var (
		tsStr    string
		session  *string
		host     *string
		payload  string
		parseOK  bool
		hostNote bool
	)

	if m := reSess.FindStringSubmatch(s); m != nil {
		tsStr = m[1]
		session = strPtr(strings.TrimSpace(m[2]))
		host = strPtr(strings.TrimSpace(m[3]))
		payload = m[4]
		parseOK = true
	} else if m := reSessLoose.FindStringSubmatch(s); m != nil {
		tsStr = m[1]
		session = strPtr(strings.TrimSpace(m[2]))
		host = strPtr(strings.TrimSpace(m[3]))
		payload = m[4]
		parseOK = true
	} else if m := reNoSess.FindStringSubmatch(s); m != nil {
		tsStr = m[1]
		host = strPtr(strings.TrimSpace(m[2]))
		payload = m[3]
		parseOK = true
	} else if m := reNoSessLoose.FindStringSubmatch(s); m != nil {
		tsStr = m[1]
		host = strPtr(strings.TrimSpace(m[2]))
		payload = m[3]
		parseOK = true
	} else if m := reTSOnly.FindStringSubmatch(s); m != nil {
		tsStr = m[1]
		payload = m[2]
		parseOK = false
	} else {
		ev.SessionID = "unknown"
		ev.HostFQDN = "unknown"
		return ev
	}

	if tsStr != "" {
		if t, ok := parseTS(tsStr); ok {
			ev.TSClient = &t
		}
	}

	if session != nil && strings.TrimSpace(*session) != "" {
		ev.SessionID = *session
	} else {
		ev.SessionID = "unknown"
	}

	if host != nil && strings.TrimSpace(*host) != "" {
		h := strings.TrimSpace(*host)
		if strings.ContainsAny(h, " \t") {
			hostNote = true
		}
		ev.HostFQDN = h
	} else {
		ev.HostFQDN = "unknown"
	}

	cmdText := strings.TrimSpace(payload)
	if strings.HasPrefix(cmdText, ">") {
		cmdText = strings.TrimSpace(strings.TrimPrefix(cmdText, ">"))
	}
	if cmdText != "" {
		ev.Cmd = strPtr(cmdText)
	}

	_ = parseOK
	_ = hostNote

	ev.RawLine = sanitizeUTF8(ev.RawLine)
	if ev.Cmd != nil {
		c := sanitizeUTF8(*ev.Cmd)
		c = strings.TrimSpace(c)
		ev.Cmd = &c
	}

	return ev
}

func parseTS(s string) (time.Time, bool) {
	t, err := time.ParseInLocation("20060102.150405", s, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func strPtr(s string) *string { return &s }

