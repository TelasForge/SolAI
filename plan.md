# SolAI — Minimal Harness Plan

SolAI is a harness for running long-lived Solana agents: a user starts a
**session** with one or more goals and a selection of **tools**, and the agent
runs until the goals are done or the session is stopped. LLM access is via
OpenRouter.

> **Status:** implemented. `solai` is the control-plane-launched session
> runtime; `solaid` exposes the HTTP/SSE API and provisions capabilities.
> Configuration is read from the process environment.

## 1. Essence: two components

1. **Agent runtime** (`cmd/solai`) — runs exactly one session. **Sandbox-unaware.**
2. **Control-plane API** (`solaid`) — creates, launches, and tracks sessions,
   provisions capabilities, and streams events. **Sandbox-unaware.**

```
client (curl) ─▶ solaid ──launch(cmd + capability env)──▶ runtime
                   │                                        │
                   └── in-memory sessions                   └── in-process tools
```

## 2. Sandbox boundary

Sandboxing is **surrounding infrastructure**, not code in this repo. The
control plane launches each session through a **configurable command** (default:
the runtime binary). To sandbox, the operator points that command at their own
infra, e.g. `msb run …` or any wrapper. Nothing here links a sandbox or microVM
SDK.

## 3. Session

The entire per-session configuration surface:

```
{
  "goals":        [ "..." ],                    // one or more
  "model":        "anthropic/claude-sonnet-4",  // optional OpenRouter slug
  "tools":        [ "jupiter-price" ],          // registered tool names
  "capabilities": [ "network", "rpc" ]          // infrastructure grants
}
```

`tools` empty means every registered tool. `capabilities` is not seen by the
runtime — it is the control plane's instruction for what to provision.

Global configuration (model, OpenRouter key, wallet seed, RPC endpoint) is read
from the process environment (see `README.md`).

## 4. Agent runtime

One process per session. It reads the session spec from **stdin** and runs the
goal loop.

### 4.1 Loop

- Call the model with the selected tools advertised natively.
- Execute returned tool calls; feed results back as tool messages.
- Repeat until **all goals are done**, the session is **stopped**, or a
  **safety iteration cap** trips. The cap **pauses and reports**.
- Goal completion is explicit: the runtime injects a `goal` tool the model calls
  with `complete`/`fail`/`block`/`note`. If the model stops without resolving a
  goal, the runtime re-prompts it to continue.
- One continuous conversation for the life of the session.

### 4.2 Tools

- A tool is an in-process value implementing `llm.Tool` (`Name`, `Description`,
  `Parameters`, `Call`). Tools are compiled into the runtime and called
  directly — no manifests, no subprocesses, no IPC.
- Tools register at compile time (`tool.Register`) and a session selects a
  subset by name (`tool.SetUp`).
- Adding a tool means adding a Go package and blank-importing it in
  `tools/tools.go`.

### 4.3 MCP servers

External integrations can be provided by Model Context Protocol servers. A
session lists them in `mcp_servers`; the runtime connects over the Streamable
HTTP transport, discovers their tools (`tools/list`), and adapts them to
`llm.Tool` (`mcp/`). Names are namespaced `<server>__<tool>`.

The client is a hand-rolled JSON-RPC implementation — no MCP SDK.

## 5. Control-plane API (`solaid`)

```
POST   /sessions                 create + start (session spec)
GET    /sessions                 list
GET    /sessions/{id}            status + goals
POST   /sessions/{id}/stop
GET    /sessions/{id}/events     SSE live event stream
GET    /tools                    registered tools
GET    /capabilities             available capabilities
```

- Session state is **in-memory**. No database, no crash recovery.
- Per session, `solaid` provisions the requested capabilities into the launch
  environment, spawns the runtime command, and relays its events to SSE.

### 5.1 Capabilities

A capability is an authority the control plane provisions into a session's
environment. The runtime does not enforce it; the platform does. A real
deployment maps each capability to sandbox policy (network egress, mounts,
secrets) in addition to the environment variables:

| Capability | Grants |
|---|---|
| `network` | Outbound network access (platform-enforced) |
| `rpc` | `SOLAI_RPC_URL`, `SOLAI_COMMITMENT` |
| `wallet` | `SOLAI_WALLET_SEED` |

## 6. Non-goals

Web UI · database/durability · crash recovery · auth/multi-user · implementing
sandboxes or any microVM SDK · third-party/out-of-tree tools · scheduling.

## 7. Status

| Piece | State |
|---|---|
| Session runtime + continuous goal loop | done |
| Control-plane API (HTTP/SSE, in-memory) | done |
| In-process tool registry | done |
| Built-in tools (solana, wallet, jupiter, raydium, tokenprice) | done |
| MCP client (Streamable HTTP) | done |
| Capability provisioning | done |
| Parallel tool calls | future |
