package tool

import "github.com/melodyogonna/solai/llm"

// Select returns the tools whose names appear in names, preserving their
// original order. An empty names slice returns all tools.
func Select(all []llm.Tool, names []string) []llm.Tool {
	if len(names) == 0 {
		return all
	}
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	out := make([]llm.Tool, 0, len(all))
	for _, t := range all {
		if want[t.Name()] {
			out = append(out, t)
		}
	}
	return out
}
