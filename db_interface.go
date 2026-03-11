package main

import (
	"context"
	"database/sql"
	"github.com/google/uuid"
	"strings"
	"log"
)

type APIKeyRecord struct {
	TenantID string
	KeyHash  string
	Revoked  sql.NullTime
}

type DBInterface interface {
	EnsureSchema(ctx context.Context) error
	EnsureTenant(ctx context.Context, tenantID, name string) error
	GetTenantName(ctx context.Context, tenantID string) (string, bool, error)
	ImportHistoryFile(ctx context.Context, tenantID, path string) (inserted int, skipped int, err error)
	MaxSeq(ctx context.Context, tenantID string) (int64, error)
	InsertEventWithSeq(ctx context.Context, ev Event, seq int64) error
	lookupTenantByUsername(username string) (string, bool)
	ExportLines(ctx context.Context, tenantID string, q exportQuery) ([]string, error)
	insertAPIKey(ctx context.Context, id uuid.UUID, tenant uuid.UUID, user *uuid.UUID, keyID, keyHash string) error
	RequireTenantExists(ctx context.Context, tenantID uuid.UUID) error
	GetAPIKeyByKeyID(ctx context.Context, keyID string) (APIKeyRecord, bool, error)
	Close() error
}

func OpenDB(ctx context.Context, dsn string) (DBInterface, error) {
	if looksLikePostgresDSN(dsn) {
		debugPrint(log.Printf, levelInfo, "Database backend Postgres\n")
		return OpenPgsqlDB(ctx, dsn)
	}
	debugPrint(log.Printf, levelInfo, "Database backend sqlite3\n")
	return OpenSQLiteDB(ctx, dsn)
}

func looksLikePostgresDSN(dsn string) bool {
	return strings.Contains(dsn, "host=") ||
		strings.Contains(dsn, "user=") ||
		strings.Contains(dsn, "dbname=") ||
		strings.Contains(dsn, "sslmode=")
}
