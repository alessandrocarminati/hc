package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func doRunServe(version string, args []string) {
	opts, err := getRuntimeConf(version, args)
	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}

	debugPrint(log.Printf, levelDebug, "Start serving\n")

	serve(opts)
}

func serve(opts *Options) {
	debugPrint(log.Printf, levelCrazy, "Args=%v\n", opts)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ing, err := SetupIngestion(ctx, opts)
	if err != nil {
		log.Fatalf("failed to start ingestion: %v", err)
	}
	defer ing.Stop()

	if ing.db != nil {
		// HTTP
		if opts.Cfg.Server.HTTP.Enabled {
			mux := http.NewServeMux()
			RegisterExportHandlers(mux, opts, ing.db)
			httpSrv := &http.Server{
				Addr:    opts.Cfg.Server.HTTP.Addr,
				Handler: mux,
			}
			go func() {
				debugPrint(log.Printf, levelInfo, "HTTP export listening on %s", httpSrv.Addr)
				if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					debugPrint(log.Printf, levelError, "HTTP server error: %v", err)
					cancel()
				}
			}()
		}

		// HTTPS
		if opts.Cfg.Server.HTTPS.Enabled {
			var tlsConfig *tls.Config

			muxHTTPS := http.NewServeMux()
			RegisterExportHandlers(muxHTTPS, opts, ing.db)

			debugPrint(log.Printf, levelDebug, "going to use %s to authenticate client certificates", opts.Cfg.Globals.ClientCert)
			caCert, err := os.ReadFile(opts.Cfg.Globals.ClientCert)
			if err == nil {
				debugPrint(log.Printf, levelDebug, "using %s to authenticate client certificates", opts.Cfg.Globals.ClientCert)
				caCertPool := x509.NewCertPool()
				caCertPool.AppendCertsFromPEM(caCert)

				tlsConfig = &tls.Config{
					ClientCAs:  caCertPool,
					ClientAuth: tls.VerifyClientCertIfGiven,
				}
			}

			httpsSrv := &http.Server{
				Addr:      opts.Cfg.Server.HTTPS.Addr,
				Handler:   muxHTTPS,
				TLSConfig: tlsConfig,
			}

			cert := strings.TrimSpace(opts.Cfg.Globals.Identity.CertFile)
			key := strings.TrimSpace(opts.Cfg.Globals.Identity.KeyFile)
			if cert == "" || key == "" {
				debugPrint(log.Printf, levelWarning, "HTTPS enabled but tls.cert_file/key_file not set; HTTPS server not started")
			} else {
				go func() {
					debugPrint(log.Printf, levelInfo, "HTTPS export listening on %s", httpsSrv.Addr)
					if err := httpsSrv.ListenAndServeTLS(cert, key); err != nil && err != http.ErrServerClosed {
						debugPrint(log.Printf, levelError, "HTTPS server error: %v", err)
						cancel()
					}
				}()
			}
		}

	} else {
		debugPrint(log.Printf, levelWarning, "DB not available. only ingestion service\n")
	}

	waitForShutdown(cancel)

	debugPrint(log.Printf, levelDebug, "exit\n")
}

func waitForShutdown(cancel context.CancelFunc) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	cancel()
}
