package links_test

import (
	"testing"

	"github.com/nemesis312/obsidian-mcp/internal/links"
)

func TestGhostKey(t *testing.T) {
	for in, want := range map[string]string{
		"Ghost":            "ghost",
		"Ghost.md":         "ghost",
		"./Ghost":          "ghost",
		" ./ /Ghost.MD ":   "ghost",
		"/Ghost":           "ghost",
		"a//b/../Ghost.md": "a/ghost",
		"missing.png":      "missing.png",
		"../../outside.md": "../../outside",
		"café":             "café",
	} {
		if got := links.GhostKey(in); got != want {
			t.Errorf("GhostKey(%q) = %q, want %q", in, got, want)
		}
	}
}
