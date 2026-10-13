package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/melodyogonna/solai/ipc"
)

// RunTool writes input.json to the IPC directory, spawns the tool subprocess,
// waits for it to exit, then reads output.json.
//
// Completion is signalled by a valid output.json, not by the exit code alone:
// a tool may exit nonzero and still report a structured error. A missing or
// malformed output.json is an infrastructure error carrying the stderr tail.
//
// The process is killed if ctx is cancelled or the per-tool timeout elapses.
func RunTool(ctx context.Context, dir, executable string, input ipc.Input, timeout time.Duration, extraEnv []string, ipcDir string) (ipc.Output, error) {
	if err := ipc.WriteInputTo(ipcDir, input); err != nil {
		return ipc.Output{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Resolve to an absolute path so exec finds the binary regardless of the
	// agent's current working directory (cmd.Dir only affects the child
	// process's working directory, not the path lookup for the executable).
	absExec := filepath.Join(dir, executable)
	cmd := exec.CommandContext(ctx, absExec)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), extraEnv...)

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return ipc.Output{}, fmt.Errorf("starting tool: %w", err)
	}
	slog.Debug("tool process started", "executable", executable, "pid", cmd.Process.Pid)

	waitErr := cmd.Wait()
	stderrStr := strings.TrimSpace(stderrBuf.String())
	if stderrStr != "" {
		slog.Debug("tool stderr", "executable", executable, "stderr", stderrStr)
	}

	if ctx.Err() != nil {
		return ipc.Output{}, ctx.Err()
	}

	out, present, readErr := ipc.ReadOutput(ipcDir)
	if present && readErr == nil {
		if waitErr != nil {
			slog.Warn("tool exited nonzero but produced a result", "executable", executable, "err", waitErr)
		}
		slog.Debug("tool process exited", "executable", executable, "type", out.Type)
		return out, nil
	}

	switch {
	case waitErr != nil && stderrStr != "":
		return ipc.Output{}, fmt.Errorf("tool process failed: %w\nstderr: %s", waitErr, stderrStr)
	case waitErr != nil:
		return ipc.Output{}, fmt.Errorf("tool process failed: %w", waitErr)
	case readErr != nil:
		return ipc.Output{}, readErr
	case stderrStr != "":
		return ipc.Output{}, fmt.Errorf("tool produced no result (output.json missing)\nstderr: %s", stderrStr)
	default:
		return ipc.Output{}, fmt.Errorf("tool produced no result (output.json missing)")
	}
}

// parseTaskInput converts the model's tool-call arguments into an ipc.Input.
// The model supplies a JSON object with "prompt" and (optionally) "payload";
// a bare string is accepted as a fallback.
func parseTaskInput(input string) ipc.Input {
	var in ipc.Input
	if err := json.Unmarshal([]byte(input), &in); err == nil && in.Prompt != "" {
		return in
	}
	return ipc.Input{Prompt: input}
}
