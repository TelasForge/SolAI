package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ToolInput is the JSON structure written to input.json in the IPC directory
// before starting a tool.
type ToolInput struct {
	// Prompt describes what to do. Always present.
	Prompt string `json:"prompt"`

	// Payloads holds named data values available to the tool. Auto-injected
	// values (e.g. "wallet_address") are merged in by the coordinator at
	// construction time; additional values may arrive on re-invocation.
	Payload map[string]string `json:"payload,omitempty"`
}

// ToolOutput is the JSON structure read from output.json after the tool exits.
type ToolOutput struct {
	// Type is one of: "success", "error", "request".
	Type string `json:"type"`

	// Payload holds the tool's result. For "success" it may be any JSON value.
	// For "error" it is a human-readable string.
	// For "request" it is {"capability":"...","action":"...","input":"..."}.
	Payload json.RawMessage `json:"payload"`
}

// RunTool writes input.json to the IPC directory, spawns the tool subprocess,
// waits for it to exit, then reads and returns output.json.
//
// The tool reads $SOLAI_IPC_DIR/input.json on startup and writes
// $SOLAI_IPC_DIR/output.json before exiting. solaiIPCEnv must point at the
// directory the tool should use for IPC.
//
// The process is killed if ctx is cancelled or the per-tool timeout elapses.
func RunTool(ctx context.Context, dir, executable string, input ToolInput, timeout time.Duration, extraEnv []string, ipcDir string) (ToolOutput, error) {
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return ToolOutput{}, fmt.Errorf("marshalling tool input: %w", err)
	}
	if err := os.WriteFile(filepath.Join(ipcDir, "input.json"), inputJSON, 0o600); err != nil {
		return ToolOutput{}, fmt.Errorf("writing input.json: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Resolve to an absolute path so exec finds the binary regardless of the
	// agent's current working directory (cmd.Dir only affects the child
	// process's working directory, not the path lookup for the executable).
	absExec := filepath.Join(dir, executable)
	cmd := exec.CommandContext(ctx, absExec)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extraEnv...)

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return ToolOutput{}, fmt.Errorf("starting tool: %w", err)
	}
	slog.Debug("tool process started", "executable", executable, "pid", cmd.Process.Pid)

	waitErr := cmd.Wait()
	if stderrStr := strings.TrimSpace(stderrBuf.String()); stderrStr != "" {
		slog.Debug("tool stderr", "executable", executable, "stderr", stderrStr)
	}
	if waitErr != nil {
		if ctx.Err() != nil {
			return ToolOutput{}, ctx.Err()
		}
		stderrStr := strings.TrimSpace(stderrBuf.String())
		if stderrStr != "" {
			return ToolOutput{}, fmt.Errorf("tool process failed: %w\nstderr: %s", waitErr, stderrStr)
		}
		return ToolOutput{}, fmt.Errorf("tool process failed: %w", waitErr)
	}
	slog.Debug("tool process exited", "executable", executable)

	data, err := os.ReadFile(filepath.Join(ipcDir, "output.json"))
	if err != nil {
		return ToolOutput{}, fmt.Errorf("reading output.json: %w", err)
	}
	var output ToolOutput
	if err := json.Unmarshal(data, &output); err != nil {
		return ToolOutput{}, fmt.Errorf("parsing output.json: %w", err)
	}
	if output.Type == "" {
		return ToolOutput{}, fmt.Errorf("tool produced no output (output.json has empty type)")
	}
	return output, nil
}

// parseTaskInput converts the model's tool-call arguments into a ToolInput.
// The model supplies a JSON object with "prompt" and (optionally) "payload";
// a bare string is accepted as a fallback.
func parseTaskInput(input string) ToolInput {
	var ti ToolInput
	if err := json.Unmarshal([]byte(input), &ti); err == nil && ti.Prompt != "" {
		return ti
	}
	return ToolInput{Prompt: input}
}
