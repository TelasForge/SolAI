// Package tool is a registry of in-process tools.
//
// A tool is any value implementing llm.Tool. Unlike the previous design, tools
// are compiled into the binary and called directly: there are no manifests, no
// subprocesses, and no IPC. A session selects which registered tools it gets.
package tool

import (
	"context"
	"sort"
	"sync"

	"github.com/melodyogonna/solai/llm"
)

// Factory builds a tool instance. A tool instance may hold per-session state
// (for example a rate limiter), so the factory is called fresh for each session.
type Factory func() llm.Tool

// Summary describes a registered tool without instantiating it.
type Summary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type entry struct {
	description string
	factory     Factory
}

// ErrorTool is a placeholder returned by a factory that could not construct a
// real tool. It reports the failure when called instead of panicking at startup.
type ErrorTool struct {
	ToolName string
	Err      error
}

func (t *ErrorTool) Name() string        { return t.ToolName }
func (t *ErrorTool) Description() string { return "unavailable: " + t.Err.Error() }
func (t *ErrorTool) Call(context.Context, string) (string, error) {
	return "", t.Err
}

var (
	mu       sync.RWMutex
	registry = map[string]entry{}
)

// Register adds a tool factory under name. It is typically called from a tool
// package's init function.
func Register(name, description string, factory Factory) {
	mu.Lock()
	defer mu.Unlock()
	registry[name] = entry{description: description, factory: factory}
}

// SetUp returns the named tools. An empty names slice returns every registered
// tool. Unknown names are skipped.
func SetUp(names []string) []llm.Tool {
	mu.RLock()
	defer mu.RUnlock()

	if len(names) == 0 {
		out := make([]llm.Tool, 0, len(registry))
		for _, e := range registry {
			out = append(out, e.factory())
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
		return out
	}

	out := make([]llm.Tool, 0, len(names))
	for _, n := range names {
		if e, ok := registry[n]; ok {
			out = append(out, e.factory())
		}
	}
	return out
}

// Summaries returns every registered tool, sorted by name.
func Summaries() []Summary {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Summary, 0, len(registry))
	for name, e := range registry {
		out = append(out, Summary{Name: name, Description: e.description})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
