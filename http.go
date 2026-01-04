package main

import (
	"encoding/json"
	"html/template"
	"net/http"
	"log"
	"strings"
	"time"
	"context"
)

type PageData struct {
	Title   string
	LogTree *LogTree
}

func http_present(h *History, opts *Options) {
	debugPrint(log.Printf, levelCrazy, "Args=%v, %v\n", h, opts)
	logTree, err := buildLogTree(h.RawLog)
	if err != nil {
		log.Fatalf("failed to build log tree: %v", err)
	}
	h.LogTree = logTree

	tpl, err := template.New("webpage").Parse(tmplStr)
	if err != nil {
		log.Fatalf("failed to parse template: %v", err)
	}

	var db *DB
	if strings.TrimSpace(opts.Cfg.DB.PostgresDSN) != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		db, err = OpenDB(ctx, opts.Cfg.DB.PostgresDSN)
		if err != nil {
			log.Printf("warning: failed to connect to db (export endpoints will be unavailable): %v", err)
			db = nil
		} else {
			defer db.Close()
		}
	} else {
		log.Printf("warning: db.postgres_dsn not set (export endpoints will be unavailable)")
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		data := PageData{
			Title:   opts.Verstr,
			LogTree: logTree,
		}
		if err := tpl.Execute(w, data); err != nil {
			log.Printf("template execute error: %v", err)
		}
	})

	mux.HandleFunc("/logs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(logTree)
	})

	mux.HandleFunc("/export_unsecure", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.TLS != nil {
			http.Error(w, "this endpoint is HTTP-only", http.StatusBadRequest)
			return
		}
		if !opts.Cfg.Export.Enabled || !opts.Cfg.Export.Unsecure.Enabled {
			http.Error(w, "export_unsecure disabled", http.StatusNotFound)
			return
		}
		if db == nil {
			http.Error(w, "database not available", http.StatusServiceUnavailable)
			return
		}
		tenantID := strings.TrimSpace(opts.Cfg.Export.Unsecure.TenantID)
		if tenantID == "" {
			http.Error(w, "export_unsecure misconfigured: tenant_id missing", http.StatusInternalServerError)
			return
		}

		maxRows := opts.Cfg.Export.MaxRows
		if maxRows <= 0 {
			maxRows = 200000
		}
		maxSeconds := opts.Cfg.Export.MaxSeconds
		if maxSeconds <= 0 {
			maxSeconds = 30
		}

		ctx, cancel := context.WithTimeout(r.Context(), time.Duration(maxSeconds)*time.Second)
		defer cancel()

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")

		flusher, _ := w.(http.Flusher)

		limit := maxRows
		if err := db.StreamExport(ctx, w, flusher, StreamExportOptions{TenantID: tenantID, Limit:limit, BatchSize: 500}); err != nil {
			http.Error(w, "export failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
	})

	mux.HandleFunc("/export_cidr", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.TLS != nil {
			http.Error(w, "this endpoint is HTTP-only", http.StatusBadRequest)
			return
		}
		http.Error(w, "export_cidr not implemented", http.StatusNotImplemented)
	})

	mux.HandleFunc("/export_hmac", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.TLS != nil {
			http.Error(w, "this endpoint is HTTP-only", http.StatusBadRequest)
			return
		}
		http.Error(w, "export_hmac not implemented", http.StatusNotImplemented)
	})

	mux.HandleFunc("/export_ssl", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.TLS == nil {
			http.Error(w, "this endpoint is HTTPS-only", http.StatusBadRequest)
			return
		}
		http.Error(w, "export_ssl not implemented", http.StatusNotImplemented)
	})

	lstAddr := opts.Cfg.Server.HTTP.Addr
	if strings.TrimSpace(lstAddr) == "" {
		lstAddr = ":8080"
	}

	log.Println("Listening on: " + lstAddr)
	if err := http.ListenAndServe(lstAddr, mux); err != nil {
		log.Fatalf("http server failed: %v", err)
	}
}

