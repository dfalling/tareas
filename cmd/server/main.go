// Command server is the tareas web application entry point.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	// Embedded timezone database: the distroless runtime image has no
	// zoneinfo on disk, and APP_TIMEZONE must work there.
	_ "time/tzdata"

	"github.com/dfalling/tareas/internal/api"
	"github.com/dfalling/tareas/internal/db"
	"github.com/dfalling/tareas/internal/ratelimit"
	"github.com/dfalling/tareas/internal/tokens"
	"github.com/dfalling/tareas/internal/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	addr := envOr("LISTEN_ADDR", ":8080")
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	loc, err := time.LoadLocation(envOr("APP_TIMEZONE", "UTC"))
	if err != nil {
		return fmt.Errorf("load APP_TIMEZONE: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, dsn); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	session := web.NewSessionManager(pool, envOrBool("COOKIE_SECURE", true))
	app := web.New(pool, session, envOr("APP_LOCALE", "en"), loc)

	// The API must never see session cookies and the web UI must never see
	// bearer tokens, so each front-end keeps its own mux: the API is mounted
	// first (the more specific prefix), everything else falls to the web UI.
	apiApp := api.New(pool, loc, ratelimit.New(10, 5*time.Minute))
	root := http.NewServeMux()
	root.Handle("/api/v1/", apiApp.Routes())
	root.Handle("/", app.Routes())

	// Hygiene: purge expired API tokens hourly. (scs purges its own
	// sessions.) It exits with the shutdown signal.
	tokenStore := tokens.NewStore(pool)
	go purgeExpiredTokens(ctx, tokenStore)

	srv := &http.Server{
		Addr:              addr,
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return nil
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// purgeExpiredTokens deletes API tokens past their expires_at on an hourly
// ticker until the process is asked to shut down.
func purgeExpiredTokens(ctx context.Context, store *tokens.Store) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := store.PurgeExpired(ctx)
			if err != nil {
				slog.Error("purge expired api tokens", "err", err)
				continue
			}
			if n > 0 {
				slog.Info("purged expired api tokens", "count", n)
			}
		}
	}
}

func envOrBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}
