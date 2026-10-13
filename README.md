# SolAI

> **Status:** SolAI is implemented as a minimal agent harness. See `plan.md` for
> the design and `AGENT.md` for contributor guidance.

SolAI runs long-lived Solana agents. A user starts a **session** with one or
more **goals**, chooses the **tools** and **capabilities** available to it, and
the agent works continuously until the goals are resolved or the session is
stopped. LLM access is via [OpenRouter](https://openrouter.ai).

---

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/melodyogonna/solai/main/install.sh | bash
```

Downloads the latest `solai` binary for your architecture (`x86_64` or
`aarch64`) into `~/.local/bin`. The script prints a `PATH` hint if needed.

**Requirements:** Linux (x86\_64 or aarch64) and internet access to reach the
OpenRouter API and any external services your tools call.

---

## Concepts

- **Session** — one long-running agent run. Config: goals, model, tools,
  capabilities. It runs until the goals are done or it is stopped.
- **Goal** — a task the session must accomplish. The agent works through goals
  one at a time and explicitly marks each `complete`, `fail`, or `block`.
- **Tool** — a standalone executable with a `manifest.json` that the agent can
  call. Tools are selected per session.
- **Capability** — a system feature (wallet, Solana RPC, memory) selected per
  session. Capabilities are exposed to the coordinator as tools and are reachable
  by agentic tools through the `request` protocol: the tool emits a request and
  the runtime's broker performs it.

## Run a session

For a quick local run, `solai start` builds a session from `user-goals` and the
configured model and runs it on this host.

The agent runtime reads a session spec from stdin and streams events as
JSON lines on stdout:

```bash
solai __runtime < session.json
```

```json
{
  "goals": ["Monitor SOL price and report significant moves"],
  "model": "anthropic/claude-sonnet-4",
  "tools": ["token-price"],
  "capabilities": ["wallet", "solana", "memory"]
}
```

The control plane launches the runtime this way. Sandboxing is **not** this
code's concern: the operator wraps the launch command with their own
infrastructure (e.g. a microVM runner). The runtime only launches commands.

## Control-plane API

`solaid` creates, launches, and tracks sessions and relays their events. State
is in-memory. Each session is launched as a command, so the operator can point
`--launch` at a sandbox wrapper (e.g. a microVM runner).

```bash
solaid --addr 127.0.0.1:8787 --launch "solai __runtime"

# Start a session
curl -sX POST localhost:8787/sessions -d '{
  "goals": ["Monitor SOL and report big moves"],
  "model": "anthropic/claude-sonnet-4",
  "tools": ["token-price"],
  "capabilities": ["wallet", "solana", "memory", "network-manager"]
}'

# Watch it live
curl -N localhost:8787/sessions/<id>/events
```

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

Configuration lives in `~/.solai/config.json` and is managed with
`solai config set/get/list`.

**Model & credentials:**

| Key | Description |
|---|---|
| `model.name` | OpenRouter model slug for the coordinator (e.g. `anthropic/claude-sonnet-4`) |
| `provider.openrouter` | OpenRouter API key (shared by the agent and tools) |
| `openrouter.base-url` | Optional OpenRouter API root override |

**Agent settings:**

| Key | Description |
|---|---|
| `wallet-seed` | BIP39 mnemonic — a new wallet is generated if unset |

**Solana settings:**

| Key | Description |
|---|---|
| `solana.rpc-url` | Solana RPC endpoint (default: `https://api.mainnet-beta.solana.com`) |
| `solana.commitment` | `finalized`, `confirmed`, or `processed` (default: `confirmed`) |

**Tool environment variables:**

```bash
solai config set tool-env.<tool>.<VAR_NAME> <value>
```

Values are injected into the tool's environment at runtime and redacted in
`config list` output.

---

## Tools

### Installing

```bash
solai install token-price              # latest curated release
solai install token-price@v1.0.0       # specific version
solai install owner/repo               # third-party tool from a GitHub release
solai uninstall <name>
```

Tools are stored in `~/.solai/tools/`. A release must contain `manifest.json`,
`<name>-linux-<arch>` binaries, and (optionally) `checksums.txt`.

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

### Adding a tool locally

1. Create `~/.solai/tools/my-tool/manifest.json` (`executable` must be `"./bin/my-tool"`).
2. Place the binary at `~/.solai/tools/my-tool/bin/my-tool` (chmod 0755).
3. It is discovered on the next session that selects it.

---

## Architecture

```
client (curl) ─▶ solaid ──launch(cmd)──▶ agent runtime ──exec──▶ tool
                   │                          │
                   └── in-memory sessions     └── in-process capabilities
```

| Component | Responsibility |
|---|---|
| `solaid` | Control-plane HTTP/SSE API; session lifecycle; launches the runtime |
| agent runtime | One session's goal loop; emits events |
| `llm/` | Dependency-free OpenRouter client + native tool-calling loop |
| `ipc/` | File-based tool IPC protocol: typed input/output, atomic writes |
| `solai-agent/` | Session runtime (`session/`), control plane (`controlplane/`, `cmd/solaid`), and CLI (`cli/`) |
| `tools/*` | Agentic tools |
| `registry/` | Tool installation from GitHub releases |
| `ratelimit/` | Retry and rate-limit strategies |

See `plan.md` for the full design and build order.
