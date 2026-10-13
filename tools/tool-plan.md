# Agentic Tool Development Guide

Agentic tools are standalone executables that use an internal LLM agent with
native tool calling to handle a subtask, then return a structured result to the
SolAI coordinator. This document captures the current conventions and lessons
learned.

## Structure

Each tool is a directory under `tools/` containing:

- `manifest.json` — tool metadata and capability requirements
- A compiled binary or script (referenced by `executable` in the manifest)
- Source files (Go recommended): `main.go`, `llm.go`, `models.go`

### manifest.json

```json
{
  "name": "tool-name",
  "description": "What this tool does (shown to the coordinator LLM)",
  "version": "1.0.0",
  "executable": "./bin/tool-name",
  "required_capabilities": ["network-manager"],
  "llm_options": {
    "primary": "anthropic/claude-sonnet-4",
    "supported": [
      { "model": "anthropic/claude-sonnet-4" },
      { "model": "openai/gpt-4o" }
    ]
  }
}
```

`llm_options` entries are OpenRouter model slugs (`vendor/model`). Omit it for
purely deterministic tools that do not use an LLM.

## File-based IPC

The coordinator communicates with tools via JSON files in a per-invocation temp
directory. Its path is passed via the `SOLAI_IPC_DIR` environment variable.

**`$SOLAI_IPC_DIR/input.json`** (coordinator → tool):

```json
{
  "prompt": "User's high-level goal",
  "payload": { "wallet_address": "ABC...XYZ" }
}
```

`prompt` is always present. `payload` is a flat string map carrying named values
the tool may need. Entries declared in the manifest with a `source` field are
auto-injected by the coordinator; others arrive on re-invocation when fulfilling
a capability request.

**`$SOLAI_IPC_DIR/output.json`** (tool → coordinator):

```json
{"type": "success", "payload": <json-value>}
{"type": "error",   "payload": "<error string>"}
{"type": "request", "payload": {"capability":"<name>","action":"<action>","input":"<data>","instruction":"<what to do next>"}}
```

Write exactly one JSON object to `output.json` and exit. Never write anything to
stdout (use stderr for logs).

Go tools should use the shared `github.com/melodyogonna/solai/ipc` package,
which centralizes the protocol and writes `output.json` atomically (temp file +
rename):

```go
in, err := ipc.ReadInput()                 // reads $SOLAI_IPC_DIR/input.json
// ... do work ...
ipc.WriteSuccess(result)                   // or WriteError(msg) / WriteRequest(req)
```

The runtime treats a valid `output.json` as the result regardless of the exit
code, so a tool may `exit 1` after writing a structured `error` and the
coordinator will surface that error. A missing or malformed `output.json` is an
infrastructure error.

## The Shared `llm` and `ipc` Modules

Tools import `github.com/melodyogonna/solai/llm` for the OpenRouter client and
the agent loop, and `github.com/melodyogonna/solai/ipc` for the file protocol.
There are no third-party LLM dependencies.

```go
import "github.com/melodyogonna/solai/llm"

client, err := llm.FromEnv() // reads SOLAI_LLM_MODEL / SOLAI_LLM_API_KEY / SOLAI_LLM_BASE_URL
if err != nil { ... }

tools := []llm.Tool{
    &myTool{},
}

agent := llm.NewAgent(client, tools, mySystemPrompt,
    llm.WithMaxIterations(6),
)

result, _, err := agent.Run(ctx, prompt, nil)
if err != nil { ... }
```

The agent advertises each tool through native function calling; the model
returns structured `tool_calls` and the loop executes them.

### Implementing a Tool

```go
type myTool struct{}

func (t *myTool) Name() string        { return "my-tool" }
func (t *myTool) Description() string { return "One-line description for the model." }

// Call receives the raw JSON arguments object produced by the model.
func (t *myTool) Call(ctx context.Context, argsJSON string) (string, error) {
    input := llm.ArgString(argsJSON, "input") // extracts {"input":"..."}
    ...
    return string(data), nil
}

// Parameters is optional. Without it, the agent advertises a single optional
// free-text "input" property. Provide one for a structured schema:
func (t *myTool) Parameters() map[string]any {
    return map[string]any{
        "type": "object",
        "properties": map[string]any{
            "input": map[string]any{"type": "string", "description": "Comma-separated token symbols, e.g. \"SOL,USDC\"."},
        },
    }
}
```

Helpers:

- `llm.StringSchema(description)` — one optional free-text `input` string.
- `llm.OpenObjectSchema()` — arbitrary named properties (used by capabilities).
- `llm.ArgString(argsJSON, key)` — extract a string field; returns the raw
  string unchanged when the input is not a JSON object.
- `llm.ArgRaw(argsJSON, key)` — extract a raw JSON value.

### Tool Descriptions

Descriptions are the primary way the internal LLM decides which tool to call and
how to format arguments. State the exact input format, give a concrete example,
and state what is returned. Because arguments are JSON, prefer an explicit
`Parameters()` schema over hoping the model guesses the right key names.

## LLM Initialisation

The coordinator injects:

- `SOLAI_LLM_MODEL` — OpenRouter model slug
- `SOLAI_LLM_API_KEY` — OpenRouter API key
- `SOLAI_LLM_BASE_URL` — optional API root override

`llm.FromEnv()` reads all three. The model is chosen by the coordinator from the
tool's manifest `llm_options`.

## Capability Requests

If a tool needs the coordinator to perform a privileged action (e.g. signing a
transaction), it writes a `"request"` output to `output.json` and exits
immediately — no blocking.

```go
ipc.WriteRequest(ipc.Request{
    Capability:  "solana",
    Action:      "send_transaction",
    Input:       unsignedTxBase64,
    Instruction: "Re-invoke this tool with the signed transaction in payloads.transaction.",
})
```

The coordinator reads the request, calls the named capability, then follows the
`instruction` — typically re-invoking the tool with a new `input.json` whose
`payload` holds the capability result.

Only `Regular` capabilities are requestable; `Core` and `Internal` capabilities
are not exposed to tools. The coordinator appends a `## Requestable
Capabilities` section to every tool prompt so the inner LLM knows what it can
request.

## Checklist for a New Tool

- [ ] `manifest.json` with correct `required_capabilities` and (if it uses an LLM) `llm_options`
- [ ] Read input with `ipc.ReadInput()` (or `$SOLAI_IPC_DIR/input.json`)
- [ ] Write the result with `ipc.WriteSuccess` / `ipc.WriteError` / `ipc.WriteRequest` (atomic)
- [ ] Implement `llm.Tool` (`Name`, `Description`, `Call`) and, when useful, `Parameters`
- [ ] Construct the client with `llm.FromEnv()` and the agent with `llm.NewAgent`
- [ ] If using capability requests: emit a `"request"` output with a clear `instruction`
- [ ] Never write to stdout; log to stderr
