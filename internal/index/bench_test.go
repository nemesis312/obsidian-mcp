package index_test

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nemesis312/obsidian-mcp/internal/index"
	"github.com/nemesis312/obsidian-mcp/internal/vault"
)

const (
	benchNotes   = 5000
	benchFolders = 50
)

// genVault writes benchNotes notes across benchFolders folders. Each note is
// 0.5-3 KB; when linked is true it holds 3-10 basename wikilinks to other
// notes, otherwise none. The seed is fixed, so every run sees the same vault.
func genVault(tb testing.TB, root string, linked bool) {
	tb.Helper()
	rng := rand.New(rand.NewPCG(1, 2))
	const filler = "Lorem ipsum dolor sit amet, consectetur adipiscing elit. "
	for i := 0; i < benchNotes; i++ {
		dir := filepath.Join(root, fmt.Sprintf("folder%02d", i%benchFolders))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			tb.Fatal(err)
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "# Note %d\n\n", i)
		if linked {
			for n := 3 + rng.IntN(8); n > 0; n-- {
				fmt.Fprintf(&sb, "See [[note%04d]]. ", rng.IntN(benchNotes))
			}
		}
		size := 500 + rng.IntN(2501)
		for sb.Len() < size {
			sb.WriteString(filler)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("note%04d.md", i)), []byte(sb.String()), 0o644); err != nil {
			tb.Fatal(err)
		}
	}
}

func benchSource(tb testing.TB, linked bool) *vault.Vault {
	tb.Helper()
	root := tb.TempDir()
	genVault(tb, root, linked)
	v, err := vault.New(root)
	if err != nil {
		tb.Fatal(err)
	}
	return v
}

func BenchmarkBuild5k(b *testing.B) {
	v := benchSource(b, true)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := index.Build(v); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRefreshNoChange5k(b *testing.B) {
	v := benchSource(b, true)
	ix, err := index.Build(v)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := ix.Refresh(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOrphans5k(b *testing.B) {
	v := benchSource(b, true)
	ix, err := index.Build(v)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ix.Orphans()
	}
}

// TestOrphans5kLinkless guards against a regression to O(N^2): with no links
// at all, every note is an orphan, which was the worst case of the old code.
func TestOrphans5kLinkless(t *testing.T) {
	if testing.Short() {
		t.Skip("generates 5,000 notes")
	}
	v := benchSource(t, false)
	ix, err := index.Build(v)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	orphans := ix.Orphans()
	elapsed := time.Since(start)
	if len(orphans) != benchNotes {
		t.Fatalf("orphans = %d, want %d", len(orphans), benchNotes)
	}
	t.Logf("Orphans over %d linkless notes: %v", benchNotes, elapsed)
	if elapsed > 2*time.Second {
		t.Errorf("Orphans took %v, want under 2s", elapsed)
	}
}
