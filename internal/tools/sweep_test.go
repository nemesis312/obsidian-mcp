package tools_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var sweepTools = []string{
	"append_to_note", "create_note", "delete_note", "get_backlinks", "get_canvas",
	"get_frontmatter", "get_link_graph", "get_linked_mentions", "get_note",
	"get_note_metadata", "get_notes_by_tag", "get_orphaned_notes", "get_outgoing_links",
	"list_all_tags", "list_canvases", "list_vault_files", "patch_note", "rename_tag",
	"search_vault", "update_frontmatter",
}

// No tool result, success or error, may contain an absolute vault path in any
// spelling, whatever hostile value any string argument receives.
func TestNoToolOutputContainsAbsolutePath(t *testing.T) {
	probe := newEnv(t)
	var got []string
	for name := range probe.s.ListTools() {
		got = append(got, name)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(sweepTools, ",") {
		t.Fatalf("tool set changed: update the sweep.\n got %v\nwant %v", got, sweepTools)
	}

	for _, name := range sweepTools {
		schema := probe.s.GetTool(name).Tool.InputSchema
		var props []string
		for p, def := range schema.Properties {
			if m, ok := def.(map[string]any); ok && m["type"] == "string" {
				props = append(props, p)
			}
		}
		sort.Strings(props)
		for _, prop := range props {
			for _, hostile := range hostileValues(t) {
				e := newEnv(t)
				outside := filepath.Join(t.TempDir(), "outside.md")
				seedSweepVault(t, e, outside)
				args := map[string]any{}
				for _, req := range schema.Required {
					def, _ := schema.Properties[req].(map[string]any)
					switch {
					case def["type"] == "object":
						args[req] = map[string]any{"k": "v"}
					case strings.Contains(req, "version"):
						args[req] = "sha256:0000000000000000"
					default:
						args[req] = "x"
					}
				}
				if _, ok := schema.Properties["expected_version"]; ok {
					args["expected_version"] = "sha256:0000000000000000"
				}
				val := hostile(e.root)
				args[prop] = val
				text, _ := e.call(name, args)
				for _, bad := range forbidden(e.root) {
					if strings.Contains(text, bad) {
						t.Errorf("%s(%s=%q) leaks %q: %s", name, prop, val, bad, text)
						break
					}
				}
			}
		}
	}
}

func forbidden(root string) []string {
	out := []string{root, filepath.ToSlash(root), filepath.Dir(root)}
	if raw := rawSpelling(root); raw != root {
		out = append(out, raw, filepath.ToSlash(raw), filepath.Dir(raw))
	}
	return out
}

// rawSpelling returns the pre-EvalSymlinks spelling (macOS /private/var -> /var).
func rawSpelling(root string) string {
	if strings.HasPrefix(root, "/private/var/") {
		return strings.TrimPrefix(root, "/private")
	}
	return root
}

func hostileValues(t *testing.T) []func(root string) string {
	t.Helper()
	return []func(string) string{
		func(r string) string { return filepath.Join(r, "A.md") },
		func(r string) string { return filepath.Join(r, "missing.md") },
		func(r string) string { return filepath.Join(rawSpelling(r), "A.md") },
		func(string) string { return "/etc/passwd" },
		func(string) string { return `C:\Windows\win.ini` },
		func(string) string { return `\\srv\share\x.md` },
		func(string) string { return "../outside.md" },
		func(string) string { return "missing.md" },
		func(string) string { return "dir.md" },
		func(string) string { return ".obsidian/x.md" },
		func(string) string { return "out.md" },
		func(string) string { return "Dup" },
		func(string) string { return "a\x00.md" },
	}
}

func seedSweepVault(t *testing.T, e *env, outside string) {
	t.Helper()
	e.write("A.md", "# A\n#tag [[Dup]]\n")
	e.write("a/Dup.md", "a")
	e.write("b/Dup.md", "b")
	e.write("x.canvas", `{"nodes":[],"edges":[]}`)
	if err := os.MkdirAll(filepath.Join(e.root, "dir.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(e.root, "out.md")); err != nil {
		t.Logf("symlinks unavailable, skipping out.md: %v", err)
	}
}
