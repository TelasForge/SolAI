# TODO

Open work for SolAI. Keep this current: add items as they come up and remove
them when done. The design lives in `plan.md`, user docs in `README.md`, and
agent guidance in `AGENTS.md`.

## Web UI
- [ ] Start form: let the user pick tools and capabilities (today it only sets
      goals, so a UI-started session gets no `rpc`/`wallet` provisioning)
- [ ] Sessions list: poll or refresh so it doesn't go stale after a session
      finishes
- [ ] Auth (session cookie or bearer token) before binding beyond loopback

## Control plane
- [ ] `solaid --launch` default should resolve the runtime next to its own
      executable rather than `$PATH`, so it can't launch a stale `solai`

## Tools & MCP
- [ ] MCP: stdio transport (currently Streamable HTTP only)
- [ ] MCP: auth headers / OAuth, per-server allow-deny, and treating tool
      results as untrusted input
- [ ] `GET /tools` should surface per-session MCP tools, not just built-ins
- [ ] Cost/token accounting and budgets per session

## Runtime
- [ ] Parallel tool calls: run independent calls concurrently, collect results
      by call index and append in order; bound the fan-out
- [ ] Context compaction: summarize the oldest history once it grows

## Deliberately out of scope (for now)
- Durable sessions / crash recovery
- Multi-user auth
- In-code sandboxing (it is surrounding infrastructure)
- Scheduling / triggers
- A rich UI (a minimal one exists)

## Housekeeping
- [ ] Rotate the test OpenRouter key and wallet seed written to `.env` — they
      have been exposed in logs
