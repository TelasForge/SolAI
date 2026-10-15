# SolAI

> **Status:** SolAI is implemented as a minimal agent harness. See `plan.md` for
> the design and `AGENTS.md` for contributor guidance.

SolAI runs long-lived Solana agents. A **session** has one or more **goals** and
a selection of **tools** and **capabilities**, and the agent works continuously
until the goals are resolved or the session is stopped. LLM access is via
[OpenRouter](https://openrouter.ai).

SolAI is not a CLI. The runtime is launched by a control plane and receives its
configuration from the environment, the way a container receives its
configuration from the platform that schedules it.

---

## Components

| Binary | Role |
|---|---|
| `solaid` | Control-plane HTTP/SSE API. Creates, launches, and tracks sessions. |
| `solai` | Session runtime. Reads a spec from stdin, emits events on stdout. Launched by `solaid`. |

Sandboxing is **not** part of this code. The control plane launches each
session as a command, and the operator points that command at their own
infrastructure (for example a microVM runner). The runtime and tools only ever
launch commands; where those commands run is the platform's concern.

---

## Concepts

- **Session** — one long-running agent run: goals, model, tools, capabilities.
  It runs until the goals are done or it is stopped.
- **Goal** — a task the session must accomplish. The agent works through goals
  one at a time and explicitly marks each `complete`, `fail`, or `block`.
- **Tool** — a standalone executable with a `manifest.json`. Tools are selected
  per session.
- **Capability** — a system feature (wallet, Solana RPC, memory) selected per
  session. Capabilities are exposed to the coordinator as tools and are
  reachable by agentic tools through the `request` protocol: the tool emits a
  request and the runtime's broker performs it.

---

## Running

Configuration is injected through environment variables (see
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
  "tools": ["token-price"],
  "capabilities": ["wallet", "solana", "memory"]
}
```

`model` overrides `SOLAI_MODEL` for this session.

### Control plane

```bash
solaid --addr 127.0.0.1:8787 --launch "solai"

# Start a session
curl -sX POST localhost:8787/sessions -d '{
  "goals": ["Monitor SOL and report big moves"],
  "tools": ["token-price"],
  "capabilities": ["wallet", "solana", "memory", "network-manager"]
}'

# Watch it live
curl -N localhost:8787/sessions/<id>/events
```

`solaid` inherits its environment, so the runtime it launches gets the same
injected configuration. Point `--launch` at a wrapper (e.g. a microVM runner)
to isolate sessions.

### API

```
POST   /sessions                 create + start (session spec)
GET    /sessions                 list
GET    /sessions/{id}            status + goals
POST   /sessions/{id}/stop
GET    /sessions/{id}/events     SSE live event stream
GET    /tools                    available tools
GET    /capabilities             available capabilities
```

---

## Configuration

Everything is read from the process environment. There is no config file.

| Variable | Required | Description |
|---|---|---|
| `SOLAI_MODEL` | yes | OpenRouter model slug (e.g. `anthropic/claude-sonnet-4`) |
| `OPENROUTER_API_KEY` | yes | OpenRouter API key (shared by the agent and tools) |
| `SOLAI_OPENROUTER_BASE_URL` | no | Override the OpenRouter API root |
| `SOLAI_WALLET_SEED` | no | BIP39 mnemonic; a new wallet is generated if unset |
| `SOLAI_RPC_URL` | no | Solana RPC endpoint (default `https://api.mainnet-beta.solana.com`) |
| `SOLAI_COMMITMENT` | no | `finalized`, `confirmed`, or `processed` (default `confirmed`) |
| `SOLAI_TOOLS_DIR` | no | Directory containing installed tools (default `/tools`, else `~/.solai/tools`) |
| `SOLAI_LOG_LEVEL` | no | `debug`, `info`, `warn`, or `error` (default `info`) |
| `SOLAI_LAUNCH_CMD` | no | Runtime launch command used by `solaid` (default `solai`) |

A platform injects these the same way it injects any other container
configuration (Kubernetes `env`, a secret, a mounted file rendered into a
variable, and so on).

---

## Tools

### Layout

Tools live in `SOLAI_TOOLS_DIR`, populated by the environment (a mounted
volume, the image, or an init container):

```
$SOLAI_TOOLS_DIR/<name>/
  manifest.json
  bin/<name>
```

### Tool environment variables

A tool declares the variables it needs in its manifest. The runtime resolves
them from its own environment and injects them into the tool subprocess:

```json
{
  "env": [
    { "name": "JUPITER_API_KEY", "sensitive": true, "required": false }
  ]
}
```

A missing variable marked `required` disables the tool with a warning.

### Writing a tool

Tools are standalone executables (any language) that communicate with the
runtime over file-based JSON IPC. See `tools/tool-plan.md` for the full
contract.

```json
// $SOLAI_IPC_DIR/input.json  (runtime → tool)
{ "prompt": "Objective for this tool call.", "payload": { "k": "v" } }
```

```json
// $SOLAI_IPC_DIR/output.json  (tool → runtime)
{ "type": "success", "payload": <any JSON> }
{ "type": "error",   "payload": "human-readable error" }
{ "type": "request", "payload": { "capability": "<name>", "action": "<action>", "input": "<value>", "instruction": "<what to do next>" } }
```

The tool reads `input.json`, writes the result **atomically**, and exits. Logs
go to stderr only. The result is the file — not stdout. Go tools use the shared
`github.com/melodyogonna/solai/ipc` package (`ipc.ReadInput`, `ipc.WriteSuccess`,
`ipc.WriteError`, `ipc.WriteRequest`) to get the atomic write for free.

### manifest.json

```json
{
  "name": "token-price",
  "description": "Fetches current USD prices for Solana tokens from Jupiter.",
  "version": "1.0.0",
  "executable": "./bin/token-price",
  "required_capabilities": ["network-manager"],
  "payloads": [
    { "name": "wallet_address", "description": "Solana wallet public key", "source": "wallet" }
  ],
  "env": [
    { "name": "JUPITER_API_KEY", "sensitive": true, "required": false }
  ]
}
```

`payloads` declares named values the tool expects in `input.json`'s `payload`
map. Entries with a `source` (e.g. `"wallet"`) are resolved automatically;
others are supplied by the coordinator when fulfilling a capability request.

---

## Build

```bash
make all     # .out/solai and .out/solaid
make test    # go test ./...
```

All Go code is a single module at the repo root
(`github.com/melodyogonna/solai`).

---

## Architecture

```
client (curl) ─▶ solaid ──launch(cmd)──▶ agent runtime ──exec──▶ tool
                   │                          │
                   └── in-memory sessions     └── in-process capabilities
```

| Component | Responsibility |
|---|---|
| `cmd/solaid` | Control-plane binary |
| `cmd/solai` | Session runtime binary |
| `controlplane/` | Session manager, launcher, HTTP/SSE server |
| `session/` | Session spec, event stream, continuous goal loop |
| `runtime/` | Wires environment configuration and a spec into a session |
| `llm/` | Dependency-free OpenRouter client + native tool-calling loop |
| `ipc/` | File-based tool IPC protocol: typed input/output, atomic writes |
| `capability/` | Wallet, Solana RPC, memory, tool IPC, and the capability broker |
| `tool/` | Tool discovery, manifest parsing, subprocess execution |
| `wallet/` | BIP39 seed derivation and ed25519 keypair |
| `config/` | Environment-based configuration |
| `prompts/` | Embedded system prompt |
| `registry/` | Tool installation from GitHub releases (library) |
| `ratelimit/` | Retry and rate-limit strategies |

See `plan.md` for the full design.
