# SolAI

> **Status:** SolAI is a minimal agent harness. See `plan.md` for the design and
> `AGENTS.md` for contributor guidance.

SolAI runs long-lived Solana agents. A **session** has one or more **goals** and
a selection of **tools**, and the agent works continuously until the goals are
resolved or the session is stopped. LLM access is via
[OpenRouter](https://openrouter.ai).

SolAI is not a CLI. Tools are compiled into the runtime and called in-process.
The runtime receives its configuration from the environment, the way a container
receives its configuration from the platform that schedules it.

---

## Components

| Binary | Role |
|---|---|
| `solaid` | Control-plane HTTP/SSE API and the web UI. Creates, launches, and tracks sessions, and provisions capabilities into their environment. |
| `solai` | Session runtime. Reads a spec from stdin, emits events on stdout. Launched by `solaid`. |

Sandboxing is **not** part of this code. The control plane launches each
session as a command, and the operator points that command at their own
infrastructure (for example a microVM runner). The runtime only launches
commands; where those commands run is the platform's concern.

---

## Concepts

- **Session** — one long-running agent run: goals, model, tools.
- **Goal** — a task the session must accomplish. The agent works through goals
  one at a time and explicitly marks each `complete`, `fail`, or `block`.
- **Tool** — an in-process capability the model can call (for example
  `jupiter-price` or `solana`). Tools are compiled in and selected per session.
- **MCP server** — an external [Model Context Protocol](https://modelcontextprotocol.io)
  server. Its tools are discovered at session start and exposed alongside the
  built-in tools, namespaced as `<server>__<tool>`.
- **Capability** — an infrastructure authority the control plane *provisions*
  into a session's environment (network egress, Solana RPC access, wallet
  signing). The runtime never sees the capability; it only sees the environment
  it was given.

---

## Running

Configuration comes from the environment (see
[Configuration](#configuration)); the session itself is a JSON spec.

### Runtime

The runtime reads the spec from stdin and streams events as JSON lines on
stdout:

```bash
solai < session.json
```

```json
{
  "goals": ["Monitor SOL price and report significant moves"],
  "model": "anthropic/claude-sonnet-4",
  "tools": ["jupiter-price"],
  "mcp_servers": [{ "name": "jupiter", "url": "http://jupiter-mcp:8080" }]
}
```

`model` overrides `SOLAI_MODEL` for this session. An empty `tools` list exposes
every registered tool. Each `mcp_servers` entry is connected at session start
and its tools are exposed as `<server>__<tool>` (for example
`jupiter__quote`).

### Control plane

```bash
solaid --addr 127.0.0.1:8787 --launch "solai"

# Start a session
curl -sX POST localhost:8787/api/sessions -d '{
  "goals": ["Monitor SOL and report big moves"],
  "tools": ["jupiter-price"],
  "capabilities": ["network", "rpc"]
}'

# Watch it live
curl -N localhost:8787/api/sessions/<id>/events
```

`solaid` inherits its environment, and for each session it adds the environment
granted by the requested capabilities before launching the runtime. Point
`--launch` at a wrapper (e.g. a microVM runner) to isolate sessions.

### API

```
POST   /api/sessions                 create + start (session spec)
GET    /api/sessions                 list
GET    /api/sessions/{id}            status + goals
POST   /api/sessions/{id}/stop
GET    /api/sessions/{id}/events     SSE live event stream
GET    /api/tools                    registered tools
GET    /api/capabilities             available capabilities
```

### Web UI

`solaid` also serves a minimal server-rendered UI at `/` (start a session, list
sessions, view a session). The session page shows a live event feed, populated
by `EventSource` from the `/api/sessions/{id}/events` stream. It is templ + htmx,
composed with chi, and shares the same in-memory manager as the API.

---

## Configuration

Everything is read from the process environment. There is no config file.

| Variable | Required | Description |
|---|---|---|
| `SOLAI_MODEL` | yes | OpenRouter model slug (e.g. `anthropic/claude-sonnet-4`) |
| `OPENROUTER_API_KEY` | yes | OpenRouter API key |
| `SOLAI_OPENROUTER_BASE_URL` | no | Override the OpenRouter API root |
| `SOLAI_WALLET_SEED` | no | BIP39 mnemonic for the agent wallet |
| `SOLAI_RPC_URL` | no | Solana RPC endpoint (default `https://api.mainnet-beta.solana.com`) |
| `SOLAI_COMMITMENT` | no | `finalized`, `confirmed`, or `processed` (default `confirmed`) |
| `SOLAI_LOG_LEVEL` | no | `debug`, `info`, `warn`, or `error` |
| `SOLAI_LAUNCH_CMD` | no | Runtime launch command used by `solaid` (default `solai`) |

Individual tools may read their own environment variables (for example
`JUPITER_API_KEY`); the platform injects them the same way.

---

## Capabilities

A capability is a grant the control plane provisions into a session's
environment. The runtime does not enforce it; the platform does. In a real
deployment each capability maps to sandbox policy (network egress, mounts,
secrets) in addition to environment variables.

| Capability | Grants |
|---|---|
| `network` | Outbound network access (platform-enforced) |
| `rpc` | `SOLAI_RPC_URL`, `SOLAI_COMMITMENT` |
| `wallet` | `SOLAI_WALLET_SEED` |

---

## Tools

Tools are Go values implementing `llm.Tool` (`Name`, `Description`,
`Parameters`, `Call`), registered at compile time and called directly. There are
no manifests, no subprocesses, and no IPC.

Adding a tool:

```go
package mytool

func init() {
    tool.Register("my-tool", "What it does, for the model.", func() llm.Tool {
        return &myTool{}
    })
}

type myTool struct{}

func (t *myTool) Name() string        { return "my-tool" }
func (t *myTool) Description() string { return "..." }
func (t *myTool) Parameters() map[string]any { /* JSON Schema */ }
func (t *myTool) Call(ctx context.Context, argsJSON string) (string, error) {
    // parse argsJSON, return a JSON result
}
```

Then add the package to the blank imports in `tools/tools.go`.

### MCP servers

External integrations can be provided by MCP servers instead of compiled-in
tools. Add them to the session spec:

```json
{
  "goals": ["Quote a swap"],
  "mcp_servers": [
    { "name": "jupiter", "url": "http://jupiter-mcp:8080" }
  ]
}
```

The runtime connects over the MCP **Streamable HTTP** transport, discovers each
server's tools, and adds them to the coordinator's toolset. Names are namespaced
`<server>__<tool>` to avoid collisions, and a server's `inputSchema` is used
verbatim. The runtime is a plain MCP client — no SDK is required.

---

## Build

```bash
make all        # .out/solai and .out/solaid
make generate   # regenerate web/*_templ.go from web/*.templ
make test       # go test ./...
```

## Local development

Copy `.env.example` to `.env` and fill it in, then:

```bash
make run        # builds, then runs solaidd (UI + API) at http://127.0.0.1:8787
```

`make run` loads `.env` via `go tool godotenv -o -f .env`, so the file wins over
variables already exported in your shell. The binaries themselves stay
environment-only; `.env` is gitignored.

All Go code is a single module at the repo root
(`github.com/melodyogonna/solai`).

---

## Architecture

```
client (curl) ─▶ solaid ──launch(cmd + capability env)──▶ solai runtime
                   │                                          │
                   └── in-memory sessions                     └── in-process tools
```

| Component | Responsibility |
|---|---|
| `cmd/solaid` | Control-plane binary |
| `cmd/solai` | Session runtime binary |
| `controlplane/` | Session manager, capability provisioning, launcher, HTTP/SSE server |
| `session/` | Session spec, event stream, continuous goal loop |
| `runtime/` | Wires environment configuration and a spec into a session |
| `llm/` | Dependency-free OpenRouter client + native tool-calling loop |
| `tool/` | In-process tool registry |
| `web/` | Minimal server-rendered UI (templ + htmx) |
| `mcp/` | Minimal MCP client: JSON-RPC over Streamable HTTP, tool discovery, adapters |
| `tools/` | Built-in tools (solana, wallet, jupiter, raydium, tokenprice) |
| `wallet/` | BIP39 seed derivation and ed25519 keypair |
| `config/` | Environment-based configuration |
| `prompts/` | Embedded system prompt |
| `ratelimit/` | Retry and rate-limit strategies |

See `plan.md` for the full design.
