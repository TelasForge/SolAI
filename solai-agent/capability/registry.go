package capability

import (
	"context"
	"fmt"
	"strings"

	"github.com/melodyogonna/solai/llm"
)

// Factory is a function that creates a new Capability instance.
// Used for lazy initialization — the factory is called once during SetUp.
type Factory func() Capability

// registry holds all registered capability factories, keyed by capability name.
var registry = make(map[string]Factory)

// Register adds a capability factory to the global registry.
// Typically called from init() functions in capability implementation files,
// or from main() before calling SetUp.
func Register(name string, factory Factory) {
	registry[name] = factory
}

// CapabilityChecker checks whether Regular capabilities are available.
// Implemented by CapabilityManager; accepted by the tool loader to validate
// required_capabilities declarations without creating a circular import.
type CapabilityChecker interface {
	IsRegularCapabilityAvailable(name string) bool
}

// CapabilityManager holds the active set of capabilities for a running agent.
type CapabilityManager struct {
	capabilities []Capability
}

// SetUp creates a CapabilityManager from a list of capability names.
// Names not found in the registry are silently skipped.
// Call Register() for all capabilities before calling SetUp.
func SetUp(names []string) *CapabilityManager {
	cm := &CapabilityManager{}
	for _, name := range names {
		if factory, ok := registry[name]; ok {
			cm.capabilities = append(cm.capabilities, factory())
		}
	}
	return cm
}

// GetByClass returns all capabilities of the given class.
func (m *CapabilityManager) GetByClass(class CapabilityClass) []Capability {
	var result []Capability
	for _, c := range m.capabilities {
		if c.Class() == class {
			result = append(result, c)
		}
	}
	return result
}

// GetAll returns all active capabilities regardless of class.
func (m *CapabilityManager) GetAll() []Capability {
	return m.capabilities
}

// IsRegularCapabilityAvailable reports whether a Regular capability with the
// given name is currently registered in the manager. Used by the tool loader
// to validate tool required_capabilities declarations at startup.
func (m *CapabilityManager) IsRegularCapabilityAvailable(name string) bool {
	for _, c := range m.capabilities {
		if c.Class() == Regular && c.Name() == name {
			return true
		}
	}
	return false
}

// capabilityTool adapts a Capability to the llm.Tool interface so Internal and
// Regular capabilities can be called by the LLM via native tool calling.
type capabilityTool struct{ c Capability }

func (t capabilityTool) Name() string        { return t.c.Name() }
func (t capabilityTool) Description() string { return t.c.Description() }
func (t capabilityTool) Call(ctx context.Context, argsJSON string) (string, error) {
	return t.c.Execute(ctx, argsJSON)
}

// Parameters advertises an open object schema: capabilities document their
// argument shape in Description() and parse the raw JSON themselves.
func (t capabilityTool) Parameters() map[string]any { return llm.OpenObjectSchema() }

// GetAgentTools returns Internal and Regular capabilities wrapped as llm.Tools
// so the coordinator agent can call them directly. Capabilities with an empty
// Description are excluded — they are infrastructure concerns (e.g.
// network-manager) that the LLM does not need to invoke.
func (m *CapabilityManager) GetAgentTools() []llm.Tool {
	var tools []llm.Tool
	for _, c := range m.capabilities {
		if (c.Class() == Internal || c.Class() == Regular) && c.Description() != "" {
			tools = append(tools, capabilityTool{c})
		}
	}
	return tools
}

// GetByName returns the capability with the given name, or nil if not found.
func (m *CapabilityManager) GetByName(name string) Capability {
	for _, c := range m.capabilities {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

// BuildToolRequestSection generates a documentation block for all Regular
// capabilities that expose ToolRequestDescription. This is injected into
// agentic tool prompts so inner LLMs know what capabilities they can request
// from the coordinator, and the exact request format.
func (m *CapabilityManager) BuildToolRequestSection() string {
	var parts []string
	for _, c := range m.capabilities {
		if c.Class() == Regular {
			if desc := c.ToolRequestDescription(); desc != "" {
				parts = append(parts, fmt.Sprintf("- **%s**: %s", c.Name(), desc))
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	header := "## Requestable Capabilities\n\n" +
		"If you need coordinator help (e.g. signing a transaction), output a JSON object " +
		"as your Final Answer instead of a text result:\n" +
		"{\"capability\":\"<name>\",\"action\":\"<action>\",\"input\":\"<data>\"," +
		"\"instruction\":\"<tell the coordinator what to do after, e.g. re-invoke this tool with the result>\"}\n\n" +
		"Available:\n"
	return header + strings.Join(parts, "\n")
}

// BuildCapabilityPromptSection generates a Markdown block describing all
// Internal and Regular capabilities for injection into the coordinator's
// per-cycle prompt. Capabilities with an empty Description are omitted.
// Returns an empty string if none remain.
func (m *CapabilityManager) BuildCapabilityPromptSection() string {
	var visible []Capability
	for _, c := range m.capabilities {
		if (c.Class() == Internal || c.Class() == Regular) && c.Description() != "" {
			visible = append(visible, c)
		}
	}
	if len(visible) == 0 {
		return ""
	}
	var b strings.Builder
	for i, c := range visible {
		fmt.Fprintf(&b, "- **%s**: %s", c.Name(), c.Description())
		if i < len(visible)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}
