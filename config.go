package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	Server          ServerConfig  `json:"server"`
	Parser          ParserConfig  `json:"parser"`
	DB              DBConfig      `json:"db"`
	HistoryFile     string        `json:"history_file"`
	Tenancy         TenancyConfig `json:"tenancy"`
	TLS             TLSConfig     `json:"tls"`
	Limits          LimitsConfig  `json:"limits"`
	Export          ExportConfig  `json:"Export"`
}

type ServerConfig struct {
	ListnerClear    ListenerConfig `json:"listner_clear"`
	ListnerTLS      ListenerConfig `json:"listner_tls"`
	ListnerSearch   ListenerConfig `json:"listner_search"`
	HTTP            ListenerConfig `json:"http"`
	HTTPS           ListenerConfig `json:"https"`
}

type ListenerConfig struct {
	Enabled         bool   `json:"enabled"`
	Addr            string `json:"addr"`
}

type ParserConfig struct {
	TagsFile        string `json:"tags_file"`
	RegexPrefix     string `json:"regex_prefix"`
}

type DBConfig struct {
	PostgresDSN     string `json:"postgres_dsn"`
}

type TenancyConfig struct {
	DefaultTenantID string            `json:"default_tenant_id"`
	TrustedSources  []TrustedSource   `json:"trusted_sources,omitempty"`
	Extra           map[string]string `json:"extra,omitempty"` // Currently not used
}

type TrustedSource struct {
	CIDR            string `json:"cidr"`
	TenantID        string `json:"tenant_id"`
	Note            string `json:"note,omitempty"`
}

type TLSConfig struct {
	CertFile        string `json:"cert_file"`
	KeyFile         string `json:"key_file"`
}

type LimitsConfig struct {
	MaxLineBytes    int `json:"max_line_bytes"`
}

type ExportEndpointConfig struct {
	Enabled bool   `json:"Enabled"`
	TenantID string `json:"TenantID"`
}

type ExportConfig struct {
	Enabled bool `json:"Enabled"`

	Unsecure ExportEndpointConfig `json:"Unsecure"`
	CIDR     ExportEndpointConfig `json:"CIDR"`
	HMAC     ExportEndpointConfig `json:"HMAC"`

	SSL ExportEndpointConfig `json:"SSL"`

	MaxRows    int `json:"MaxRows"`
	MaxSeconds int `json:"MaxSeconds"`
}

func DefaultConfig() Config {
	return Config{
		Server: ServerConfig{
			ListnerClear:   ListenerConfig{Enabled: true, Addr: ":12345"},
			ListnerTLS:     ListenerConfig{Enabled: false, Addr: ":12346"},
			ListnerSearch:  ListenerConfig{Enabled: true, Addr: ":12347"},
			HTTP:           ListenerConfig{Enabled: true, Addr: ":8080"},
		},
		Parser: ParserConfig{
			TagsFile:      "tags.json",
			RegexPrefix:   "",
		},
		DB: DBConfig{
			PostgresDSN: "",
		},
		Tenancy: TenancyConfig{
			DefaultTenantID: "",
			TrustedSources:  nil,
		},
		TLS: TLSConfig{
			CertFile: "",
			KeyFile:  "",
		},
		Limits: LimitsConfig{
			MaxLineBytes: 64 * 1024,
		},
	}
}

func ReadConfig(cl CommandLine) (Config, error) {
	if cl.ConfigPath == "" {
		return DefaultConfig(), nil
	}
	return ReadConfigFile(cl.ConfigPath)
}

func ReadConfigFile(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}

	cfg := DefaultConfig()

	dec := json.NewDecoder(bytesReader(b))
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	if dec.More() {
		// for safety
	}
	if err := dec.Decode(&struct{}{}); err == nil {
		return Config{}, fmt.Errorf("parse config %q: unexpected extra JSON content", path)
	}

	return cfg, nil
}

func bytesReader(b []byte) *byteReader { return &byteReader{b: b} }

type byteReader struct {
	b []byte
	i int64
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.i >= int64(len(r.b)) {
		return 0, os.ErrClosed // will be treated as EOF by json decoder? no.
	}
	n := copy(p, r.b[r.i:])
	r.i += int64(n)
	return n, nil
}
