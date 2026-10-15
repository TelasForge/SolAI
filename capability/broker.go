package capability

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/melodyogonna/solai/ipc"
)

// Broker executes capability requests made by agentic tools. It is the bridge
// between the tool request protocol and the session's selected capabilities:
// tools never touch the runtime directly, they emit a request and the broker
// performs it on their behalf.
type Broker struct {
	manager *CapabilityManager
}

// NewBroker creates a Broker over a session's capability manager.
func NewBroker(manager *CapabilityManager) *Broker {
	return &Broker{manager: manager}
}

// Execute runs the requested capability. Only Regular capabilities are
// requestable by tools; a missing or non-requestable capability is an error.
func (b *Broker) Execute(ctx context.Context, req ipc.Request) (string, error) {
	if b == nil || b.manager == nil {
		return "", fmt.Errorf("no capabilities available in this session")
	}
	if req.Capability == "" {
		return "", fmt.Errorf("capability request is missing a capability name")
	}

	c := b.manager.GetByName(req.Capability)
	if c == nil {
		return "", fmt.Errorf("capability %q is not available in this session", req.Capability)
	}
	if c.Class() != Regular {
		return "", fmt.Errorf("capability %q is not requestable by tools", req.Capability)
	}

	payload, err := json.Marshal(map[string]string{
		"action": req.Action,
		"input":  req.Input,
	})
	if err != nil {
		return "", fmt.Errorf("encoding capability input: %w", err)
	}
	return c.Execute(ctx, string(payload))
}
