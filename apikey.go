package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
)

func doRunAPIKeyCreate(version string, args []string) {
        opts, err := getRuntimeConf(version, args)
        if err != nil {
                fmt.Printf("%v\n", err)
                os.Exit(1)
        }

        debugPrint(log.Printf, levelDebug, "Api key creating\n")

        err = CreateAPIKey(opts)
	if err != nil {
		 debugPrint(log.Printf, levelError, "Error: %v\n", err)
	}
}


func CreateAPIKey(opts *Options) error {
	debugPrint(log.Printf, levelCrazy, "opts=%v\n", opts)



	ctx := context.Background()

	debugPrint(log.Printf, levelDebug, "connect db\n")
        db, err := OpenDB(ctx, opts.Cfg.DB.PostgresDSN)

	if err != nil {
		return err
	}
	defer db.SQL.Close()

	debugPrint(log.Printf, levelDebug, "perform checks: user=%s, tenant=%s\n", opts.AKUserID.String(), opts.AKTenantID.String())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := ensureTenantExists(ctx, db.SQL, opts.AKTenantID); err != nil {
		return err
	}

	keyID, err := generateKeyID()
	if err != nil {
		return err
	}
	secret, err := generateSecret()
	if err != nil {
		return err
	}

	debugPrint(log.Printf, levelDebug, "calculate hashes from %s, %s\n", keyID, secret)
	pepper := strings.TrimSpace(opts.Cfg.Globals.Pepper)
	keyHash := hashSecretSHA256(secret, pepper)

	debugPrint(log.Printf, levelDebug, "insert into db\n")
	id := uuid.New()
	if err := insertAPIKey(ctx, db.SQL, id, opts.AKTenantID, &opts.AKUserID, keyID, keyHash); err != nil {
		if isUniqueViolation(err) {
			for i := 0; i < 3; i++ {
				keyID, _ = generateKeyID()
				if err2 := insertAPIKey(ctx, db.SQL, id, opts.AKTenantID, &opts.AKUserID, keyID, keyHash); err2 == nil {
					goto PRINT
				} else if !isUniqueViolation(err2) {
					return err2
				}
			}
			return fmt.Errorf("apikey: failed to generate unique key_id after retries: %w", err)
		}
		return err
	}

PRINT:
	apiKey := keyID + "." + secret
	fmt.Printf("tenant_id: %s\n", opts.AKTenantID.String())
	if &opts.AKUserID != nil {
		fmt.Printf("user_id:   %s\n", opts.AKUserID.String())
	}
	fmt.Printf("key_id:    %s\n", keyID)
	fmt.Printf("api_key:   %s\n", apiKey)
	fmt.Println("note: api_key is shown only now; store it safely.")
	return nil
}

func insertAPIKey(ctx context.Context, db *sql.DB, id uuid.UUID, tenant uuid.UUID, user *uuid.UUID, keyID, keyHash string) error {
	debugPrint(log.Printf, levelDebug, "insert into api_keys values ('%s', '%s', '%s', '%s', '%s', %s));\n", id.String(), tenant.String(), user.String(), keyID, keyHash, "1234")
	_, err := db.ExecContext(ctx, `
		insert into api_keys (id, tenant_id, user_id, key_id, key_hash)
		values ($1,$2,$3,$4,$5)
	`, id, tenant, user, keyID, keyHash)
	return err
}

func ensureTenantExists(ctx context.Context, db *sql.DB, tenant uuid.UUID) error {
	var tmp uuid.UUID
	err := db.QueryRowContext(ctx, `select id from tenants where id = $1`, tenant).Scan(&tmp)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("apikey: tenant %s not found in tenants table", tenant.String())
	}
	return err
}

func generateKeyID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "hc_" + hex.EncodeToString(b), nil
}

func generateSecret() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashSecretSHA256(secret, pepper string) string {
	h := sha256.New()
	h.Write([]byte(secret))
	h.Write([]byte(":"))
	h.Write([]byte(pepper))
	return hex.EncodeToString(h.Sum(nil))
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "duplicate key value") || strings.Contains(s, "unique constraint")
}
