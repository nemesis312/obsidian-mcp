package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/nemesis312/obsidian-mcp/internal/tools"
	"github.com/nemesis312/obsidian-mcp/internal/vault"
)

type env struct {
	t    *testing.T
	s    *server.MCPServer
	root string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	v, err := vault.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := server.NewMCPServer("test", "0", server.WithToolCapabilities(false))
	tools.RegisterReadTools(s, v)
	tools.RegisterWriteTools(s, v)
	tools.RegisterGraphTools(s, v)
	tools.RegisterTagTools(s, v)
	return &env{t: t, s: s, root: v.Root()}
}

// call invokes a registered tool handler and returns (text, isError).
func (e *env) call(name string, args map[string]any) (string, bool) {
	e.t.Helper()
	st := e.s.GetTool(name)
	if st == nil {
		e.t.Fatalf("tool %s not registered", name)
	}
	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	res, err := st.Handler(context.Background(), req)
	if err != nil {
		e.t.Fatalf("%s: protocol error: %v", name, err)
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String(), res.IsError
}

func (e *env) write(rel, content string) {
	e.t.Helper()
	p := filepath.Join(e.root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) read(rel string) string {
	e.t.Helper()
	b, err := os.ReadFile(filepath.Join(e.root, rel))
	if err != nil {
		e.t.Fatal(err)
	}
	return string(b)
}

func (e *env) exists(rel string) bool {
	_, err := os.Stat(filepath.Join(e.root, rel))
	return err == nil
}

func TestCreateNote(t *testing.T) {
	e := newEnv(t)

	out, isErr := e.call("create_note", map[string]any{
		"path": "folder/idea", "content": "body\n",
		"frontmatter": map[string]any{"title": "T"},
	})
	if isErr {
		t.Fatalf("unexpected error: %s", out)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["path"] != "folder/idea.md" {
		t.Errorf("path = %v, want folder/idea.md", resp["path"])
	}
	if got := e.read("folder/idea.md"); got != "---\ntitle: T\n---\nbody\n" {
		t.Errorf("content = %q", got)
	}

	// errors
	if out, isErr := e.call("create_note", map[string]any{"path": "folder/idea.md", "content": "x"}); !isErr || !strings.Contains(out, "already exists") {
		t.Errorf("duplicate: isErr=%v out=%q", isErr, out)
	}
	if _, isErr := e.call("create_note", map[string]any{"path": "a.txt", "content": "x"}); !isErr {
		t.Error("non-md extension must fail")
	}
	if e.exists("a.txt") {
		t.Error("a.txt must not exist")
	}
	if _, isErr := e.call("create_note", map[string]any{"path": ".obsidian/x.md", "content": "x"}); !isErr {
		t.Error("protected dir must fail")
	}
	if _, isErr := e.call("create_note", map[string]any{"path": "../x.md", "content": "x"}); !isErr {
		t.Error("traversal must fail")
	}
	if _, isErr := e.call("create_note", map[string]any{"content": "x"}); !isErr {
		t.Error("missing path must fail")
	}
}

func TestAppendToNote(t *testing.T) {
	e := newEnv(t)
	e.write("n.md", "first")

	if out, isErr := e.call("append_to_note", map[string]any{"path": "n", "content": "second"}); isErr {
		t.Fatalf("unexpected error: %s", out)
	}
	if got := e.read("n.md"); got != "first\nsecond" {
		t.Errorf("content = %q", got)
	}
	if _, isErr := e.call("append_to_note", map[string]any{"path": "missing.md", "content": "x"}); !isErr {
		t.Error("missing note must fail")
	}
	if _, isErr := e.call("append_to_note", map[string]any{"path": "n.txt", "content": "x"}); !isErr {
		t.Error("non-md must fail")
	}
}

// versionOf reads rel through get_note and returns its version token.
func (e *env) versionOf(rel string) string {
	e.t.Helper()
	out, isErr := e.call("get_note", map[string]any{"path": rel})
	if isErr {
		e.t.Fatalf("get_note %s: %s", rel, out)
	}
	var r struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		e.t.Fatal(err)
	}
	return r.Version
}

func TestPatchNote(t *testing.T) {
	e := newEnv(t)
	e.write("n.md", "old")

	if out, isErr := e.call("patch_note", map[string]any{"path": "n.md", "content": "new", "expected_version": e.versionOf("n.md")}); isErr {
		t.Fatalf("unexpected error: %s", out)
	}
	if got := e.read("n.md"); got != "new" {
		t.Errorf("content = %q", got)
	}
	if _, isErr := e.call("patch_note", map[string]any{"path": "script.sh", "content": "x", "expected_version": "sha256:0123456789abcdef"}); !isErr {
		t.Error("non-md must fail")
	}
	if e.exists("script.sh") {
		t.Error("script.sh must not be created")
	}
	if _, isErr := e.call("patch_note", map[string]any{"path": "n.md"}); !isErr {
		t.Error("missing content must fail")
	}
}

func TestUpdateFrontmatter(t *testing.T) {
	e := newEnv(t)
	e.write("n.md", "---\nstatus: draft\n---\nBody stays\n")

	if out, isErr := e.call("update_frontmatter", map[string]any{
		"path": "n.md", "updates": map[string]any{"status": "done", "priority": 2},
	}); isErr {
		t.Fatalf("unexpected error: %s", out)
	}
	got := e.read("n.md")
	if !strings.Contains(got, "status: done") || !strings.Contains(got, "priority: 2") || !strings.HasSuffix(got, "---\nBody stays\n") {
		t.Errorf("content = %q", got)
	}
	if _, isErr := e.call("update_frontmatter", map[string]any{"path": "n.md", "updates": "nope"}); !isErr {
		t.Error("non-object updates must fail")
	}
	if _, isErr := e.call("update_frontmatter", map[string]any{"path": "nope.md", "updates": map[string]any{"a": 1}}); !isErr {
		t.Error("missing note must fail")
	}
}

func TestDeleteNote(t *testing.T) {
	e := newEnv(t)
	e.write("n.md", "x")
	e.write("keep.txt", "x")

	if out, isErr := e.call("delete_note", map[string]any{"path": "n.md", "expected_version": e.versionOf("n.md")}); isErr {
		t.Fatalf("unexpected error: %s", out)
	}
	if e.exists("n.md") {
		t.Error("note still exists")
	}
	if _, isErr := e.call("delete_note", map[string]any{"path": "n.md", "expected_version": "sha256:0123456789abcdef"}); !isErr {
		t.Error("deleting missing note must fail")
	}
	if _, isErr := e.call("delete_note", map[string]any{"path": "keep.txt", "expected_version": "sha256:0123456789abcdef"}); !isErr {
		t.Error("non-md delete must fail")
	}
	if !e.exists("keep.txt") {
		t.Error("keep.txt must survive")
	}
}

func TestWriteTools_SymlinkEscape(t *testing.T) {
	e := newEnv(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(e.root, "out")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, isErr := e.call("create_note", map[string]any{"path": "out/pwn.md", "content": "x"}); !isErr {
		t.Error("create through escaping symlink must fail")
	}
	if _, err := os.Stat(filepath.Join(outside, "pwn.md")); err == nil {
		t.Error("file escaped the vault")
	}
}

func TestRenameTag(t *testing.T) {
	e := newEnv(t)
	e.write("a.md", "---\ntags: [old, old/child]\n---\nSee #old #old, and (#old) #older\n```\n#old\n```\n")
	e.write("b.md", "---\ntags: [x]\n---\nnothing")
	e.write("bad.md", "---\ntags: [old\n---\n#old")

	out, isErr := e.call("rename_tag", map[string]any{"old_tag": "old", "new_tag": "#fresh"})
	if isErr {
		t.Fatalf("unexpected error: %s", out)
	}
	var resp struct {
		NotesUpdated int      `json:"notes_updated"`
		Renamed      []string `json:"renamed_files"`
		Failed       []struct {
			Path  string `json:"path"`
			Error string `json:"error"`
		} `json:"failed_files"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if resp.NotesUpdated != 1 || len(resp.Renamed) != 1 || resp.Renamed[0] != "a.md" {
		t.Errorf("renamed = %+v", resp)
	}
	if len(resp.Failed) != 1 || resp.Failed[0].Path != "bad.md" || resp.Failed[0].Error == "" {
		t.Errorf("failed = %+v", resp.Failed)
	}
	got := e.read("a.md")
	for _, want := range []string{"fresh", "fresh/child", "See #fresh #fresh, and (#fresh) #older\n```\n#old\n```\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("a.md missing %q: %q", want, got)
		}
	}

	for _, bad := range []string{"", "#", "has space", "a#b"} {
		if _, isErr := e.call("rename_tag", map[string]any{"old_tag": "fresh", "new_tag": bad}); !isErr {
			t.Errorf("new_tag %q must be rejected", bad)
		}
	}
}

// Windows strips a trailing "." or space, resolves 8.3 short names and
// treats "::$INDEX_ALLOCATION" as the folder itself, so each of these paths
// would land in a protected folder. All are refused, and nothing is created.
func TestCreateNoteWindowsNameForms(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	for _, p := range []string{".obsidian-mcp./x.md", ".git./x.md", ".obsidian ./x.md", ".git::$INDEX_ALLOCATION/x.md", "OBSIDI~1/x.md", "GIT~1/x.md"} {
		e := newEnv(t)
		out, isErr := e.call("create_note", map[string]any{"path": p, "content": "x"})
		if !isErr || !strings.Contains(out, "path_violation") {
			t.Errorf("create_note(%q) = %q, isErr=%v; want path_violation", p, out, isErr)
		}
		entries, err := os.ReadDir(e.root)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Errorf("create_note(%q) created %v", p, entries)
		}
	}
	// A "~<digit>" name that is no protected folder's short name still works.
	e := newEnv(t)
	if out, isErr := e.call("create_note", map[string]any{"path": "draft~2.md", "content": "x"}); isErr {
		t.Errorf("create_note(draft~2.md) = %q, want success", out)
	}
	if !e.exists("draft~2.md") {
		t.Error("draft~2.md was not created")
	}
}
