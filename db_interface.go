package main

import (
	"context"
        "io"
        "net/http"
	"github.com/google/uuid"
)

type DBInterface interface {
	EnsureSchema(ctx context.Context) error
	EnsureTenant(ctx context.Context, tenantID, name string) error
	GetTenantName(ctx context.Context, tenantID string) (string, bool, error)
	ImportHistoryFile(ctx context.Context, tenantID, path string) (inserted int, skipped int, err error)
	StreamExport(ctx context.Context, w io.Writer, flusher http.Flusher, opt StreamExportOptions) error
	MaxSeq(ctx context.Context, tenantID string) (int64, error)
	InsertEventWithSeq(ctx context.Context, ev Event, seq int64) error
	lookupTenantByUsername(username string) (string, bool)
	insertAPIKey(ctx context.Context, id uuid.UUID, tenant uuid.UUID, user *uuid.UUID, keyID, keyHash string) error
}
