// Command solaid is the SolAI control plane: it creates and supervises agent
// sessions and exposes them over an HTTP/SSE API.
//
// It is sandbox-unaware. Each session is launched through a configurable
// command (--launch, or SOLAI_LAUNCH_CMD); point that at your own
// infrastructure to isolate sessions.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/melodyogonna/solai/config"
	"github.com/melodyogonna/solai/controlplane"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8787", "HTTP listen address")
	launch := flag.String("launch", envOr("SOLAI_LAUNCH_CMD", "solai"),
		"runtime launch command (receives the session spec on stdin, emits events on stdout)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mgr := controlplane.NewManager(ctx, controlplane.ExecLauncher{
		Command: *launch,
		Stderr:  os.Stderr,
	})
	handler := controlplane.NewServer(mgr, config.ToolsDir()).Handler()

	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	slog.Info("solaid listening", "addr", *addr, "launch", *launch)

	select {
	case <-ctx.Done():
		slog.Info("shutting down")
	case err := <-errCh:
		slog.Error("server error", "err", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
