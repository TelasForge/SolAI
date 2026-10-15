package capability

import "context"

// CapabilityClass defines the visibility and access level of a capability.
type CapabilityClass int

const (
	// Core capabilities run invisibly in the background and are never
	// exposed to the LLM or to agentic tools.
	Core CapabilityClass = iota

	// Internal capabilities are exposed to the coordinator LLM as callable
	// tools and injected into the cycle prompt, but are private to the
	// coordinator — agentic tools cannot request them.
	Internal

	// Regular capabilities are exposed to the coordinator LLM exactly like
	// Internal capabilities, AND can additionally be requested by agentic
	// tools at runtime via the capability request protocol.
	Regular
)

// Capability is the interface that all system capabilities must implement.
type Capability interface {
	// Name returns the unique identifier for this capability.
	Name() string

	// Description returns a human-readable description used as the LLM tool
	// description. Both Internal and Regular capabilities expose this to the
	// coordinator LLM.
	Description() string

	// Class returns the CapabilityClass, determining visibility and access.
	Class() CapabilityClass

	// Execute performs the capability's action.
	Execute(ctx context.Context, input string) (string, error)

	// ToolRequestDescription returns documentation for agentic tools describing
	// the available actions and their input/output format for this capability.
	// Regular capabilities with runtime actions return a non-empty string here;
	// it is injected into every tool's system prompt so tools know what they
	// can request. Return "" for capabilities with no runtime-requestable actions
	// (e.g. capabilities that only affect runtime configuration).
	ToolRequestDescription() string
}
