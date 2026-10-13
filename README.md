# Solana AI

> **Early development** — SolAI is experimental software. Expect breaking changes, missing features, and rough edges. Do not run it with a wallet holding funds you cannot afford to lose.

An autonomous AI agent for the Solana blockchain. SolAI runs a continuous reasoning loop powered by a user-configured LLM via [OpenRouter](https://openrouter.ai), coordinating a suite of external tools to accomplish user-defined goals — monitoring token prices, reading on-chain data, executing transactions, or anything else a tool can be built to do.

---

## Installation

### System requirements

- **OS:** Linux (x86\_64 or aarch64)
- **Internet access:** required to reach the OpenRouter API and any tools that call external services

### Install

```bash
curl -fsSL https://raw.githubusercontent.com/melodyogonna/solai/main/install.sh | bash
```

This downloads the latest `solai` binary for your architecture (`x86_64` or `aarch64`) and places it in `~/.local/bin`. If that directory is not on your `PATH` the script will print the exact command to add it.

## Quick start

```bash
# Select which OpenRouter model the coordinator uses (any model slug)
solai config set model.name anthropic/claude-sonnet-4

# Set the OpenRouter API key (shared by the coordinator and agentic tools)
solai config set provider.openrouter <your-openrouter-api-key>

solai config set user-goals "Monitor SOL price and report every cycle"

solai install token-price   # install a tool
solai start                 # launch the agent
```

---

## How it works

SolAI operates in a **native tool-calling loop**. Each cycle:

1. The agent is given a system prompt (its role and rules), the available tools, and the user goals.
2. It plans which tools to call and in what order.
3. It calls those tools, observes results, and adapts.
4. It produces a structured summary of what was accomplished, what failed, and what to try next.
5. It immediately begins the next cycle. Each cycle has a configurable timeout (`cycle-timeout`) to prevent runaway LLM calls.

State is carried across cycles in two ways:

- **Conversation window** — the most recent cycles of conversation history are shared with each new cycle so it has context on recent actions.
- **Memory capability** — the agent can explicitly persist a current plan, observations, pending tasks, and completed tasks across cycles by calling the built-in `memory` tool.

---

## CLI

```
solai install <name[@tag]>         Install a curated tool by short name
solai install <owner/repo[@tag]>   Install a third-party tool from a GitHub release
solai uninstall <name>             Remove an installed tool
solai config set <key> <value>     Set a configuration value
solai config get <key>             Get a configuration value
solai config list                  List all values (sensitive fields redacted)
solai start                        Start the autonomous agent
```

### Configuration keys

**Coordinator model** (required):

| Key | Description |
|---|---|
| `model.name` | OpenRouter model slug for the coordinator (e.g. `anthropic/claude-sonnet-4`, `openai/gpt-4o`, `google/gemini-2.5-pro`) |

**Provider credentials:**

| Key | Description |
|---|---|
| `provider.openrouter` | OpenRouter API key (shared by the coordinator and all agentic tools) |
| `openrouter.base-url` | Override the OpenRouter API root (optional; defaults to `https://openrouter.ai/api/v1`) |

**Agent settings:**

| Key | Description |
|---|---|
| `user-goals` | Goals the agent should pursue autonomously |
| `cycle-timeout` | Maximum time allowed for a single cycle before it is cancelled and the next begins (default: `5m`) |
| `wallet-seed` | BIP39 mnemonic — a new wallet is generated if unset |

**Solana settings:**

| Key | Description |
|---|---|
| `solana.rpc-url` | Solana RPC endpoint (default: `https://api.mainnet-beta.solana.com`) |
| `solana.commitment` | Commitment level: `finalized`, `confirmed`, or `processed` (default: `confirmed`) |

**Tool environment variables:**

Tools can declare runtime variables they need (e.g. API keys). Set them with:

```bash
solai config set tool-env.<tool>.<VAR_NAME> <value>
# example:
solai config set tool-env.birdeye.BIRDEYE_API_KEY abc123
```

Values are stored in `~/.solai/config.json` and injected into the tool's environment at runtime. All tool env values are redacted in `config list` output.

Configuration is stored in `~/.solai/config.json` and written atomically.

---

## Architecture

```
solai start
  └─ config.Load()                  ~/.solai/config.json
  └─ newLLM()                       OpenRouter client
  └─ agent.LoadConfigFrom()         reads model, wallet, cycle timeout
  └─ capability.SetUp()             communication, wallet, solana, network-manager, memory
  └─ agent.Run()                    autonomous cycle loop
       └─ SystemManager.Setup()     loads tools from ~/.solai/tools
       └─ SystemManager.Start(ctx)  background cleanup jobs
       └─ tool-calling cycle loop (shares recent conversation across cycles)
            └─ buildCyclePrompt()   injects agent memory + user goals
            └─ llm.Agent.Run()      native tool calls (exponential retry on transient errors)
                 └─ AgenticTool.Call()   writes input.json → spawns tool → reads output.json
```

### Packages

| Package | Responsibility |
|---|---|
| `cli/` | Cobra commands: `install`, `uninstall`, `config`, `start` |
| `config/` | `~/.solai/config.json` — load, save, set, get |
| `registry/` | Tool installation from GitHub releases |
| `agent/` | Autonomous cycle loop, prompt assembly, config loading |
| `capability/` | Capability system: wallet, solana, memory, LLM provider, SystemManager |
| `tool/` | Tool discovery, manifest parsing, file-based IPC, subprocess execution |
| `prompts/` | Embedded system prompt (`system.md`) |
| `wallet/` | BIP39 seed derivation, ed25519 keypair, Base58 encoding |
| `llm/` | Dependency-free OpenRouter client + native tool-calling agent loop (shared by the agent and tools) |
| `ratelimit/` | `ExponentialRetry` (agent cycles), `FixedWindowLimiter` (tool rate limiting) |

### Capabilities

Capabilities are system-level features injected at startup, separate from agentic tools. Each has a class that determines who can see and use it:

| Class | Accessible to | Purpose |
|---|---|---|
| `Core` | Nobody | Background infrastructure; never exposed to any LLM |
| `Internal` | Coordinator LLM only | Callable tool + injected into the cycle prompt |
| `Regular` | Coordinator LLM and agentic tools | Same as Internal, plus tools can request actions via the capability request protocol |

**Built-in capabilities:**

| Capability | Class | Description |
|---|---|---|
| `system-manager` | Core | Owns tool loading, LLM provider logging, cleanup job scheduling |
| `communication` | Core | Allocates per-invocation IPC directories for tool subprocess communication |
| `wallet` | Regular | Agent's Solana keypair — `address` and `sign` actions; address is auto-injected into tool payloads |
| `solana` | Regular | Solana RPC: `get_balance`, `transfer_sol`, `get_recent_blockhash`, `send_transaction`, `get_account_info` |
| `network-manager` | Regular | Declarative marker for tools that use outbound network access; no runtime actions |
| `memory` | Internal | Persists plan, observations, pending tasks, and completed tasks across cycles |

---

## Tools

### Installing tools

```bash
solai install token-price                       # latest release (core/curated tool)
solai install token-price@v1.0.0               # specific version
solai install owner/repo                        # third-party tool (latest release)
solai install owner/repo@v1.0.0                # third-party tool (specific tag)
```

Tools are downloaded from GitHub releases and stored in `~/.solai/tools/`. The release must include:

- `manifest.json` — tool manifest with `"executable": "./bin/<name>"`
- `<name>-linux-amd64` / `<name>-linux-arm64` — statically linked binary
- `checksums.txt` (optional) — SHA256 checksums for verification

### Writing a tool

Tools are standalone executables (any language) that communicate via file-based JSON IPC.

**IPC protocol:**

Before spawning a tool the coordinator allocates a temporary directory and sets `$SOLAI_IPC_DIR` in the tool's environment. The tool reads `$SOLAI_IPC_DIR/input.json` on startup, does its work, writes `$SOLAI_IPC_DIR/output.json`, and exits.

```json
// $SOLAI_IPC_DIR/input.json  (coordinator → tool)
{ "prompt": "One sentence describing the objective.", "payload": { "wallet_address": "ABC...XYZ" } }
```

```json
// $SOLAI_IPC_DIR/output.json  (tool → coordinator)
{ "type": "success", "payload": <any JSON value> }
{ "type": "error",   "payload": "human-readable error string" }
{ "type": "request", "payload": { "capability": "<name>", "action": "<action>", "input": "<value>", "instruction": "<natural language for the coordinator>" } }
```

`success` and `error` are terminal. `request` is a capability request: the tool writes it and exits immediately. The coordinator reads the `instruction` field and follows it — typically by calling the named capability and re-invoking the tool with the result in `payload`. No re-invocation logic is hardcoded in tool binaries; the inner tool LLM composes the request dynamically.

Tool errors are returned as strings in `payload` so the coordinator LLM can observe them in the tool-calling loop and adapt.

**manifest.json:**

```json
{
  "name": "token-price",
  "description": "Fetches current USD prices for Solana tokens from Jupiter.",
  "version": "1.0.0",
  "executable": "./bin/token-price",
  "required_capabilities": ["network-manager"],
  "payloads": [
    { "name": "wallet_address", "description": "Solana wallet public key", "source": "wallet" }
  ]
}
```

`payloads` declares named values the tool expects in `input.json`'s `payload` map. Entries with a `source` field (e.g. `"wallet"`) are resolved automatically at load time by calling that capability — no LLM involvement. Entries without a `source` are supplied by the coordinator LLM on re-invocation when fulfilling a capability request.

For tools that need runtime environment variables (e.g. API keys), declare them in `env`. The agent reads values from `tool-env.<name>.*` in the config and injects them into the tool's environment:

```json
{
  "name": "birdeye",
  "description": "...",
  "version": "1.0.0",
  "executable": "./bin/birdeye",
  "required_capabilities": ["network-manager"],
  "env": [
    { "name": "BIRDEYE_API_KEY", "sensitive": true,  "required": true  },
    { "name": "BIRDEYE_BASE_URL", "sensitive": false, "required": false }
  ]
}
```

- `required: true` — the agent refuses to load the tool if the value is not set; a clear error message points to the fix command
- `sensitive: true` — the value is redacted in `config list` output
- Values are set with: `solai config set tool-env.birdeye.BIRDEYE_API_KEY <value>`

For tools that need their own LLM, add `llm_options`:

```json
{
  "name": "my-tool",
  "description": "...",
  "version": "1.0.0",
  "executable": "./bin/my-tool",
  "llm_options": {
    "primary": "anthropic/claude-sonnet-4",
    "supported": [
      { "model": "anthropic/claude-sonnet-4" },
      { "model": "openai/gpt-4o" }
    ]
  }
}
```

The agent injects `SOLAI_LLM_MODEL`, `SOLAI_LLM_API_KEY`, and (optionally) `SOLAI_LLM_BASE_URL` into the tool's environment automatically. The OpenRouter model slugs (`vendor/model`) subsume the old provider split — any model reachable through OpenRouter can be listed.

### Adding a tool locally (without a GitHub release)

1. Create `~/.solai/tools/my-tool/manifest.json` (see format above, `executable` must be `"./bin/my-tool"`).
2. Place the binary at `~/.solai/tools/my-tool/bin/my-tool` (chmod 0755).
3. The tool is discovered automatically on the next `solai start`.
