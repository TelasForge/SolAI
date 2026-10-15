// Command solai is the SolAI session runtime.
//
// It reads a session spec from stdin, takes its configuration from the
// environment (injected by the surrounding platform), and streams session
// events as JSON lines on stdout. It is launched by the control plane
// (solaid); it is not a command-line tool.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/melodyogonna/solai/config"
	"github.com/melodyogonna/solai/runtime"
	"github.com/melodyogonna/solai/session"
)

func main() {
	initLogger()

	var spec session.Spec
	if err := json.NewDecoder(os.Stdin).Decode(&spec); err != nil {
		fatal("reading session spec: " + err.Error())
	}
	if len(spec.Goals) == 0 {
		fatal("session spec has no goals")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	emit := session.NewLineEmitter(os.Stdout)
	if err := runtime.Run(ctx, config.FromEnv(), spec, emit); err != nil {
		slog.Error("session failed", "err", err)
		os.Exit(1)
	}
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "solai: "+msg)
	os.Exit(1)
}

// initLogger configures the global slog logger. The level is controlled by the
// SOLAI_LOG_LEVEL environment variable (debug, info, warn, error; default: info).
func initLogger() {
	level := slog.LevelInfo
	switch strings.ToLower(os.Getenv("SOLAI_LOG_LEVEL")) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
}
