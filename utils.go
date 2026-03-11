package main

import (
	"database/sql"
	"fmt"
	"github.com/google/uuid"
	"log"
	"strings"
	"time"
	"unicode/utf8"
)

type Options struct {
	Cfg               Config
	LogLevel          DebugLevels
	LegacyHistoryFile string
	AKTenantID        uuid.UUID
	AKUserID          uuid.UUID
	Verstr            string
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

func getRuntimeConf(version string, args []string) (*Options, error) {
	debugPrint(log.Printf, levelCrazy, "Args=%s, %v\n", version, args)
	cl, err := ParseCommandLine(args)
	if err != nil {
		return nil, err
	}

	DebugLevel = cl.LogLevel.Value

	debugPrint(log.Printf, levelDebug, "reading config file %s\n", cl.ConfigPath)
	cfg, err := ReadConfig(cl)
	if err != nil {
		return nil, err
	}

	debugPrint(log.Printf, levelDebug, "Build Runtime conf\n")
	opts, err := ResolveOptions(cfg, cl, version)
	if err != nil {
		return nil, err
	}

	return opts, nil
}

func ResolveOptions(cfg Config, cl CommandLine, verstr string) (*Options, error) {
	o := Options{
		Cfg:      cfg,
		LogLevel: cl.LogLevel,
	}

	err := cfg.validate()
	if err != nil {
		return nil, err
	}

	o.LogLevel = cl.LogLevel
	o.Verstr = verstr
	o.LegacyHistoryFile = cl.HistoryFile
	o.AKUserID = cl.AKUserID
	o.AKTenantID = cl.AKTenantID
	return &o, nil
}

func sanitizeUTF8(s string) string {
	debugPrint(log.Printf, levelCrazy, "Args=%s\n", s)
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, "�")
}

func shortSQL(s string) string {
	debugPrint(log.Printf, levelCrazy, "Args=%s\n", s)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
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

func formatExportLine(tsClient sql.NullTime, tsIngested time.Time, sessionID sql.NullString, host sql.NullString, cwd sql.NullString, cmd sql.NullString, raw string) string {
	debugPrint(log.Printf, levelCrazy, "Args=%v, %v, %v, %v, %v, %s, %s\n", tsClient, tsIngested, sessionID, host, cwd, cmd, raw)
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
