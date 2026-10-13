# AGENT.md

Guidance for AI coding agents working on SolAI. Keep this file accurate and
concise; put user-facing docs in `README.md` and architecture detail in `plan.md`.

## Golden rules

1. **No OS-level sandboxing.** Tools run as ordinary child processes. Do not add
   bubblewrap/bwrap, namespaces, or seccomp to `solai-agent/tool`.
2. **OpenRouter only.** One credential (`provider.openrouter`); the model is an
   OpenRouter slug (`vendor/model`). No `model.provider`, no per-vendor keys, no
   `SOLAI_LLM_PROVIDER`.
3. **Native tool calling only.** Tools are JSON-schema functions; the model
   returns structured `tool_calls`. No ReAct text format, no markdown-fence
   stripping.
4. Keep `README.md`, `plan.md`, `tools/tool-plan.md`, and this file in sync with
   code changes.

## Layout

```
ratelimit/               shared: rate limiting + retry
llm/                     shared: OpenRouter client + tool-calling agent loop
solai-agent/             coordinator binary (`solai`)
tools/token-price/       agentic tool
tools/raydium/           agentic tool
tools/jupiter-swap/      agentic tool
registry/index.json      curated tool index
```

`solai-agent/` packages: `cli`, `config`, `agent`, `capability`, `tool`,
`prompts`, `wallet`, `registry`. See `README.md` for what each does.

`llm/`: `client.go` (OpenRouter, retries), `agent.go` (`Agent.Run`,
`TrimHistory`, `ErrMaxIterations`), `tool.go` (`Tool`, `ParameterizedTool`,
schema/arg helpers), `types.go`.

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
- **History invariant:** never split an assistant `tool_calls` message from its
  matching `tool` result. `llm.TrimHistory` cuts on user-message boundaries;
  preserve that.
- **Tools as subprocesses:** read `$SOLAI_IPC_DIR/input.json`, write exactly one
  `$SOLAI_IPC_DIR/output.json` (`success`/`error`/`request`), log to stderr only.
  Full contract in `tools/tool-plan.md`.
- **Capabilities:** `Core` (invisible), `Internal` (coordinator-only),
  `Regular` (coordinator + requestable by tools). Empty `Description()` hides
  one from the LLM. Register in `cli/agent_run.go`.
- **PR titles:** `[Package] Title`, both parts start uppercase.
- **Comments:** keep production code sparsely commented; tests may use comments
  freely.
- **Development:** a human may be editing concurrently — treat the codebase, not
  memory, as the source of truth.

## Gotchas

- Modules importing `llm` carry a local `replace ... => ../llm` / `../../llm`
  because `llm` is unpublished; `go mod tidy` fails without it.
- Module paths are inconsistent by history (`.../raydium` vs
  `.../tools/token-price`). Check `go.mod` before assuming.
- Tool env comes only from `tool-env.<tool>.<VAR>`; a missing `required` var
  disables the tool with a warning (not fatal).
- `network-manager` grants nothing at the OS level — it is a declarative marker.
- `system-manager` is a `Core` capability and is therefore hidden from the LLM.
