package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/google/uuid"
)

type Config struct {
	Server  ServerConfig `json:"server"`
	DB      DBConfig     `json:"db"`
	ACL     []ACL        `json:"acl"`
	Tenants []Tenant     `json:"tenants"`
	Globals Globals      `json:"globals"`
}

type ServerConfig struct {
	IngestClear ListenerConfig `json:"ingest_clear"`
	IngestTLS   ListenerConfig `json:"ingest_tls"`
	HTTP        HTTPConfig     `json:"http"`
	HTTPS       HTTPConfig     `json:"https"`
}

type ListenerConfig struct {
	Enabled bool       `json:"enabled"`
	Addr    string     `json:"addr"`
	Auth    []AuthMode `json:"auth"`
	Tenants []string   `json:"tenants"`
}

type HTTPConfig struct {
	Enabled bool       `json:"enabled"`
	Addr    string     `json:"addr"`
	Auth    []AuthMode `json:"auth"`
	Tenants []string   `json:"tenants"`
}

type AuthMode string

const (
	AuthNone   AuthMode = "none"
	AuthCert   AuthMode = "cert"
	AuthAPIKey AuthMode = "apikey"
)

func (a AuthMode) Valid() bool {
	switch strings.ToLower(string(a)) {
	case string(AuthNone), string(AuthCert), string(AuthAPIKey):
		return true
	default:
		return false
	}
}

type DBConfig struct {
	PostgresDSN string `json:"postgres_dsn"`
}

type ACL struct {
	ID    string    `json:"id"`
	Rules []ACLRule `json:"rules"`
}

type ACLRule struct {
	CIDR   string `json:"CIDR"`
	Action string `json:"action"` // "allow" / "deny"
	Name   string `json:"name"`
}

type Tenant struct {
	TenantID        string `json:"tenantID"`
	TenantName      string `json:"tenant_name"`
	ACL             string `json:"acl"`
	Cert            string `json:"cert"`
}

type Globals struct {
	Identity        Identity `json:"identity"`
	MaxLineBytes    int      `json:"max_line_bytes"`
	MaxRows         int      `json:"max_rows"`
	DefaultTenantID string   `json:"default_tenant_id"`
	MaxSeconds      int      `json:"max_seconds"`
}

type Identity struct {
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
}

func ReadConfig(cl CommandLine) (Config, error) {
	path := cl.ConfigPath
	if path == "" {
		path = "hc-config.json"
	}
	return ReadConfigFile(path)
}

func ReadConfigFile(path string) (Config, error) {
	var cfg Config

	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config file %q: %w", path, err)
	}

	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("parse config JSON %q: %w", path, err)
	}

	if err := cfg.validate(); err != nil {
		return Config{}, fmt.Errorf("invalid config %q: %w", path, err)
	}

	return cfg, nil
}

func (c Config) validate() error {
	// Globals
	if c.Globals.MaxLineBytes <= 0 {
		return errors.New("globals.max_line_bytes must be > 0")
	}
	if c.Globals.MaxRows <= 0 {
		return errors.New("globals.max_rows must be > 0")
	}
	if c.Globals.MaxSeconds <= 0 {
		return errors.New("globals.max_seconds must be > 0")
	}
	if c.Globals.Identity.CertFile == "" {
		return errors.New("globals.identity.cert_file is required")
	}
	if c.Globals.Identity.KeyFile == "" {
		return errors.New("globals.identity.key_file is required")
	}
	if c.Globals.DefaultTenantID == "" {
		return errors.New("globals.identity.DefaultTenantID is required")
	}

	// Build maps for cross-reference checks
	tenantIDs := make(map[string]struct{}, len(c.Tenants))
	for i, t := range c.Tenants {
		if t.TenantID == "" {
			return fmt.Errorf("tenants[%d].tenantID is required", i)
		}
		if !isValidUUID(t.TenantID) {
			return fmt.Errorf("tenants[%d].tenantID is not a valid UUID: %q", i, t.TenantID)
		}
		if _, dup := tenantIDs[t.TenantID]; dup {
			return fmt.Errorf("duplicate tenantID: %q", t.TenantID)
		}
		tenantIDs[t.TenantID] = struct{}{}
		if t.ACL == "" {
			return fmt.Errorf("tenants[%d].acl is required", i)
		}
		if t.Cert == "" {
			return fmt.Errorf("tenants[%d].cert is required", i)
		}
	}
	if len(c.Tenants) == 0 {
		return errors.New("tenants must not be empty")
	}

	aclIDs := make(map[string]struct{}, len(c.ACL))
	for i, a := range c.ACL {
		if a.ID == "" {
			return fmt.Errorf("acl[%d].id is required", i)
		}
		if _, dup := aclIDs[a.ID]; dup {
			return fmt.Errorf("duplicate acl id: %q", a.ID)
		}
		aclIDs[a.ID] = struct{}{}
		for j, r := range a.Rules {
			if r.CIDR == "" {
				return fmt.Errorf("acl[%d].rules[%d].CIDR is required", i, j)
			}
			if _, _, err := net.ParseCIDR(r.CIDR); err != nil {
				return fmt.Errorf("acl[%d].rules[%d].CIDR invalid (%q): %v", i, j, r.CIDR, err)
			}
			act := strings.ToLower(strings.TrimSpace(r.Action))
			if act != "allow" && act != "deny" {
				return fmt.Errorf("acl[%d].rules[%d].action must be allow|deny, got %q", i, j, r.Action)
			}
		}
	}

	// Tenant -> ACL reference exists
	for i, t := range c.Tenants {
		if _, ok := aclIDs[t.ACL]; !ok {
			return fmt.Errorf("tenants[%d] references unknown acl id %q", i, t.ACL)
		}
	}

	// Server listener validation + referenced tenants exist
	if err := validateListener("server.ingest_clear", c.Server.IngestClear, tenantIDs); err != nil {
		return err
	}
	if err := validateListener("server.ingest_tls", c.Server.IngestTLS, tenantIDs); err != nil {
		return err
	}
	if err := validateHTTP("server.http", c.Server.HTTP, tenantIDs); err != nil {
		return err
	}
	if err := validateHTTP("server.https", c.Server.HTTPS, tenantIDs); err != nil {
		return err
	}

	// DB
	if strings.TrimSpace(c.DB.PostgresDSN) == "" {
		return errors.New("db.postgres_dsn is required")
	}

	return nil
}

func validateListener(name string, l ListenerConfig, tenantIDs map[string]struct{}) error {
	if !l.Enabled {
		return nil
	}
	if strings.TrimSpace(l.Addr) == "" {
		return fmt.Errorf("%s.addr is required when enabled", name)
	}
	if len(l.Tenants) == 0 {
		return fmt.Errorf("%s.tenants must not be empty when enabled", name)
	}
	for i, id := range l.Tenants {
		if !isValidUUID(id) {
			return fmt.Errorf("%s.tenants[%d] is not a valid UUID: %q", name, i, id)
		}
		if _, ok := tenantIDs[id]; !ok {
			return fmt.Errorf("%s.tenants[%d] references unknown tenantID %q", name, i, id)
		}
	}
	return nil
}

func validateHTTP(name string, h HTTPConfig, tenantIDs map[string]struct{}) error {
	if !h.Enabled {
		return nil
	}
	if strings.TrimSpace(h.Addr) == "" {
		return fmt.Errorf("%s.addr is required when enabled", name)
	}
	if len(h.Tenants) == 0 {
		return fmt.Errorf("%s.tenants must not be empty when enabled", name)
	}
	for i, id := range h.Tenants {
		if !isValidUUID(id) {
			return fmt.Errorf("%s.tenants[%d] is not a valid UUID: %q", name, i, id)
		}
		if _, ok := tenantIDs[id]; !ok {
			return fmt.Errorf("%s.tenants[%d] references unknown tenantID %q", name, i, id)
		}
	}

	// auth list
	if len(h.Auth) == 0 {
		return fmt.Errorf("%s.auth must not be empty when enabled", name)
	}
	for i, a := range h.Auth {
		if !a.Valid() {
			return fmt.Errorf("%s.auth[%d] invalid: %q (allowed: none|cert|apikey)", name, i, a)
		}
	}
	return nil
}

func isValidUUID(u string) bool {
	_, err := uuid.Parse(u)
	return err == nil
}
