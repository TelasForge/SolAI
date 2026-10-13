# SolAI — Minimal Harness Plan

SolAI is a harness for running long-lived Solana agents: a user starts a
**session** with one or more goals, chooses the **tools** and **capabilities**
available to it, and the agent runs until the goals are done or the session is
stopped. LLM access is via OpenRouter.

The plan is deliberately minimal. It describes the smallest system that
accomplishes that goal, with no durability, UI, or in-code sandboxing.

> **Status:** implemented. `solai start` runs a local session, `solai __runtime`
> is the control-plane-launched runtime, and `solaid` exposes the HTTP/SSE API.

## 1. Essence: two components

1. **Agent runtime** — runs exactly one session. **Sandbox-unaware.**
2. **Control-plane API** (`solaid`) — creates, launches, and tracks sessions
   and streams their events. **Sandbox-unaware.**

```
client (curl) ─▶ solaid ──launch(cmd)──▶ agent runtime ──exec──▶ tool
                   │                          │
                   └── in-memory sessions     └── in-process capabilities
```

## 2. Sandbox boundary

Sandboxing is **surrounding infrastructure**, not code in this repo. The
control plane launches each agent through a **configurable command** (default:
exec the runtime binary). To sandbox, the operator points that command at their
own infra, e.g. `msb run …` or any wrapper. The runtime launches tools the same
way — as a command. Nothing here links a sandbox or microVM SDK.

## 3. Session

The entire user-facing configuration surface:

```
{
  "id":           "<generated>",
  "status":       "pending | running | paused | done | failed | stopped",
  "goals":        [ "..." ],                    // one or more
  "model":        "anthropic/claude-sonnet-4",  // OpenRouter slug
  "tools":        [ "token-price" ],            // names from the registry
  "capabilities": [ "wallet", "solana", "memory" ]
}
```

No budgets, scheduling, or approvals yet.

## 4. Agent runtime

One process per session. It reads the session spec from **stdin**, loads only
the selected tools, and runs the goal loop.

### 4.1 Loop

- Call the model with the selected tools advertised natively.
- Execute returned tool calls; feed results back as tool messages.
- Repeat until **all goals are done**, the session is **stopped**, or a
  **safety iteration cap** trips. The cap **pauses and reports** — it never
  silently truncates.
- Goal completion is explicit: the runtime injects a `goal` tool the model calls
  with `complete`/`fail`/`block`/`note`. If the model stops without resolving a
  goal, the runtime re-prompts it to continue instead of ending the session.
- One continuous conversation for the life of the session.
- Context = the full message history. If it grows too large, summarize the
  oldest block and continue.

### 4.2 Tools

- A tool is an executable plus a `manifest.json`: `name`, `description`,
  JSON-Schema for arguments, `required_capabilities`.
- The runtime loads **only the session's selected tools**.
- A tool is a plain program. It may use an LLM internally if it needs to.
- Invocation is via **file IPC** (below).

### 4.3 File IPC protocol

The coordinator communicates with each one-shot tool through a unique temp
directory, passed as `SOLAI_IPC_DIR`.

```
$SOLAI_IPC_DIR/input.json    coordinator → tool
$SOLAI_IPC_DIR/output.json   tool → coordinator
```

- The coordinator creates the unique dir, writes `input.json`, then starts the
  tool with `SOLAI_IPC_DIR` set.
- The tool reads `input.json`, does its work, writes the result, and exits.
- The result file is written atomically (write `output.json.tmp`, then rename)
  so the coordinator never observes a partial file.
- The coordinator waits for process exit, then reads `output.json`.
- **Logs go to stderr only.** The result is the file; stdout/stderr are not
  part of the contract.
- Completion is signalled by *process exit + presence of a valid
  `output.json`*, not by pipe EOF. This avoids the stdin/stdout deadlock family
  (pipe-buffer stalls, grandchildren holding the pipe, `Wait`/pipe ordering,
  log/result stream corruption) and works identically across a sandbox/VM
  boundary via a mounted volume.

`input.json`:

```json
{ "prompt": "Objective for this tool call.", "payload": { "k": "v" } }
```

`output.json` (one of):

```json
{ "type": "success", "payload": <any JSON> }
{ "type": "error",   "payload": "human-readable error" }
{ "type": "request", "payload": { "capability": "...", "action": "...", "input": "...", "instruction": "..." } }
```

A `request` output asks the runtime to invoke the named capability on the tool's
behalf. The runtime (or, with no broker configured, the coordinator) performs it
and returns the capability result together with the tool's `instruction`; the
coordinator then follows the instruction, typically by re-invoking the tool with
the result in `payload`. A missing or unparseable `output.json`, or a nonzero
exit with no result, is an error carrying the stderr tail.

### 4.4 Capabilities

- Capabilities run **in the agent runtime process**; selected per session.
- Tools reach them through the `request` protocol above. The runtime's **broker**
  (`capability.Broker`) executes each request; only `Regular` capabilities are
  requestable by tools.
- Selected capabilities are also exposed to the coordinator as tools.
- Minimal built-ins: `wallet`, `solana`, `memory`.

## 5. Control-plane API (`solaid`)

The API is the deliverable. No UI.

```
POST   /sessions                 create + start (session spec in §3)
GET    /sessions                 list
GET    /sessions/{id}            status + goals
POST   /sessions/{id}/stop
GET    /sessions/{id}/events     SSE live event stream
GET    /tools                    available tools
GET    /capabilities             available capabilities
```

- Session state is **in-memory**. No database, no migrations, no crash recovery.
- Per session, `solaid` spawns the runtime command, passes the session spec,
  and relays the runtime's event stream to SSE. The launch command is the
  sandbox seam (§2).

## 6. Non-goals

Web UI · database/durability · crash recovery · auth/multi-user · implementing
sandboxes or any microVM SDK · cost dashboards · scheduling/triggers · approval
gates · OCI tool registry · per-tool model selection.

## 7. Build order

1. ✅ **Session runtime** — session spec in; continuous goal loop; event stream
   out. (`solai-agent/session`)
2. ✅ **Control-plane API** — session CRUD + process supervisor + SSE, in-memory.
   (`solai-agent/controlplane`, `cmd/solaid`)
3. ✅ **Tool protocol** — selected-tools-only loading; file IPC with atomic
   `output.json` writes and the exit-code/result discriminator. (shared `ipc/`
   module, `tool.Select`, `tool.RunTool`)
4. ✅ **Capability broker** — expose the selected capabilities to tools via the
   `request` protocol. (`capability.Broker`, brokering in `tool.AgenticTool`)

The runtime runs a continuously-running, in-memory goal loop, with "where it
runs" left entirely to the operator's infrastructure.
