package tool

import (
	"context"
	"testing"

	"github.com/melodyogonna/solai/llm"
)

type selectTool struct{ name string }

func (t *selectTool) Name() string                                 { return t.name }
func (t *selectTool) Description() string                          { return t.name }
func (t *selectTool) Call(context.Context, string) (string, error) { return "", nil }

func TestSelect(t *testing.T) {
	all := []llm.Tool{&selectTool{name: "a"}, &selectTool{name: "b"}, &selectTool{name: "c"}}

	if got := Select(all, nil); len(got) != 3 {
		t.Errorf("empty selection should return all, got %d", len(got))
	}

	got := Select(all, []string{"c", "b"})
	if len(got) != 2 || got[0].Name() != "b" || got[1].Name() != "c" {
		t.Errorf("unexpected selection: %v", names(got))
	}

	if got := Select(all, []string{"missing"}); len(got) != 0 {
		t.Errorf("expected no matches, got %v", names(got))
	}
}

func names(tools []llm.Tool) []string {
	out := make([]string, len(tools))
	for i, t := range tools {
		out[i] = t.Name()
	}
	return out
}
