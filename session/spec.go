// Package session implements the continuous, goal-driven agent runtime.
//
// A session is one long-running run: it receives a Spec (goals, model, tool and
// capability selection), works through its goals with the LLM, and emits a
// stream of Events. It never ends on a timer and never recreates the agent
// between steps — the whole point of replacing the old cycle loop.
//
// The runtime is sandbox-unaware: it only launches tool commands. Where those
// commands actually run is the surrounding infrastructure's concern.
package session

// Status is the lifecycle state of a session.
type Status string

const (
	StatusPending Status = "pending"
	StatusRunning Status = "running"
	StatusPaused  Status = "paused"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
	StatusStopped Status = "stopped"
)

// GoalStatus is the lifecycle state of a single goal.
type GoalStatus string

const (
	GoalPending GoalStatus = "pending"
	GoalRunning GoalStatus = "running"
	GoalDone    GoalStatus = "done"
	GoalFailed  GoalStatus = "failed"
	GoalBlocked GoalStatus = "blocked"
)

// Goal is a single objective within a session.
type Goal struct {
	ID     string     `json:"id"`
	Text   string     `json:"text"`
	Status GoalStatus `json:"status"`
	Note   string     `json:"note,omitempty"`
}

// MCPServer configures one Model Context Protocol server.
type MCPServer struct {
	// Name namespaces the server's tools (e.g. "jupiter" -> "jupiter__quote").
	Name string `json:"name"`

	// URL is the Streamable HTTP endpoint.
	URL string `json:"url"`

	// Headers are sent with every request (e.g. Authorization).
	Headers map[string]string `json:"headers,omitempty"`
}

// Spec is the input to a session. It is the entire user-facing configuration
// surface: what to accomplish, which model, and which tools/capabilities are
// available.
type Spec struct {
	// ID is an optional caller-provided identifier.
	ID string `json:"id,omitempty"`

	// Goals are the objectives the session must resolve, in order.
	Goals []string `json:"goals"`

	// Model is the OpenRouter model slug for the session.
	Model string `json:"model"`

	// Tools are the tool names to expose to the model. Empty means all
	// registered tools.
	Tools []string `json:"tools,omitempty"`

	// Capabilities are the infrastructure authorities the control plane
	// provisions into the session's environment (network, rpc, wallet). The
	// runtime never sees this list; it only sees the resulting environment.
	Capabilities []string `json:"capabilities,omitempty"`

	// MCPServers are Model Context Protocol servers whose tools the session
	// should expose. The runtime connects to them at session start and adds
	// their tools alongside the registered ones.
	MCPServers []MCPServer `json:"mcp_servers,omitempty"`

	// SystemPrompt overrides the runtime's default system prompt when non-empty.
	SystemPrompt string `json:"system_prompt,omitempty"`

	// SafetyCap bounds the number of model turns in the session. Zero uses a
	// default. Reaching the cap pauses the session rather than truncating it.
	SafetyCap int `json:"safety_cap,omitempty"`
}
