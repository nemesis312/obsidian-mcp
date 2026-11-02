package tools_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var versionRE = regexp.MustCompile(`^sha256:[0-9a-f]{16}$`)

type noteResp struct {
	Success bool   `json:"success"`
	Path    string `json:"path"`
	Version string `json:"version"`
	Content string `json:"content"`
}

func decodeNote(t *testing.T, out string) noteResp {
	t.Helper()
	var r noteResp
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	return r
}

func TestGetNoteVersioned(t *testing.T) {
	e := newEnv(t)
	e.write("n.md", "---\na: 1\n---\nbody é\n")

	out, isErr := e.call("get_note", map[string]any{"path": "n"})
	if isErr {
		t.Fatal(out)
	}
	if strings.Contains(out, "\n") && strings.Contains(strings.SplitN(out, `"content"`, 2)[0], "\n") {
		t.Errorf("not compact: %q", out)
	}
	r := decodeNote(t, out)
	if r.Path != "n.md" || r.Content != e.read("n.md") || !versionRE.MatchString(r.Version) {
		t.Errorf("get_note = %+v", r)
	}
}

func TestPatchNoteWithoutVersionRefused(t *testing.T) {
	e := newEnv(t)
	e.write("n.md", "old")

	out, isErr := e.call("patch_note", map[string]any{"path": "n.md", "content": "new"})
	if !isErr || !strings.Contains(out, "expected_version is required") {
		t.Fatalf("no version: isErr=%v out=%q", isErr, out)
	}
	if e.read("n.md") != "old" {
		t.Error("file changed")
	}

	out, isErr = e.call("patch_note", map[string]any{"path": "n.md", "content": "new", "expected_version": "sha256:0123456789abcdef"})
	if !isErr || !strings.Contains(out, "version conflict:") || !strings.Contains(out, "current version: sha256:") {
		t.Fatalf("stale: isErr=%v out=%q", isErr, out)
	}
	if e.read("n.md") != "old" {
		t.Error("file changed")
	}

	out, isErr = e.call("patch_note", map[string]any{"path": "n.md", "content": "new", "expected_version": e.versionOf("n.md")})
	if isErr {
		t.Fatal(out)
	}
	r := decodeNote(t, out)
	if !r.Success || r.Path != "n.md" || r.Version != e.versionOf("n.md") || e.read("n.md") != "new" {
		t.Errorf("patch = %+v", r)
	}
}

func TestDeleteNoteWithoutVersionRefused(t *testing.T) {
	e := newEnv(t)
	e.write("n.md", "x")

	out, isErr := e.call("delete_note", map[string]any{"path": "n.md"})
	if !isErr || !strings.Contains(out, "expected_version is required") || !e.exists("n.md") {
		t.Fatalf("no version: isErr=%v out=%q", isErr, out)
	}
	out, isErr = e.call("delete_note", map[string]any{"path": "n.md", "expected_version": "sha256:0123456789abcdef"})
	if !isErr || !strings.Contains(out, "version conflict:") || !strings.Contains(out, "current version: sha256:") || !e.exists("n.md") {
		t.Fatalf("stale: isErr=%v out=%q", isErr, out)
	}
	out, isErr = e.call("delete_note", map[string]any{"path": "n.md", "expected_version": e.versionOf("n.md")})
	if isErr {
		t.Fatal(out)
	}
	if out != `{"success":true,"path":"n.md"}` || e.exists("n.md") {
		t.Errorf("delete = %q", out)
	}
	if out, isErr := e.call("delete_note", map[string]any{"path": "n.md", "expected_version": 5}); !isErr || !strings.Contains(out, "expected_version must be a string") {
		t.Errorf("non-string version: %q", out)
	}
}

func TestAppendVersionOptionalTool(t *testing.T) {
	e := newEnv(t)
	e.write("n.md", "a")

	out, isErr := e.call("append_to_note", map[string]any{"path": "n", "content": "b"})
	if isErr {
		t.Fatal(out)
	}
	r := decodeNote(t, out)
	if !r.Success || r.Path != "n.md" || r.Version != e.versionOf("n.md") {
		t.Errorf("append = %+v", r)
	}
	out, isErr = e.call("append_to_note", map[string]any{"path": "n", "content": "c", "expected_version": "sha256:0123456789abcdef"})
	if !isErr || !strings.Contains(out, "version conflict:") || e.read("n.md") != "a\nb" {
		t.Errorf("stale append: isErr=%v out=%q", isErr, out)
	}
	out, isErr = e.call("append_to_note", map[string]any{"path": "n", "content": "c", "expected_version": e.versionOf("n.md")})
	if isErr || e.read("n.md") != "a\nb\nc" {
		t.Errorf("current append: isErr=%v out=%q", isErr, out)
	}
	out, isErr = e.call("update_frontmatter", map[string]any{"path": "n", "updates": map[string]any{"k": "v"}})
	if isErr || !versionRE.MatchString(decodeNote(t, out).Version) {
		t.Errorf("update_frontmatter: isErr=%v out=%q", isErr, out)
	}
	out, isErr = e.call("create_note", map[string]any{"path": "/new/x", "content": "c"})
	r = decodeNote(t, out)
	if isErr || r.Path != "new/x.md" || r.Version != e.versionOf("new/x.md") {
		t.Errorf("create_note: isErr=%v out=%q", isErr, out)
	}
}

func TestNoteToolsByName(t *testing.T) {
	e := newEnv(t)
	e.write("sub/Deep Note.md", "---\ntitle: D\n---\nhello")
	e.write("Other.md", "see [[Deep Note]] and Deep Note")

	for _, tool := range []string{"get_note", "get_note_metadata", "get_frontmatter", "append_to_note", "get_backlinks", "get_outgoing_links", "get_linked_mentions"} {
		args := map[string]any{"path": "Deep Note"}
		if tool == "append_to_note" {
			args["content"] = "more"
		}
		out, isErr := e.call(tool, args)
		if isErr {
			t.Errorf("%s: %s", tool, out)
			continue
		}
		if tool != "get_frontmatter" && !strings.Contains(out, `"path":"sub/Deep Note.md"`) && !strings.Contains(out, `"path": "sub/Deep Note.md"`) {
			t.Errorf("%s did not report sub/Deep Note.md: %s", tool, out)
		}
	}
	out, _ := e.call("get_note_metadata", map[string]any{"path": "Deep Note"})
	var md struct {
		Version   string   `json:"version"`
		Backlinks []string `json:"backlinks"`
	}
	if err := json.Unmarshal([]byte(out), &md); err != nil || !versionRE.MatchString(md.Version) || len(md.Backlinks) != 1 || md.Backlinks[0] != "Other.md" {
		t.Errorf("get_note_metadata = %s (%v)", out, err)
	}
}

func TestNoteToolsAmbiguousAndDidYouMean(t *testing.T) {
	e := newEnv(t)
	e.write("a/Dup.md", "x")
	e.write("b/Dup.md", "x")
	e.write("sub/Deep Note.md", "x")

	prefix := map[string]string{
		"get_note": "failed to read note: ", "get_note_metadata": "failed to get metadata: ",
		"get_frontmatter": "failed to get frontmatter: ", "append_to_note": "failed to append: ",
		"get_backlinks": "", "get_outgoing_links": "", "get_linked_mentions": "failed to get mentions: ",
	}
	for tool, pre := range prefix {
		for arg, want := range map[string]string{
			"Dup":      `ambiguous note name "Dup": matches a/Dup.md, b/Dup.md; pass one of these paths`,
			"Deep Not": "note not found: Deep Not; did you mean: sub/Deep Note.md?",
		} {
			args := map[string]any{"path": arg}
			if tool == "append_to_note" {
				args["content"] = "x"
			}
			out, isErr := e.call(tool, args)
			if !isErr || out != pre+want {
				t.Errorf("%s(%q) = %q (isErr=%v), want %q", tool, arg, out, isErr, pre+want)
			}
		}
	}
}

func TestAbsoluteArgsRefused(t *testing.T) {
	e := newEnv(t)
	e.write("A.md", "x")
	abs := filepath.Join(e.root, "A.md")
	for _, tool := range []string{"get_note", "create_note"} {
		out, isErr := e.call(tool, map[string]any{"path": abs, "content": "x"})
		if !isErr || !strings.Contains(out, "absolute paths are not accepted") || strings.Contains(out, e.root) {
			t.Errorf("%s: isErr=%v out=%q", tool, isErr, out)
		}
	}
}

func TestDescriptionsMentionVersion(t *testing.T) {
	e := newEnv(t)
	for _, name := range []string{"patch_note", "delete_note"} {
		tool := e.s.GetTool(name).Tool
		if !strings.Contains(tool.Description, "expected_version") {
			t.Errorf("%s description lacks expected_version: %q", name, tool.Description)
		}
		found := false
		for _, r := range tool.InputSchema.Required {
			found = found || r == "expected_version"
		}
		if !found {
			t.Errorf("%s: expected_version not required", name)
		}
	}
	for _, name := range []string{"append_to_note", "update_frontmatter"} {
		tool := e.s.GetTool(name).Tool
		if !strings.Contains(tool.Description, "expected_version is optional") {
			t.Errorf("%s description: %q", name, tool.Description)
		}
		for _, r := range tool.InputSchema.Required {
			if r == "expected_version" {
				t.Errorf("%s: expected_version must be optional", name)
			}
		}
	}
}

// Case-only duplicates: the graph tools label the result with the note the
// argument addressed, which is the note the data came from.
func TestGraphToolsLabelAddressedNote(t *testing.T) {
	e := newEnv(t)
	e.write("Notes/A.md", "[[Target]]")
	e.write("notes/a.md", "")
	// A case-insensitive file system folds the two into one file.
	ents, _ := os.ReadDir(e.root)
	n := 0
	for _, d := range ents {
		if strings.EqualFold(d.Name(), "notes") {
			n++
		}
	}
	if n != 2 {
		t.Skip("case-insensitive file system")
	}

	for _, arg := range []string{"Notes/A.md", "notes/a.md"} {
		for _, tool := range []string{"get_outgoing_links", "get_backlinks", "get_linked_mentions"} {
			out, isErr := e.call(tool, map[string]any{"path": arg})
			if isErr {
				t.Fatalf("%s(%s): %s", tool, arg, out)
			}
			if !strings.Contains(out, `"path":"`+arg+`"`) && !strings.Contains(out, `"path": "`+arg+`"`) {
				t.Errorf("%s(%s) labelled another note: %s", tool, arg, out)
			}
		}
	}
	out, _ := e.call("get_outgoing_links", map[string]any{"path": "notes/a.md"})
	if !strings.Contains(out, `"count":0`) {
		t.Errorf("notes/a.md has no links, got %s", out)
	}
	out, _ = e.call("get_outgoing_links", map[string]any{"path": "Notes/A.md"})
	if !strings.Contains(out, `"count":1`) {
		t.Errorf("Notes/A.md has one link, got %s", out)
	}
}
