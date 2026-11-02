package tools_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/server"

	"github.com/nemesis312/obsidian-mcp/internal/tools"
	"github.com/nemesis312/obsidian-mcp/internal/vault"
)

// Helper protocol for TestTwoProcessRace. The test binary re-executes itself
// with -test.run=^TestRaceHelper$; the environment selects what the helper
// does. The helper registers the real tool handlers on the vault, prints
// raceReady, waits for "go" on stdin (the start barrier), then prints one
// raceResult JSON line per operation.
const (
	raceRoleEnv   = "OBSIDIAN_MCP_RACE_ROLE"
	raceVaultEnv  = "OBSIDIAN_MCP_RACE_VAULT"
	raceOpEnv     = "OBSIDIAN_MCP_RACE_OP"
	raceIDEnv     = "OBSIDIAN_MCP_RACE_ID"
	raceNEnv      = "OBSIDIAN_MCP_RACE_N"
	raceTokensEnv = "OBSIDIAN_MCP_RACE_TOKENS"

	raceReady   = "READY"
	raceTimeout = 60 * time.Second
)

type raceResult struct {
	I    int    `json:"i"`
	OK   bool   `json:"ok"`
	Text string `json:"text"`
}

// newEnvAt is newEnv on an existing vault directory, so several processes
// can share one vault.
func newEnvAt(t *testing.T, dir string) *env {
	t.Helper()
	v, err := vault.New(dir)
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

// TestRaceHelper is not a test on its own: it is the body of a helper
// process started by TestTwoProcessRace.
func TestRaceHelper(t *testing.T) {
	if os.Getenv(raceRoleEnv) == "" {
		t.Skip("helper process for TestTwoProcessRace")
	}
	op, id := os.Getenv(raceOpEnv), os.Getenv(raceIDEnv)
	n, err := strconv.Atoi(os.Getenv(raceNEnv))
	if err != nil {
		t.Fatalf("bad %s: %v", raceNEnv, err)
	}
	var tokens []string
	if p := os.Getenv(raceTokensEnv); p != "" {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &tokens); err != nil {
			t.Fatal(err)
		}
	}
	token := func(i int) string {
		if i >= len(tokens) {
			t.Fatalf("no token for %d", i)
		}
		return tokens[i]
	}

	e := newEnvAt(t, os.Getenv(raceVaultEnv))

	// Warm up lazy per-process state (first-call setup, index build) so both
	// helpers race at full speed from the barrier. Results are ignored.
	e.call("list_vault_files", map[string]any{})
	e.call("get_note", map[string]any{"path": "race-warmup"})

	fmt.Println(raceReady)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil || line != "go\n" {
		t.Fatalf("start barrier: got %q, err %v", line, err)
	}

	enc := json.NewEncoder(os.Stdout)
	for k := 0; k < n; k++ {
		// "delete" walks the notes backwards while "append1" walks them
		// forwards, so the delete subtest sees both orders (append first at
		// low i, delete first at high i) and a real race where they cross.
		i := k
		if op == "delete" {
			i = n - 1 - k
		}
		var name string
		var args map[string]any
		switch op {
		case "create":
			name, args = "create_note", map[string]any{"path": fmt.Sprintf("race/c%d.md", i), "content": "from " + id}
		case "append":
			name, args = "append_to_note", map[string]any{"path": "race/log.md", "content": fmt.Sprintf("%s-%d", id, i)}
		case "append1":
			name, args = "append_to_note", map[string]any{"path": fmt.Sprintf("race/d%d.md", i), "content": "A"}
		case "delete", "deleteSame":
			name, args = "delete_note", map[string]any{"path": fmt.Sprintf("race/d%d.md", i), "expected_version": token(i)}
		default:
			t.Fatalf("unknown op %q", op)
		}
		text, isErr := e.call(name, args)
		if err := enc.Encode(raceResult{I: i, OK: !isErr, Text: text}); err != nil {
			t.Fatal(err)
		}
	}
}

// raceSpec describes one helper process.
type raceSpec struct {
	op, id, tokens string
}

// raceHelper is a running helper process.
type raceHelper struct {
	id     string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  chan string
	stderr *bytes.Buffer
	waited bool
}

// output returns what the helper wrote to stderr, for failure messages.
// Call it only after the process has been waited for.
func (h *raceHelper) output() string { return h.stderr.String() }

// runRace runs two helpers against dir with n operations each, released
// together by the start barrier, and returns their results indexed by i.
// Helpers are killed when the test ends and after raceTimeout at most.
func runRace(t *testing.T, dir string, n int, specs ...raceSpec) map[string][]raceResult {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), raceTimeout)
	t.Cleanup(cancel)

	helpers := make([]*raceHelper, 0, len(specs))
	for _, sp := range specs {
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestRaceHelper$", "-test.count=1")
		cmd.Env = append(os.Environ(),
			raceRoleEnv+"=helper",
			raceVaultEnv+"="+dir,
			raceOpEnv+"="+sp.op,
			raceIDEnv+"="+sp.id,
			raceNEnv+"="+strconv.Itoa(n),
			raceTokensEnv+"="+sp.tokens,
		)
		cmd.WaitDelay = 5 * time.Second
		h := &raceHelper{id: sp.id, cmd: cmd, lines: make(chan string, 4*n+16), stderr: &bytes.Buffer{}}
		cmd.Stderr = h.stderr
		if h.stdin, err = cmd.StdinPipe(); err != nil {
			t.Fatal(err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if !h.waited {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		})
		go func() {
			defer close(h.lines)
			sc := bufio.NewScanner(stdout)
			sc.Buffer(make([]byte, 64*1024), 1<<20)
			for sc.Scan() {
				h.lines <- sc.Text()
			}
		}()
		helpers = append(helpers, h)
	}

	// fail kills every helper, waits for them, and reports their stderr.
	fail := func(format string, args ...any) {
		t.Helper()
		var sb strings.Builder
		for _, h := range helpers {
			if !h.waited {
				_ = h.cmd.Process.Kill()
				_ = h.cmd.Wait()
				h.waited = true
			}
			fmt.Fprintf(&sb, "\n--- helper %s stderr:\n%s", h.id, h.output())
		}
		t.Fatalf(format+"%s", append(args, sb.String())...)
	}

	// Start barrier: release both helpers only once both are ready.
	for _, h := range helpers {
		select {
		case l, ok := <-h.lines:
			if !ok || l != raceReady {
				fail("helper %s did not become ready (got %q)", h.id, l)
			}
		case <-ctx.Done():
			fail("helper %s not ready within %v", h.id, raceTimeout)
		}
	}
	for _, h := range helpers {
		if _, err := io.WriteString(h.stdin, "go\n"); err != nil {
			fail("release helper %s: %v", h.id, err)
		}
		_ = h.stdin.Close()
	}

	results := make(map[string][]raceResult, len(helpers))
	for _, h := range helpers {
		res := make([]raceResult, n)
		seen := make([]bool, n)
	collect:
		for {
			select {
			case l, ok := <-h.lines:
				if !ok {
					break collect
				}
				if !strings.HasPrefix(l, "{") {
					continue // go test's own output, e.g. "PASS"
				}
				var r raceResult
				if err := json.Unmarshal([]byte(l), &r); err != nil {
					fail("helper %s: bad line %q: %v", h.id, l, err)
				}
				if r.I < 0 || r.I >= n || seen[r.I] {
					fail("helper %s: unexpected result index %d", h.id, r.I)
				}
				res[r.I], seen[r.I] = r, true
			case <-ctx.Done():
				fail("helper %s did not finish within %v", h.id, raceTimeout)
			}
		}
		err := h.cmd.Wait()
		h.waited = true
		if err != nil {
			fail("helper %s failed: %v", h.id, err)
		}
		for i, ok := range seen {
			if !ok {
				fail("helper %s: no result for %d", h.id, i)
			}
		}
		results[h.id] = res
	}
	return results
}

func countOK(rs []raceResult) int {
	n := 0
	for _, r := range rs {
		if r.OK {
			n++
		}
	}
	return n
}

// writeTokens stores the delete tokens outside the vault and returns the file.
func writeTokens(t *testing.T, tokens []string) string {
	t.Helper()
	b, err := json.Marshal(tokens)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "tokens.json")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// seedDeleteNotes creates race/d<i>.md and returns each note's version as
// reported by get_note.
func seedDeleteNotes(t *testing.T, e *env, n int) []string {
	t.Helper()
	tokens := make([]string, n)
	for i := range tokens {
		rel := fmt.Sprintf("race/d%d.md", i)
		e.write(rel, fmt.Sprintf("note %d\n", i))
		out, isErr := e.call("get_note", map[string]any{"path": rel})
		if isErr {
			t.Fatalf("get_note %s: %s", rel, out)
		}
		var got struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil || got.Version == "" {
			t.Fatalf("get_note %s: no version in %q (%v)", rel, out, err)
		}
		tokens[i] = got.Version
	}
	return tokens
}

// TestTwoProcessRace runs two real server processes on one vault and checks
// that the cross-process write guard keeps creates, appends and deletes
// consistent: no lost update, no double success.
func TestTwoProcessRace(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		const n = 100
		dir := t.TempDir()
		res := runRace(t, dir, n, raceSpec{op: "create", id: "A"}, raceSpec{op: "create", id: "B"})
		e := newEnvAt(t, dir)
		t.Logf("create wins: A %d, B %d", countOK(res["A"]), countOK(res["B"]))
		for i := 0; i < n; i++ {
			a, b := res["A"][i], res["B"][i]
			if a.OK == b.OK {
				t.Errorf("c%d: A ok=%v (%s), B ok=%v (%s); want exactly one ok", i, a.OK, a.Text, b.OK, b.Text)
				continue
			}
			winner, loser := "A", b
			if b.OK {
				winner, loser = "B", a
			}
			if got := e.read(fmt.Sprintf("race/c%d.md", i)); got != "from "+winner {
				t.Errorf("c%d: content %q, want %q", i, got, "from "+winner)
			}
			if !strings.Contains(loser.Text, "already exists") {
				t.Errorf("c%d: loser text %q, want it to contain \"already exists\"", i, loser.Text)
			}
		}
	})

	t.Run("append", func(t *testing.T) {
		const n = 100
		dir := t.TempDir()
		e := newEnvAt(t, dir)
		e.write("race/log.md", "# log\n")
		res := runRace(t, dir, n, raceSpec{op: "append", id: "A"}, raceSpec{op: "append", id: "B"})
		for _, id := range []string{"A", "B"} {
			for i, r := range res[id] {
				if !r.OK {
					t.Errorf("%s-%d: append failed: %s", id, i, r.Text)
				}
			}
		}
		count := map[string]int{}
		for _, l := range strings.Split(e.read("race/log.md"), "\n") {
			count[l]++
		}
		lost := 0
		for _, id := range []string{"A", "B"} {
			for i := 0; i < n; i++ {
				l := fmt.Sprintf("%s-%d", id, i)
				if c := count[l]; c != 1 {
					lost++
					if lost <= 10 {
						t.Errorf("line %s appears %d times, want 1", l, c)
					}
				}
			}
		}
		if lost > 0 {
			t.Errorf("%d of %d appended lines are lost or duplicated", lost, 2*n)
		}
	})

	t.Run("delete", func(t *testing.T) {
		const n = 50
		dir := t.TempDir()
		e := newEnvAt(t, dir)
		tokens := writeTokens(t, seedDeleteNotes(t, e, n))
		res := runRace(t, dir, n, raceSpec{op: "append1", id: "A"}, raceSpec{op: "delete", id: "B", tokens: tokens})
		t.Logf("wins: append %d, delete %d", countOK(res["A"]), countOK(res["B"]))
		for i := 0; i < n; i++ {
			a, b := res["A"][i], res["B"][i]
			rel := fmt.Sprintf("race/d%d.md", i)
			if a.OK == b.OK {
				t.Errorf("d%d: append ok=%v (%s), delete ok=%v (%s); want exactly one ok", i, a.OK, a.Text, b.OK, b.Text)
				continue
			}
			if exists := e.exists(rel); exists != a.OK {
				t.Errorf("d%d: file exists=%v, append ok=%v; want exists iff append ok", i, exists, a.OK)
				continue
			}
			if a.OK {
				if got := e.read(rel); !strings.HasSuffix(got, "A") {
					t.Errorf("d%d: content %q, want it to end with A", i, got)
				}
			}
		}
	})

	t.Run("deleteSame", func(t *testing.T) {
		const n = 50
		dir := t.TempDir()
		e := newEnvAt(t, dir)
		tokens := writeTokens(t, seedDeleteNotes(t, e, n))
		res := runRace(t, dir, n,
			raceSpec{op: "deleteSame", id: "A", tokens: tokens},
			raceSpec{op: "deleteSame", id: "B", tokens: tokens})
		t.Logf("delete wins: A %d, B %d", countOK(res["A"]), countOK(res["B"]))
		for i := 0; i < n; i++ {
			a, b := res["A"][i], res["B"][i]
			if a.OK == b.OK {
				t.Errorf("d%d: A ok=%v (%s), B ok=%v (%s); want exactly one ok", i, a.OK, a.Text, b.OK, b.Text)
				continue
			}
			loser := b
			if b.OK {
				loser = a
			}
			if !strings.Contains(loser.Text, "note not found") {
				t.Errorf("d%d: loser text %q, want it to contain \"note not found\"", i, loser.Text)
			}
			if e.exists(fmt.Sprintf("race/d%d.md", i)) {
				t.Errorf("d%d: file still exists", i)
			}
		}
	})
}
