package tools_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// No tool error may contain the absolute vault root, and a missing note is
// reported as "note not found: <arg>".
func TestToolErrorsNoAbsoluteRoot(t *testing.T) {
	e := newEnv(t)
	e.write("A.md", "x")
	if err := os.MkdirAll(filepath.Join(e.root, "dir.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	type call struct {
		tool string
		args map[string]any
	}
	var missing []call
	for _, p := range []string{"missing.md", "sub/missing", "nodir/x.md"} {
		missing = append(missing,
			call{"get_note", map[string]any{"path": p}},
			call{"get_note_metadata", map[string]any{"path": p}},
			call{"get_frontmatter", map[string]any{"path": p}},
			call{"append_to_note", map[string]any{"path": p, "content": "y"}},
			call{"delete_note", map[string]any{"path": p}},
		)
	}
	for _, c := range missing {
		text, isErr := e.call(c.tool, c.args)
		if !isErr {
			t.Errorf("%s(%v): want error, got %s", c.tool, c.args, text)
			continue
		}
		if strings.Contains(text, e.root) {
			t.Errorf("%s(%v) leaks root: %q", c.tool, c.args, text)
		}
		if !strings.Contains(text, "note not found: ") {
			t.Errorf("%s(%v) = %q, want note not found", c.tool, c.args, text)
		}
	}
	// Errors other than "not found" are vault-relative too.
	for _, c := range []call{
		{"get_note", map[string]any{"path": "dir.md"}},
		{"append_to_note", map[string]any{"path": "dir.md", "content": "y"}},
		{"delete_note", map[string]any{"path": "dir.md"}},
	} {
		text, isErr := e.call(c.tool, c.args)
		if !isErr {
			t.Errorf("%s(%v): want error, got %s", c.tool, c.args, text)
		}
		if strings.Contains(text, e.root) {
			t.Errorf("%s(%v) leaks root: %q", c.tool, c.args, text)
		}
	}
}
