// Package ipc defines the file-based protocol between the SolAI runtime and
// one-shot agentic tools.
//
// The runtime creates a fresh directory per invocation and points the tool at
// it via SOLAI_IPC_DIR. The tool reads input.json and writes output.json
// atomically (temp file + rename). Completion is signalled by process exit plus
// a valid output.json — never by stdout, which is reserved for logging.
package ipc

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// EnvDir is the environment variable naming the per-invocation directory.
const EnvDir = "SOLAI_IPC_DIR"

const (
	inputFile  = "input.json"
	outputFile = "output.json"
	tmpSuffix  = ".tmp"
)

// Type discriminates the three kinds of tool output.
type Type string

const (
	TypeSuccess Type = "success"
	TypeError   Type = "error"
	TypeRequest Type = "request"
)

// Input is what the runtime writes for a tool to read.
type Input struct {
	// Prompt describes the objective for this invocation. Always present.
	Prompt string `json:"prompt"`

	// Payload carries named values the tool may need.
	Payload map[string]string `json:"payload,omitempty"`
}

// Output is what a tool writes for the runtime to read.
type Output struct {
	Type    Type            `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// Request is the payload carried by a TypeRequest output. It asks the runtime
// to invoke a capability and then follow Instruction.
type Request struct {
	Capability  string `json:"capability"`
	Action      string `json:"action"`
	Input       string `json:"input"`
	Instruction string `json:"instruction"`
}

// Dir returns the directory named by SOLAI_IPC_DIR, or "" when unset.
func Dir() string { return os.Getenv(EnvDir) }

// ReadInput reads input.json from the directory named by SOLAI_IPC_DIR.
func ReadInput() (Input, error) { return ReadInputFrom(Dir()) }

// ReadInputFrom reads input.json from dir.
func ReadInputFrom(dir string) (Input, error) {
	if dir == "" {
		return Input{}, fmt.Errorf("ipc: %s is not set", EnvDir)
	}
	data, err := os.ReadFile(filepath.Join(dir, inputFile))
	if err != nil {
		return Input{}, fmt.Errorf("ipc: reading %s: %w", inputFile, err)
	}
	var in Input
	if err := json.Unmarshal(data, &in); err != nil {
		return Input{}, fmt.Errorf("ipc: parsing %s: %w", inputFile, err)
	}
	return in, nil
}

// WriteInput writes input.json to the directory named by SOLAI_IPC_DIR.
func WriteInput(in Input) error { return WriteInputTo(Dir(), in) }

// WriteInputTo writes input.json to dir.
func WriteInputTo(dir string, in Input) error {
	if dir == "" {
		return fmt.Errorf("ipc: %s is not set", EnvDir)
	}
	data, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("ipc: marshalling input: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, inputFile), data, 0o600); err != nil {
		return fmt.Errorf("ipc: writing %s: %w", inputFile, err)
	}
	return nil
}

// WriteOutput atomically writes output.json to the directory named by
// SOLAI_IPC_DIR.
func WriteOutput(o Output) error { return WriteOutputTo(Dir(), o) }

// WriteOutputTo atomically writes output.json to dir: it writes a temp file in
// the same directory and renames it into place, so a reader never observes a
// partial file.
func WriteOutputTo(dir string, o Output) error {
	if dir == "" {
		return fmt.Errorf("ipc: %s is not set", EnvDir)
	}
	data, err := json.Marshal(o)
	if err != nil {
		return fmt.Errorf("ipc: marshalling output: %w", err)
	}
	tmp := filepath.Join(dir, outputFile+tmpSuffix)
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("ipc: writing temp output: %w", err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, outputFile)); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("ipc: committing output: %w", err)
	}
	return nil
}

// WriteSuccess writes a success output with an arbitrary JSON payload.
func WriteSuccess(payload any) error { return writePayload(TypeSuccess, payload) }

// WriteError writes an error output with a human-readable message.
func WriteError(msg string) error { return writePayload(TypeError, msg) }

// WriteRequest writes a capability request output.
func WriteRequest(req Request) error { return writePayload(TypeRequest, req) }

func writePayload(t Type, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("ipc: marshalling %s payload: %w", t, err)
	}
	return WriteOutput(Output{Type: t, Payload: raw})
}

// ReadOutput reads output.json from dir.
//
// present is false when the file does not exist. A present-but-malformed file
// (invalid JSON or an empty type) returns present=true with an error, so the
// caller can distinguish "no result" from "bad result".
func ReadOutput(dir string) (out Output, present bool, err error) {
	data, err := os.ReadFile(filepath.Join(dir, outputFile))
	if errors.Is(err, os.ErrNotExist) {
		return Output{}, false, nil
	}
	if err != nil {
		return Output{}, false, fmt.Errorf("ipc: reading %s: %w", outputFile, err)
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return Output{}, true, fmt.Errorf("ipc: parsing %s: %w", outputFile, err)
	}
	if out.Type == "" {
		return Output{}, true, fmt.Errorf("ipc: %s has an empty type", outputFile)
	}
	return out, true, nil
}
