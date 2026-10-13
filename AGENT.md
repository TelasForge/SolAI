# AGENT.md

Guidance for AI coding agents working on SolAI. Keep this file accurate and
concise. `plan.md` is the design; `README.md` is the user-facing doc.

## Golden rules

1. **The runtime never implements sandboxing.** It only launches commands. Where
   a session or tool actually runs is the operator's infrastructure (e.g. a
   microVM wrapper around the launch command). Do not add sandbox SDKs here.
2. **OpenRouter only.** One credential (`provider.openrouter`); the model is an
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
ratelimit/               shared: rate limiting + retry
llm/                     shared: OpenRouter client + tool-calling loop
ipc/                     shared: file-based tool IPC protocol (atomic writes)
solai-agent/             runtime + CLI
  session/               session spec, event stream, continuous goal loop
  controlplane/          control-plane API: manager, launcher, HTTP/SSE server
  cmd/solaid/            control-plane binary (`solaid`)
tools/token-price/       agentic tool
tools/raydium/           agentic tool
tools/jupiter-swap/      agentic tool
registry/index.json      curated tool index
```

`solai-agent/` packages today: `cli`, `config`, `capability`, `tool`,
`prompts`, `wallet`, `registry`, plus `session` and `controlplane`.

`llm/`: `client.go` (OpenRouter, retries), `agent.go` (`Agent`, `TrimHistory`,
`ErrMaxIterations`), `tool.go` (`Tool`, `ParameterizedTool`, schema/arg
helpers), `types.go`.

## Build & test

```bash
make all      # .out/solai + tools/token-price/bin/token-price
make test     # solai-agent tests + every tools/* module
```

Build tools individually with `go build -o /tmp/<name> ./tools/<name>`.
`go test ./...` from the repo root does NOT work (root is not a module) — run
from a module dir or use `make test`. Also test the shared modules directly:
`(cd llm && go test ./...)`, `(cd ratelimit && go test ./...)`.

Before committing: `gofmt -w llm solai-agent tools` and `go vet ./...` in each
module must be clean.

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

- `solai-agent/session` is the session runtime. `solai __runtime` runs one
  session from a spec on stdin (used by the control plane); `solai start` builds
  a spec from config and runs it locally.
- `solai-agent/controlplane` + `cmd/solaid` are the control-plane API
  (in-memory sessions, SSE relay). It launches each session via a configurable
  command — the sandbox seam.
- A tool is a plain program; tool IPC uses the shared `ipc/` module (typed
  input/output, atomic `output.json`).
- Config lives in `~/.solai/config.json`; see `README.md` for the keys.

## Gotchas

- Modules importing `llm` carry a local `replace ... => ../llm` / `../../llm`
  because `llm` is unpublished; `go mod tidy` fails without it.
- Module paths are inconsistent by history (`.../raydium` vs
  `.../tools/token-price`). Check `go.mod` before assuming.
- Tool env comes only from `tool-env.<tool>.<VAR>`; a missing `required` var
  disables the tool with a warning (not fatal).
- `network-manager` grants nothing at the OS level — it is a declarative marker.
