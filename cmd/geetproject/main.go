package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	geetproject "github.com/andrecastillo/geetproject"
	"github.com/andrecastillo/geetproject/internal/api"
	"github.com/andrecastillo/geetproject/internal/cli"
	"github.com/andrecastillo/geetproject/internal/store"
)

const usage = cli.Usage + `
Server environment:
  GEETPROJECT_ADDR   listen address (default :8080)
  GEETPROJECT_DB     path to the SQLite file (default ./geet.db, /data/geet.db in the container)
  GEETPROJECT_URL    server the CLI talks to (default http://localhost:8080)
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		log.SetFlags(log.LstdFlags | log.Lmsgprefix)
		log.SetPrefix("geetproject: ")
		if err := serve(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		// Everything else is a CLI command against a running server.
		if err := cli.Run(os.Args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				fmt.Print(usage)
				os.Exit(2)
			}
			fmt.Fprintf(os.Stderr, "geetproject: %v\n", err)
			os.Exit(1)
		}
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	// Accept the pre-rename GEET_* names, loudly. Without this, anything still
	// passing GEET_DB to a binary that only reads GEETPROJECT_DB would fall back
	// to the default path and quietly start an empty database - which looks
	// exactly like data loss rather than a misconfiguration.
	if suffix, ok := strings.CutPrefix(key, "GEETPROJECT_"); ok {
		legacy := "GEET_" + suffix
		if v := os.Getenv(legacy); v != "" {
			fmt.Fprintf(os.Stderr, "geetproject: %s is deprecated, use %s\n", legacy, key)
			return v
		}
	}
	return fallback
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", envOr("GEETPROJECT_ADDR", ":8080"), "listen address")
	dbPath := fs.String("db", envOr("GEETPROJECT_DB", "geet.db"), "path to the SQLite file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if dir := filepath.Dir(*dbPath); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create data dir: %w", err)
		}
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	srv := &http.Server{
		Addr:              *addr,
		Handler:           logRequests(api.New(st, geetproject.WebHandler())),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Shut down cleanly so SQLite gets to checkpoint its WAL rather than being
	// killed mid-write when the container stops.
	idle := make(chan struct{})
	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
		<-sigs
		log.Print("shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("shutdown: %v", err)
		}
		close(idle)
	}()

	log.Printf("listening on %s (db %s)", *addr, *dbPath)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-idle
	return nil
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(rec, r)
		if r.URL.Path != "/healthz" {
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, rec.code, time.Since(start).Round(time.Millisecond))
		}
	})
}

type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.code = code
	s.ResponseWriter.WriteHeader(code)
}
