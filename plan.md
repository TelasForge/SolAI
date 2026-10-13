# SolAI — Architecture Plan

## Overview

An autonomous AI agent that interacts with the Solana blockchain. Built in Go with a small dependency-free client for the [OpenRouter](https://openrouter.ai) Chat Completions API and native function/tool calling as the reasoning engine — no LLM framework. OpenRouter unifies access to Google, OpenAI, Anthropic, and other models behind a single key and model slug. The agent runs in a continuous loop, coordinating a suite of user-installed agentic tools to accomplish user-defined goals. If a goal cannot be accomplished with available tools, the agent reports this rather than hallucinating actions.

![Solai Architecture diagram](https://res.cloudinary.com/melodyogonna/image/upload/v1773001206/solai-architecture_cqawjr.png)
_Original architecture diagram. Some details have changed in the implementation but the core vision is the same_

---

## Core Design Principles

- **Hierarchical multi-agent system.** SolAI is a coordinator agent that delegates to subagents. Each agentic tool is itself an LLM agent — it receives a prompt from the main agent, reasons about it using its own LLM, calls the specific capability it wraps (an API, an RPC endpoint, a smart contract), and returns a structured result.
- **The coordinator plans; subagents execute.** The main agent never calls external APIs or executes transactions directly. It decomposes goals into prompts and dispatches them to the appropriate subagent tools.
- **Two-layer memory across cycles.** A new agent run is started each cycle, but state is carried forward via two mechanisms: (1) the recent conversation history is passed into each cycle's `llm.Agent.Run` and trimmed on user-turn boundaries (`llm.TrimHistory`); (2) an explicit `memory` capability tool the LLM calls to persist structured state (current plan, observations, pending tasks). Together they allow multi-cycle strategies without unbounded context growth.
- **Native tool calling.** Tools are advertised as JSON-schema function definitions; the model returns structured `tool_calls` rather than ReAct-formatted text. This removes the fragile text parsing (and the associated recovery hacks) required by the previous implementation.
- **Tools as plugins.** Agentic tools are packaged and distributed as GitHub releases, installable via `solai install`. No code changes to the agent are required to add a tool.

---

## Startup Flow

```
solai start
  └─ Load ~/.solai/config.json
  └─ Validate: model.name, provider.openrouter, and user_goals must be set
  └─ Build OpenRouter client (llm.NewClient)
  └─ Register capabilities: communication, wallet, solana, network-manager, memory
  └─ agent.Run(ctx, cfg, capManager)
```

---

## Coordinator Cycle Loop

Cycles run back-to-back with no sleep between them. Each cycle has a `cycle_timeout` (default 5m) after which it is cancelled and a new one starts immediately.

```
SystemManager.Setup()
  └─ Discover tools from ~/.solai/tools/ — load manifests, validate capabilities
  └─ Log configured LLM provider

buildCyclePrompt()   [rebuilt every cycle]
  └─ ## Agent Memory — injected from MemoryCapability.BuildMemorySection() if non-empty
  └─ ## Your Goals — user goals from config
  (tool definitions are advertised natively via the function-calling API)

runCycle()  [llm.Agent, max 10 tool-calling iterations, exponential retry up to 3×]
  └─ Model returns structured tool_calls; the agent executes each and feeds results back
  └─ Each tool call to an agentic tool spawns a subagent process:
       allocates temp IPC dir; writes input.json (prompt + payload map, with auto-injected values merged in)
       appends Requestable Capabilities section to tool prompt so inner LLM can compose capability requests
       runs tool; on "request" output: coordinator LLM reads instruction and acts accordingly (typically calls capability, re-invokes tool with result in payload)
       reads final {"type":"success"|"error",...} from output.json
  └─ On transient error (e.g. LLM rate limit): retry with 2s→30s exponential backoff
  └─ On llm.ErrMaxIterations: log warning, no retry, continue to next cycle
  └─ On cycle_timeout: log warning, continue to next cycle
```

---

## Agentic Tools

### What they are

Agentic tools are **LLM subagents** — each one wraps a specific capability (a Solana RPC call, a DEX API, a price feed, etc.) and exposes it to the coordinator as a single callable tool. When the coordinator invokes a tool, the tool binary:

1. Receives a natural-language prompt from the coordinator (overview + task list)
2. Spins up its own internal LLM agent
3. Uses that agent to reason about the prompt and call its wrapped capability
4. Returns a structured result to the coordinator

This means the coordinator never needs to know the details of how a capability works — it just describes what it wants accomplished and the subagent figures out how.

Agentic tools are standalone executables (any language) distributed as GitHub releases. Each has a subdirectory under `~/.solai/tools/`:

```
~/.solai/tools/<name>/
  manifest.json        — metadata, executable path, capabilities required
  bin/<name>           — statically linked binary (chmod 0755)
```

### IPC protocol

The coordinator communicates with each subagent via **file-based JSON IPC**. Before spawning the tool, the coordinator allocates a temporary directory (`$SOLAI_IPC_DIR`) and writes `input.json` there. The tool reads `$SOLAI_IPC_DIR/input.json` on startup, does its work, writes `$SOLAI_IPC_DIR/output.json`, and exits. The coordinator reads the output after the process terminates.

`$SOLAI_IPC_DIR` is a host temporary directory unique to each invocation.

**`input.json`** (coordinator → subagent):
```json
{
  "prompt": "One sentence describing the objective.",
  "payload": {"wallet_address": "ABC...XYZ"}
}
```

`prompt` is always present and describes what to do. `payload` is a flat `map[string]string` carrying named values the tool may need (e.g. `wallet_address`, `signed_transaction`). Entries declared in the manifest with a `source` field are auto-injected by the coordinator at load time; others are supplied by the coordinator LLM on subsequent invocations when fulfilling a capability request.

The coordinator also appends a `## Requestable Capabilities` section to every tool's `prompt` describing which `Regular` capabilities the inner tool LLM may request and the exact JSON format for doing so. The inner LLM generates these requests dynamically — no logic is hardcoded in tool binaries.

**`output.json`** (subagent → coordinator):
```json
{ "type": "success", "payload": <any JSON value> }
{ "type": "error",   "payload": "human-readable error string" }
{ "type": "request", "payload": { "capability": "<name>", "action": "<action>", "input": "<value>", "instruction": "<natural language for the coordinator>" } }
```

`success` and `error` are terminal — the coordinator reads them and returns the result to its tool-calling loop. `request` is a capability request: the tool writes it and exits immediately (no blocking). The coordinator LLM reads the `instruction` field (natural language composed by the inner tool LLM) and follows it — the instruction may say to re-invoke the tool with the result in `payload`, call another capability first, or anything else. No re-invocation logic is hardcoded in infrastructure.

**Capability request / re-invocation flow:**

When the coordinator observes a `request` output:
1. Calls the named capability with the specified `action` and `input`.
2. Follows the `instruction` — typically: re-invokes the same tool with a new `input.json` where `payload` contains the capability result under the appropriate key.
3. The tool's response to the re-invocation is the final result for this step.

Only `Regular` capabilities are requestable this way — `Core` and `Internal` capabilities are not accessible to tools.

### Tool execution

Tools run as ordinary child processes. The coordinator sets `cmd.Dir` to the tool directory, resolves the executable to an absolute path, and injects the resolved env (LLM credentials, `SOLAI_IPC_DIR`, and any `tool-env.*` values). There is no OS-level sandbox; the `required_capabilities` field is validated against the registered capabilities but grants no permissions.

### Tool manifest format

```json
{
  "name": "<tool-name>",
  "description": "What this tool does.",
  "version": "1.0.0",
  "executable": "./bin/<tool-name>",
  "required_capabilities": ["network-manager"],
  "payloads": [
    {"name": "wallet_address",     "description": "Solana wallet public key", "source": "wallet"},
    {"name": "signed_transaction", "description": "Base64-encoded signed Solana transaction"}
  ],
  "env": [
    { "name": "SOME_API_KEY", "sensitive": true,  "required": true  },
    { "name": "SOME_BASE_URL", "sensitive": false, "required": false }
  ],
  "llm_options": {
    "primary": "anthropic/claude-sonnet-4",
    "supported": [
      { "model": "anthropic/claude-sonnet-4" },
      { "model": "openai/gpt-4o" }
    ]
  }
}
```

`payloads` declares named data the tool expects in `input.json`'s `payload` map. Each entry has:
- `name`: the key in the `payload` map
- `description`: shown to the coordinator LLM as part of the tool's description (so it knows what to supply on re-invocation)
- `source` (optional): a capability name — if set, the coordinator resolves the value automatically at load time by calling that capability (e.g. `"wallet"` → wallet address). No LLM involvement for sourced entries.

`llm_options` is required for any tool that uses an internal LLM agent (which is the expected pattern). Each entry is an OpenRouter model slug (`vendor/model`); the `primary` model is preferred, with the `supported` list as fallback order. The coordinator injects `SOLAI_LLM_MODEL`, `SOLAI_LLM_API_KEY`, and (optionally) `SOLAI_LLM_BASE_URL` into the subagent's environment. Tools that are purely deterministic (no reasoning required) may omit `llm_options`.

`env` declares runtime variables the tool needs. Values are set via `solai config set tool-env.<tool>.<VAR> <value>` and injected into the tool's environment at runtime. `sensitive: true` causes the value to be redacted in `config list` output. `required: true` causes the agent to refuse to load the tool if the value is missing.

### Tool installation

```
solai install <name[@tag]>          — curated registry (index.json hosted on GitHub)
solai install owner/repo[@tag]      — direct GitHub release install
solai uninstall <name>              — remove installed tool

Short-name path:
  └─ Fetch registry/index.json from GitHub
  └─ Resolve name → versioned asset URLs
  └─ Download manifest.json, binary, verify SHA256 against checksums.txt (if present)
  └─ Write to ~/.solai/tools/{name}/

GitHub path:
  └─ GET github.com/repos/{owner}/{repo}/releases/{latest|tag}
  └─ Download manifest.json → validate name, description, executable fields
  └─ Download {name}-linux-{amd64|arm64}
  └─ Verify SHA256 against checksums.txt (if present)
  └─ Write to ~/.solai/tools/{name}/manifest.json
  └─ Write to ~/.solai/tools/{name}/bin/{name} (chmod 0755)

Both paths: abort early if the tool is already installed.
Tool name from remote manifest is validated against [a-z0-9][a-z0-9-]* before any file operations.
```

---

## Capabilities

Capabilities are system-level features registered at startup. They run inside the agent process and serve one or more of three roles: injecting context into the coordinator prompt, acting as callable tools for the coordinator LLM, or being requestable by agentic tools at runtime.

| Class | Accessible to | Purpose |
|---|---|---|
| `Core` | Nobody | Background infrastructure; never exposed to any LLM |
| `Internal` | Coordinator LLM only | Exposed as callable tools to the coordinator; description injected into the cycle prompt |
| `Regular` | Coordinator LLM **and** agentic tools | Same as Internal, plus tools can request actions via the capability request protocol at runtime |

The distinction between `Internal` and `Regular` is intentional: some capabilities (e.g. Solana RPC) hold private keys and should only be callable by the trusted coordinator LLM. Others (e.g. wallet signing) are safe to expose to tools under the coordinator's supervision — the coordinator LLM reviews each tool invocation before it runs.

A `Regular` capability may also have no runtime actions (e.g. `network-manager`) — in that case `ToolRequestDescription()` returns empty and no request docs are generated, but the capability still appears in the coordinator prompt and validates tool manifests that declare it.

Capabilities with an empty `Description()` (e.g. `network-manager`) are excluded from both the coordinator's tool list and the `## Available Tools` prompt section. This prevents infrastructure-only capabilities from confusing the LLM with misleading action suggestions.

### Implemented capabilities

| Name | Class | Effect |
|---|---|---|
| `system-manager` | Core | Owns tool loading, LLM provider logging, cleanup jobs |
| `wallet` | Regular | Derives ed25519 keypair from BIP39 seed; provides `address` and `sign` actions requestable by tools; coordinator can also call it directly |
| `network-manager` | Regular | Declarative marker for tools that use outbound network access; no runtime request actions |
| `solana` | Regular | Solana RPC access (balance, transfer, blockhash, send_transaction, account_info); callable by the coordinator LLM directly, and requestable by agentic tools via the capability request protocol (e.g. for transaction signing and submission) |
| `memory` | Internal | Two-layer cross-cycle state: (a) trimmed conversation history (last few user turns, automatic); (b) structured plan/observation/task store callable by coordinator LLM |

### Planned capabilities

| Name | Class | Effect |
|---|---|---|
| `file-manager` | Regular | Grants tools access to user-configured paths (not yet implemented) |
| `web-ui` | Internal | Web interface for configuring agent and tools |

---

## Configuration

Stored in `~/.solai/config.json`. Managed via `solai config set/get/list`.

```json
{
  "model": {
    "name": "anthropic/claude-sonnet-4"
  },
  "providers": {
    "openrouter": ""
  },
  "openrouter_base_url": "",
  "wallet_seed": "",
  "cycle_timeout": "5m",
  "user_goals": "",
  "tool_env": {
    "<tool-name>": {
      "VAR_NAME": "value"
    }
  }
}
```

`model.name` selects the coordinator's LLM as an OpenRouter model slug. `providers.openrouter` holds the API key shared by the coordinator and every agentic tool; `openrouter_base_url` optionally overrides the API root.

Because OpenRouter fronts every vendor, there is a single credential. The coordinator and tools may still use different models by setting different slugs — a tool whose `llm_options.primary` is `openai/gpt-4o` uses that model with the same key.

`cycle_timeout` caps the duration of a single coordinator cycle (default 5m). There is no sleep between cycles.

`tool_env` holds per-tool runtime variables declared in the tool's `env` manifest field. Set via `solai config set tool-env.<tool>.<VAR> <value>`. Sensitive values are redacted in `config list`.


---

## Shared modules

| Module | Purpose |
|---|---|
| `llm` | Dependency-free OpenRouter Chat Completions client (`Client`) plus a native tool-calling agent loop (`Agent`). Shared by the coordinator and every agentic tool. |
| `ratelimit` | `RateLimitStrategy` and `RetryStrategy` interfaces + implementations. Used by the agent (exponential retry on LLM calls) and by tools (fixed-window rate limiting for external APIs). |

### Implementations

| Type | Struct | Parameters |
|---|---|---|
| Rate limit | `FixedWindowLimiter` | `limit int`, `window time.Duration` — at most N calls per window |
| Retry | `ExponentialRetry` | `maxAttempts`, `initialDelay`, `maxDelay`, `multiplier` — backs off exponentially, respects context cancellation |

---

## What is not yet implemented

| Feature | Notes |
|---|---|
| Web UI | Planned long-term; listed as an Internal capability |
| Tool isolation | Tools currently run as ordinary subprocesses; OS-level sandboxing may be reintroduced later as an opt-in |
