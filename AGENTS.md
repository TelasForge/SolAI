# AGENTS.md

Guidance for AI coding agents working on SolAI. Keep this file accurate and
concise. `plan.md` is the design; `README.md` is the user-facing doc.

## Golden rules

1. **The runtime never implements sandboxing.** It only launches commands. Where
   a session or tool actually runs is the operator's infrastructure (e.g. a
   microVM wrapper around the launch command). Do not add sandbox SDKs here.
2. **OpenRouter only.** One credential (`OPENROUTER_API_KEY`); the model is an
   OpenRouter slug (`vendor/model`).
3. **Native tool calling.** Tools are JSON-schema functions; the model returns
   structured `tool_calls`.
4. **Sessions.** A session runs continuously until its goals are resolved or it
   is stopped; one conversation for the life of the session.
5. Keep `README.md`, `plan.md`, `tools/tool-plan.md`, and this file in sync.

## Architecture

Two components (see `plan.md`):

- **Agent runtime** — runs one session; reads a spec from stdin, runs the goal
  loop, emits JSON-line events on stdout. Sandbox-unaware.
- **Control plane (`solaid`)** — HTTP/SSE API; in-memory sessions; launches the
  runtime as a command and relays its events.

Sandboxing is surrounding infrastructure. Do not build it here.

## Layout

```
cmd/solai/               session runtime (binary: `solai`)
cmd/solaid/              control-plane API (binary: `solaid`)
session/                 session spec, event stream, continuous goal loop
runtime/                 wires env config + spec into a session
controlplane/            control-plane API: manager, launcher, HTTP/SSE server
capability/              capabilities + request broker
config/                  environment-based configuration
tool/                    tool manifests, loading, subprocess runner
wallet/                  wallet/keypair
prompts/                 embedded system prompts
registry/                tool installation + curated index (index.json)
llm/                     OpenRouter client + tool-calling loop
ipc/                     file-based tool IPC protocol (atomic writes)
ratelimit/               rate limiting + retry
tools/token-price/       agentic tool
tools/raydium/           agentic tool
tools/jupiter-swap/      agentic tool
```

Everything lives in a single Go module at the repo root
(`github.com/melodyogonna/solai`). Do not add nested `go.mod` files.

`llm/`: `client.go` (OpenRouter, retries), `agent.go` (`Agent`, `TrimHistory`,
`ErrMaxIterations`), `tool.go` (`Tool`, `ParameterizedTool`, schema/arg
helpers), `types.go`.

## Build & test

```bash
make all      # .out/solai, .out/solaid, tools/token-price/bin/token-price
make test     # go test ./... from the repo root
```

Build binaries individually, e.g.
`go build -o /tmp/solai ./cmd/solai` or
`go build -o /tmp/token-price ./tools/token-price`.

Before committing: `gofmt -w .` and `go vet ./...` from the repo root must be
clean.

## Conventions

- **Tool contract:** an `llm.Tool` has `Name`, `Description`, and
  `Call(ctx, argsJSON)`. `argsJSON` is the raw arguments object; parse it with
  `llm.ArgString` / `llm.ArgRaw`. Optionally implement `Parameters()` to
  advertise a schema (`llm.StringSchema`, `llm.OpenObjectSchema`); without it the
  agent sends one optional free-text `input` property.

  ```go
  func (t *myTool) Call(ctx context.Context, argsJSON string) (string, error) {
      input := llm.ArgString(argsJSON, "input")
      // ...
  }
  ```
- **Goal loop:** the runtime injects a `goal` tool (`complete`/`fail`/`block`/
  `note`). If the model stops without resolving a goal, re-prompt it to continue;
  do not end the session. A safety cap pauses and reports — it never silently
  truncates.
- **History invariant:** never split an assistant `tool_calls` message from its
  matching `tool` result.
- **Tools as subprocesses:** the runtime writes `$SOLAI_IPC_DIR/input.json`,
  starts the tool, waits for exit, then reads `output.json`. Completion is
  signalled by a valid `output.json`, not the exit code: a tool may exit nonzero
  and still report a structured `error`. A missing/malformed result is an
  infrastructure error. Both sides use `github.com/melodyogonna/solai/ipc`.
  Full contract in `tools/tool-plan.md`.
- **Capabilities:** `Core` (invisible), `Internal` (coordinator-only),
  `Regular` (coordinator + requestable by tools). Empty `Description()` hides
  one from the LLM. A tool's `request` output is executed by `capability.Broker`
  on the runtime side (only `Regular` capabilities are requestable); the broker
  returns the result to the coordinator, which follows the tool's instruction.
- **PR titles:** `[Package] Title`, both parts start uppercase.
- **Comments:** keep production code sparsely commented; tests may use comments
  freely.
- **Development:** a human may be editing concurrently — treat the codebase, not
  memory, as the source of truth.

## Architecture notes

- `runtime.Run` wires a `config.Config` (from the environment) and a
  `session.Spec` (from stdin) into a `session.Runtime`.
- `cmd/solai` is the runtime binary: it reads a spec from stdin and streams
  events on stdout. It is launched by the control plane, not run as a CLI.
- `controlplane` + `cmd/solaid` are the control-plane API (in-memory sessions,
  SSE relay). It launches each session via a configurable command — the sandbox
  seam.
- A tool is a plain program; tool IPC uses the shared `ipc/` package (typed
  input/output, atomic `output.json`).
- Configuration is read from environment variables only; there is no config
  file. See `README.md` for the variable list.

## Gotchas

- One Go module at the repo root. All tool sources under `tools/` are packages
  of it; building `./tools/<name>` resolves the root `go.mod`.
- Tool env variables are read from the process environment; a missing
  `required` var disables the tool with a warning (not fatal).
- `network-manager` grants nothing at the OS level — it is a declarative marker.
