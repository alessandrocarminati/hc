package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"io"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type ExportService struct {
	Opts *Options
	DB   *DB
}

func RegisterExportHandlers(mux *http.ServeMux, opts *Options, db *DB) {
	s := &ExportService{Opts: opts, DB: db}

	mux.HandleFunc("/export", s.handleExportUnsecure)

	mux.HandleFunc("/web_app", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not implemented", http.StatusNotImplemented)
	})
}

type exportQuery struct {
	Grep1   string
	Grep2   string
	Grep3   string
	Session string

	Order string
	Limit int

	Color string
}

func getIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

func parseBearerAPIKey(v string) (keyID, secret string) {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(strings.ToLower(v), "bearer ") {
		return "", ""
	}
	tok := strings.TrimSpace(v[len("bearer "):])

	debugPrint(log.Printf, levelDebug, "found %s\n", tok)
	i := strings.IndexByte(tok, '.')
	if i <= 0 || i == len(tok)-1 {
		debugPrint(log.Printf, levelInfo, "Api key not usable\n")
		return "", ""
	}

	return tok[:i], tok[i+1:]
}

func (s *ExportService) getTenantFromHTTPAPI(msg *http.Request) string{
	debugPrint(log.Printf, levelCrazy, "ARG=%v\n", *msg)

	debugPrint(log.Printf, levelDebug, "Extract Authorization header\n")
	authz := msg.Header.Get("Authorization")
	if authz == "" {
		debugPrint(log.Printf, levelDebug, "no authorization header!\n")
		return ""
	}

	debugPrint(log.Printf, levelDebug, "Expected: \"Authorization: <key_id>.<secret>\"\n")
	keyID, secret := parseBearerAPIKey(authz)
	if keyID == "" || secret == "" {
		debugPrint(log.Printf, levelDebug, "no authorization header wrong format\n")
		return ""
	}

	debugPrint(log.Printf, levelDebug, "Lookup api_keys row by key_id\n")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var (
		tenantID string
		keyHash  string
		revoked  sql.NullTime
	)

	err := s.DB.SQL.QueryRowContext(ctx, `
		select tenant_id::text, key_hash, revoked_at
		from api_keys
		where key_id = $1
	`, keyID).Scan(&tenantID, &keyHash, &revoked)

	if err != nil {
		return ""
	}

	if revoked.Valid {
		debugPrint(log.Printf, levelDebug, "key explicitly revoked\n")
		return ""
	}

	debugPrint(log.Printf, levelDebug, "Verify secret\n")
	pepper := strings.TrimSpace(s.Opts.Cfg.Globals.Pepper)
	if !verifySecretSHA256(secret, pepper, keyHash) {
		return ""
	}

	debugPrint(log.Printf, levelDebug, "SUCCESS: authenticated, tenant resolved\n")
	return tenantID
}

func (s *ExportService) getTenant(msg *http.Request) string{
	debugPrint(log.Printf, levelCrazy, "Args: %v\n", msg)

	authMethods := s.Opts.Cfg.Server.HTTP.Auth
	TLSFlag := false
	if msg.TLS != nil {
		authMethods = s.Opts.Cfg.Server.HTTPS.Auth
		TLSFlag = true
	}
	debugPrint(log.Printf, levelDebug, "Itearate over defined methods %v: TLS=%t\n", authMethods, TLSFlag)
	for _, method := range authMethods {
		switch AuthMode(strings.ToLower(string(method))) {
			case AuthNone:
				debugPrint(log.Printf, levelInfo, "Using default tenant\n")
				t := strings.TrimSpace(s.Opts.Cfg.Globals.DefaultTenantID)
				if t != "" {
					return t
				}
			case AuthAPIKey:
				debugPrint(log.Printf, levelDebug, "Using AuthAPIKey method\n")
				if !TLSFlag {
					 debugPrint(log.Printf, levelWarning, "== WARNING == use of APIKEY in cleartex request!\n")
				}
				t := s.getTenantFromHTTPAPI(msg)
				if t != "" {
					return t
				}
			case AuthCert:
				debugPrint(log.Printf, levelDebug, "Using AuthCert method\n")
			default:
				debugPrint(log.Printf, levelWarning, "Warning unsupported auth method in the list\n")
		}
	}
	return ""
}

func (s *ExportService) handleExportUnsecure(w http.ResponseWriter, r *http.Request) {
	debugPrint(log.Printf, levelCrazy, "Args=%v, %v\n", w, r)
	if r.Method != http.MethodGet {
		debugPrint(log.Printf, levelInfo, "not allowed method request form %s\n", getIP(r))
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !s.Opts.Cfg.Server.IngestClear.Enabled {
		http.Error(w, "export_unsecure disabled", http.StatusNotFound)
		return
	}

	cd := connDataFromRequest(r)

	authPipeline := []authFn{
		func(c connData) authRes { return AuthAllow }, // stub allow-all
	}

	switch runAuthPipeline(cd, authPipeline) {
	case AuthAllow:
		// proceed
	default:
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	tenantID := s.getTenant(r)
	if tenantID == "" {
		debugPrint(log.Printf, levelError, "no default tenantID\n")
		http.Error(w, "export_unsecure no default tenantID", http.StatusInternalServerError)
		return
	}

	q, err := parseExportQuery(r, s.Opts.Cfg.Globals.MaxRows)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	pipe, err := CompileGrepPipeline(q.Grep1, q.Grep2, q.Grep3, q.Color)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	maxSec := s.Opts.Cfg.Globals.MaxSeconds
	if maxSec <= 0 {
		maxSec = 30
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(maxSec)*time.Second)
	defer cancel()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	flusher, _ := w.(http.Flusher)

	rows, err := s.queryExportRows(ctx, tenantID, q)
	if err != nil {
		log.Printf("export_unsecure query failed: %v", err)
		http.Error(w, "export query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	encErr := streamRowsAsText(ctx, w, flusher, rows, pipe)
	if encErr != nil {
		if !errors.Is(encErr, context.Canceled) && !errors.Is(encErr, context.DeadlineExceeded) {
			log.Printf("export_unsecure stream error: %v", encErr)
		}
		return
	}
}

func parseExportQuery(r *http.Request, maxRows int) (exportQuery, error) {
	v := r.URL.Query()

	q := exportQuery{
		Grep1:   v.Get("grep1"),
		Grep2:   v.Get("grep2"),
		Grep3:   v.Get("grep3"),
		Session: strings.TrimSpace(v.Get("session")),

		Order: strings.TrimSpace(v.Get("order")),
		Color: strings.TrimSpace(v.Get("color")),
	}

	if q.Order == "" {
		q.Order = "ingest_asc"
	}
	switch q.Color {
	case "", "never":
		q.Color = "never"
	case "always":
	default:
		return exportQuery{}, fmt.Errorf("invalid color=%q (use never|always)", q.Color)
	}

	limit := maxRows
	if limit <= 0 {
		limit = 200000
	}
	if s := strings.TrimSpace(v.Get("limit")); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			return exportQuery{}, fmt.Errorf("invalid limit=%q", s)
		}
		if n > limit {
			n = limit
		}
		limit = n
	}
	q.Limit = limit

	return q, nil
}

func (s *ExportService) queryExportRows(ctx context.Context, tenantID string, q exportQuery) (*sql.Rows, error) {
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

	debugPrint(log.Printf, levelDebug, "export sql=%s args=%s\n", sb.String(), safeJSON(args))

	return s.DB.SQL.QueryContext(ctx, sb.String(), args...)
}

func exportOrderSQL(order string) (string, error) {
	switch order {
	case "ingest_desc":
		return "ts_ingested desc, id desc", nil
	case "ingest_asc":
		return "ts_ingested asc, id asc", nil
	case "client_desc":
		return "ts_client desc nulls last, id desc", nil
	case "client_asc":
		return "ts_client asc nulls last, id asc", nil
	default:
		return "", fmt.Errorf("invalid order=%q (use ingest_desc|ingest_asc|client_desc|client_asc)", order)
	}
}

func streamRowsAsText(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, rows *sql.Rows, pipe *GrepPipeline) error {
	const flushEvery = 200
	n := 0

	for rows.Next() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var rawLine string
		if err := rows.Scan(&rawLine); err != nil {
			return err
		}

		line := strings.TrimRight(rawLine, "\r\n")
		if !pipe.Match(line) {
			continue
		}

		if pipe.ColorEnabled() {
			line = pipe.Highlight(line)
		}

		if _, err := io.WriteString(w, line+"\n"); err != nil {
			return err
		}

		n++
		if flusher != nil && (n%flushEvery) == 0 {
			flusher.Flush()
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}
	if flusher != nil {
		flusher.Flush()
	}
	return nil
}

func safeJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
