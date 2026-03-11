package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type ExportService struct {
	Opts *Options
	DB   DBInterface
}

func RegisterExportHandlers(mux *http.ServeMux, opts *Options, db DBInterface) {
	s := &ExportService{Opts: opts, DB: db}

	mux.HandleFunc("/export", s.handleExport)

	mux.HandleFunc("/web_app", func(w http.ResponseWriter, r *http.Request) {
		debugPrint(log.Printf, levelInfo, "Wip /web_app page reached!\n")
		http.Error(w, "work in progress", http.StatusNotImplemented)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		debugPrint(log.Printf, levelInfo, "not implemented or planned page reached!\n")
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
	Key   string
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

func (s *ExportService) getTenantFromHTTPAPI(msg *http.Request) string {
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

	rec, ok, err := s.DB.GetAPIKeyByKeyID(ctx, keyID)

	if err != nil || !ok {
		return ""
	}

	if rec.Revoked.Valid {
		debugPrint(log.Printf, levelDebug, "key explicitly revoked\n")
		return ""
	}

	debugPrint(log.Printf, levelDebug, "Verify secret\n")
	pepper := strings.TrimSpace(s.Opts.Cfg.Globals.Pepper)
	if !verifySecretSHA256(secret, pepper, rec.KeyHash) {
		return ""
	}

	debugPrint(log.Printf, levelDebug, "SUCCESS: authenticated, tenant resolved\n")
	return rec.TenantID
}

func (s *ExportService) getTenantFromHTTPSCert(r *http.Request) string {
	if r == nil || r.TLS == nil {
		return ""
	}

	if len(r.TLS.VerifiedChains) == 0 {
		return ""
	}

	if len(r.TLS.PeerCertificates) == 0 {
		return ""
	}

	cert := r.TLS.PeerCertificates[0]
	cn := strings.TrimSpace(cert.Subject.CommonName)
	if cn == "" {
		return ""
	}
	debugPrint(log.Printf, levelDebug, "Authenticated as: %s", cert.Subject.CommonName)

	tenantID, ok := s.DB.lookupTenantByUsername(cn)
	if !ok {
		return ""
	}

	debugPrint(log.Printf, levelDebug, "SUCCESS: authenticated, tenant resolved\n")
	return tenantID
}

func (s *ExportService) getTenant(msg *http.Request) string {
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
			t := s.getTenantFromHTTPSCert(msg)
			if t != "" {
				return t
			}

		default:
			debugPrint(log.Printf, levelWarning, "Warning unsupported auth method in the list\n")
		}
	}
	return ""
}

func (s *ExportService) handleExport(w http.ResponseWriter, r *http.Request) {
	debugPrint(log.Printf, levelCrazy, "Args=%v, %v\n", w, r)
	if r.Method != http.MethodGet {
		debugPrint(log.Printf, levelInfo, "not allowed method request form %s\n", getIP(r))
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !s.Opts.Cfg.Server.IngestClear.Enabled {
		http.Error(w, "export disabled", http.StatusNotFound)
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
		http.Error(w, "export no default tenantID", http.StatusInternalServerError)
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

	lines, err := s.DB.ExportLines(ctx, tenantID, q)
	if err != nil {
		log.Printf("export query failed: %v", err)
		http.Error(w, "export query failed", http.StatusInternalServerError)
		return
	}

	textLines, err := linesToText(ctx, lines, pipe, q.Key)
	if err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			log.Printf("export stream error: %v", err)
		}
		return
	}

	for _, line := range textLines {
		if _, err := io.WriteString(w, line+"\n"); err != nil {
			return
		}
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
		Key:   strings.TrimSpace(v.Get("key")),
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

func linesToText(ctx context.Context, lines []string, pipe *GrepPipeline, key string) ([]string, error) {
	out := make([]string, 0, len(lines))

	for _, rawLine := range lines {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		line := strings.TrimRight(rawLine, "\r\n")
		if !pipe.Match(line) {
			continue
		}

		if key != "" {
			privKey, err := base64.StdEncoding.DecodeString(key)
			if err == nil {
				decr, err := decryptString(line, privKey)
				if err == nil {
					line = decr
				}
			}
		}

		if pipe.ColorEnabled() {
			line = pipe.Highlight(line)
		}

		out = append(out, line)
	}

	return out, nil
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

/*
	func streamRowsAsText(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, rows *sql.Rows, pipe *GrepPipeline, key string) error {
		const flushEvery = 200
		n := 0

		debugPrint(log.Printf, levelCrazy, "processing text with key='%s'\n", key)

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

			if key != "" {
				for {
					debugPrint(log.Printf, levelCrazy, "Decrypt '%s' using '%s'\n", line, key)
					PrivKey, err := base64.StdEncoding.DecodeString(key)
					if err != nil {
						debugPrint(log.Printf, levelWarning, "Ecryption key does not work(%v), fallback unencrypted.\n", err)
						break
					}
					decr, err := decryptString(line, PrivKey)
					if err != nil {
						debugPrint(log.Printf, levelWarning, "Ecryption key can not decrypt(%v), fallback unencrypted.\n", err)
						break
					}
					line = decr
					break
				}
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
*/
func safeJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
