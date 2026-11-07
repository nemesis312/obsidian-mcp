package tools_test

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var sweepTools = []string{
	"append_to_note", "create_note", "delete_note", "edit_section", "get_backlinks", "get_canvas",
	"get_frontmatter", "get_link_graph", "get_linked_mentions", "get_note",
	"get_note_metadata", "get_notes_by_tag", "get_orphaned_notes", "get_outgoing_links",
	"list_all_tags", "list_canvases", "list_vault_files", "move_note", "patch_note", "rename_tag",
	"search_vault", "update_frontmatter",
}

// No tool result, success or error, may contain an absolute vault path in any
// spelling, or the content of a file outside the vault, whatever hostile
// value any string argument receives. Every tool is also called with no
// arguments, so tools that take none are swept too. Each call runs in its own
// subtest, named after the tool, the argument and the hostile value's index,
// and a leak report prints the full arguments.
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

	calls := map[string]int{}
	okResults := 0 // non-error results with non-empty text

	// sweep calls name in a fresh seeded vault with the arguments args
	// builds from that vault's root, and reports whether the result was a
	// non-error with non-empty text.
	// echoed, when not "", names the argument whose own value the tool may
	// quote back (a section heading it did not find): that value is taken
	// out of the result before the leak check.
	sweep := func(t *testing.T, name, echoed string, args func(root string) map[string]any) (string, bool) {
		t.Helper()
		e := newEnv(t)
		seedSweepVault(t, e, filepath.Join(t.TempDir(), "outside.md"))
		a := args(e.root)
		text, isErr := e.call(name, a)
		calls[name]++
		ok := !isErr && text != ""
		if ok {
			okResults++
		}
		checked := text
		if v, isStr := a[echoed].(string); echoed != "" && isStr && v != "" {
			checked = strings.ReplaceAll(checked, fmt.Sprintf("%q", v), "")
			checked = strings.ReplaceAll(checked, v, "")
		}
		for _, bad := range forbidden(e.root) {
			if strings.Contains(checked, bad) {
				t.Errorf("%s(%q) leaks %q: %s", name, a, bad, text)
				break
			}
		}
		return text, ok
	}

	for _, name := range sweepTools {
		schema := probe.s.GetTool(name).Tool.InputSchema

		t.Run(name+"/no-args", func(t *testing.T) {
			_, ok := sweep(t, name, "", func(string) map[string]any { return map[string]any{} })
			if len(schema.Required) == 0 && !ok {
				t.Errorf("%s() takes no required argument but gave an error or an empty result", name)
			}
		})

		var props []string
		for p, def := range schema.Properties {
			if m, ok := def.(map[string]any); ok && m["type"] == "string" {
				props = append(props, p)
			}
		}
		sort.Strings(props)
		for _, prop := range props {
			for i, hostile := range hostileValues(t) {
				t.Run(fmt.Sprintf("%s/%s/%d", name, prop, i), func(t *testing.T) {
					defaults, deep := sweepDefaults[name]
					echoed := ""
					if name == "edit_section" && prop == "heading" {
						echoed = prop
					}
					text, _ := sweep(t, name, echoed, func(root string) map[string]any {
						args := map[string]any{}
						if deep {
							// Valid defaults, so the hostile value reaches
							// the code that uses prop.
							for k, v := range defaults {
								args[k] = v
							}
							args[prop] = hostile(root)
							return args
						}
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
						args[prop] = hostile(root)
						return args
					})
					if deep && prop != "path" && prop != "mode" {
						for _, generic := range []string{"note not found", "invalid mode"} {
							if strings.Contains(text, generic) {
								t.Errorf("%s with a hostile %s stopped at %q before reaching the code under test: %s", name, prop, generic, text)
							}
						}
					}
				})
			}
		}
	}

	if f := flag.Lookup("test.run"); f != nil && strings.Contains(f.Value.String(), "/") {
		return // subtests filtered: the counts are partial
	}
	for _, name := range sweepTools {
		if calls[name] == 0 {
			t.Errorf("%s was never called", name)
		}
	}
	if okResults == 0 {
		t.Error("no call returned a non-error, non-empty result: the sweep proves nothing")
	}
}

// sweepDefaults are valid arguments for the tools whose placeholder "x"
// arguments would stop every call early (a missing note, an invalid mode),
// so a hostile value in another argument would never be exercised. They
// carry no expected_version, which would stop at a version conflict.
var sweepDefaults = map[string]map[string]any{
	"edit_section": {"path": "A", "heading": "A", "mode": "append", "content": "x"},
	"move_note":    {"path": "A", "new_path": "moved", "dry_run": true},
}

// sweepSecret is the content of the file outside the vault that out.md
// links to; no tool may ever return it.
const sweepSecret = "secret"

func forbidden(root string) []string {
	out := []string{root, filepath.ToSlash(root), filepath.Dir(root), sweepSecret}
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
		func(string) string { return ".obsidian-mcp/write.lock" },
		func(string) string { return ".OBSIDIAN-MCP/x.md" },
		func(string) string { return ".obsidian-mcp./x.md" },
		func(string) string { return "/C:/x.md" },
		func(string) string { return "OBSIDI~1/x" },
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
	if err := os.WriteFile(outside, []byte(sweepSecret), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(e.root, "out.md")); err != nil {
		t.Logf("symlinks unavailable, skipping out.md: %v", err)
	}
}
