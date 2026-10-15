# AGENTS.md

Guidance for AI coding agents working on SolAI. Keep this file accurate and
concise. `plan.md` is the design; `README.md` is the user-facing doc; `TODO.md` is the
open-work list.

## Golden rules

1. **The runtime never implements sandboxing.** It only launches commands. Where
   a session actually runs is the operator's infrastructure (e.g. a microVM
   wrapper around the launch command). Do not add sandbox SDKs here.
2. **OpenRouter only.** One credential (`OPENROUTER_API_KEY`); the model is an
   OpenRouter slug (`vendor/model`).
3. **Native tool calling.** Tools are JSON-schema functions; the model returns
   structured `tool_calls`.
4. **Sessions.** A session runs continuously until its goals are resolved or it
   is stopped; one conversation for the life of the session.
5. Keep `README.md`, `plan.md`, `TODO.md`, and this file in sync. Open work is
tracked in `TODO.md`.

## Architecture

Two components (see `plan.md`):

- **Agent runtime** (`cmd/solai`, `runtime/`) — runs one session; reads a spec
  from stdin, runs the goal loop, emits JSON-line events on stdout.
  Sandbox-unaware.
- **Control plane** (`cmd/solaid`, `controlplane/`) — HTTP/SSE API; in-memory
  sessions; provisions capabilities into the runtime's environment and launches
  it as a command.

Tools are **in-process**: Go values implementing `llm.Tool`, registered at
compile time and called directly. There are no manifests, binaries, or IPC.

External integrations can instead be provided by **MCP servers** (`mcp/`): the
runtime is a minimal MCP client that discovers their tools and adapts them to
`llm.Tool`.

Capabilities are **not** in the runtime. They are infrastructure grants the
control plane provisions into a session's environment (network, RPC, wallet).

## Layout

```
cmd/solai/               session runtime (binary: `solai`)
cmd/solaid/              control-plane API (binary: `solaid`)
runtime/                 wires env config + spec into a session
controlplane/            session manager, capability provisioning, launcher, HTTP/SSE
session/                 session spec, event stream, continuous goal loop
tool/                    in-process tool registry
mcp/                     minimal MCP client (Streamable HTTP)
web/                     minimal server-rendered UI (templ + htmx)
tools/                   built-in tools (solana, wallet, jupiter, raydium, tokenprice)
llm/                     OpenRouter client + tool-calling loop
wallet/                  BIP39 seed derivation and ed25519 keypair
config/                  environment-based configuration
prompts/                 embedded system prompt
ratelimit/               rate limiting + retry
```

Everything lives in a single Go module at the repo root
(`github.com/melodyogonna/solai`). Do not add nested `go.mod` files.

`llm/`: `client.go` (OpenRouter, retries), `agent.go` (`Agent`, `TrimHistory`,
`ErrMaxIterations`), `tool.go` (`Tool`, `ParameterizedTool`, schema/arg
helpers), `types.go`.

## Build & test

```bash
make all      # .out/solai, .out/solaid
make test     # go test ./... from the repo root
```

`make run` builds and runs `solaid` with `.env` loaded, and `make generate`
regenerates templ components. Both `templ` and `godotenv` are pinned as `go tool`
dependencies in `go.mod` (run them with `go tool templ` / `go tool godotenv`).
The binaries remain environment-only; `.env` is loaded by the tooling, not the
code.

Before committing: `gofmt -w .` and `go vet ./...` from the repo root must be
clean.

## Conventions

- **Tool contract:** an `llm.Tool` has `Name`, `Description`, and
  `Call(ctx, argsJSON)`. `argsJSON` is the raw arguments object; parse it with
  `json.Unmarshal` (or `llm.ArgString` / `llm.ArgRaw` for loose input). Implement
  `Parameters()` to advertise a JSON Schema; without it the agent sends one
  optional free-text `input` property.

  ```go
  func (t *myTool) Call(ctx context.Context, argsJSON string) (string, error) {
      var args struct{ Tokens []string `json:"tokens"` }
      if err := json.Unmarshal([]byte(argsJSON), &args); err != nil { ... }
      // ...
  }
  ```
- **Registering a tool:** call `tool.Register(name, description, factory)` from
  an `init()`, and add the package to the blank imports in `tools/tools.go`.
  The factory is called once per session, so a tool may hold session state.
- **Goal loop:** the runtime injects a `goal` tool (`complete`/`fail`/`block`/
  `note`). If the model stops without resolving a goal, re-prompt it to continue;
  do not end the session. A safety cap pauses and reports — it never silently
  truncates.
- **History invariant:** never split an assistant `tool_calls` message from its
  matching `tool` result.
- **Capabilities live in the control plane.** A capability is a grant the
  control plane provisions into the runtime environment; the runtime only reads
  environment variables. Do not add capability enforcement to the runtime.
- **PR titles:** `[Package] Title`, both parts start uppercase.
- **Comments:** keep production code sparsely commented; tests may use comments
  freely.
- **Development:** a human may be editing concurrently — treat the codebase, not
  memory as the source of truth.

## Gotchas

- **templ codegen:** after editing `web/*.templ`, run `go tool templ generate
  ./web` (or `make generate`) and commit the generated `web/*_templ.go`.
- **Routing:** `cmd/solaid` composes a chi router — the stdlib API is mounted at
  `/api/*` via `http.StripPrefix`, and the web UI is mounted at `/`.
- One Go module at the repo root. Do not add nested `go.mod` files.
- Tool environment variables are read from the process environment; the platform
  injects them.
- `tools/<name>` packages register themselves on import; a binary that needs the
  registry populated must import `tools` (the runtime does; `cmd/solaid` does).
- `network` grants nothing at the runtime level — it is provisioned by the
  platform.
