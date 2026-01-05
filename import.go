package main

import (
	"fmt"
	"context"
	"os"
	"log"
)

func doImport(version string, args []string) {

	debugPrint(log.Printf, levelCrazy, "Args=%s, %v\n", version, args)
	opts, err := getRuntimeConf(version, args)
	if err != nil {
		fmt.Printf("%v", err)
		os.Exit(1)
	}

	debugPrint(log.Printf, levelDebug, "check config consistency\n")

	if opts.Cfg.Tenancy.DefaultTenantID == "" {
		fmt.Fprintln(os.Stderr, "DefaultTenantID is required when using -import")
		os.Exit(2)
	}

	if opts.Cfg.DB.PostgresDSN == "" {
		fmt.Fprintln(os.Stderr, "postgres_dsn must be set in config for import")
		os.Exit(2)
	}

	debugPrint(log.Printf, levelDebug, "connecting db\n")

	ctx := context.Background()

	db, err := OpenDB(ctx, opts.Cfg.DB.PostgresDSN)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer db.Close()

	debugPrint(log.Printf, levelDebug, "make sure schema exists\n")
	if err := db.EnsureSchema(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	debugPrint(log.Printf, levelDebug, "fetch TenantName\n")
	TenantName, exists, err := db.GetTenantName(ctx, opts.Cfg.Tenancy.DefaultTenantID)
	if err != nil || !exists{
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	debugPrint(log.Printf, levelDebug, "EnsureTenant\n")
	if err := db.EnsureTenant(ctx, opts.Cfg.Tenancy.DefaultTenantID, TenantName); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	debugPrint(log.Printf, levelDebug, "populating\n")
	inserted, skipped, err := db.ImportHistoryFile(
		ctx,
		opts.Cfg.Tenancy.DefaultTenantID,
		opts.LegacyHistoryFile,
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Printf(
		"Import completed: inserted=%d skipped=%d\n",
		inserted,
		skipped,
	)
}
