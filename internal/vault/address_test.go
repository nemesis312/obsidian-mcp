package vault

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func addressVault(t *testing.T) *Vault {
	t.Helper()
	v := newTestVault(t)
	writeRaw(t, v, "sub/Deep Note.md", "deep")
	writeRaw(t, v, "v1.2 notes.md", "dots")
	writeRaw(t, v, "caf\u00e9.md", "nfc") // NFC
	writeRaw(t, v, "a/Dup.md", "a")
	writeRaw(t, v, "b/Dup.md", "b")
	writeRaw(t, v, ".trash/Old.md", "old")
	writeRaw(t, v, "img.png", "png")
	return v
}

func TestAddressNote(t *testing.T) {
	v := addressVault(t)
	ok := []struct{ in, want string }{
		{"sub/Deep Note.md", "sub/Deep Note.md"},
		{"sub/Deep Note", "sub/Deep Note.md"},
		{"Deep Note", "sub/Deep Note.md"},
		{"deep note", "sub/Deep Note.md"},
		{"/sub/Deep Note", "sub/Deep Note.md"},
		{"sub\\Deep Note", "sub/Deep Note.md"},
		{"SUB/deep note.md", "sub/Deep Note.md"},
		{"./sub/Deep Note.md", "sub/Deep Note.md"},
		{"v1.2 notes", "v1.2 notes.md"},
		{"cafe\u0301", "caf\u00e9.md"}, // NFD argument, NFC file
		{".trash/Old.md", ".trash/Old.md"},
	}
	for _, c := range ok {
		got, err := v.AddressNote(c.in)
		if err != nil || got != c.want {
			t.Errorf("AddressNote(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}

	bad := []struct{ in, want string }{
		{"Dup", `ambiguous note name "Dup": matches a/Dup.md, b/Dup.md; pass one of these paths`},
		{"Deep Not", "note not found: Deep Not; did you mean: sub/Deep Note.md?"},
		{"img.png", "note not found: img.png"},
		{"../x", "note not found: ../x"},
		{"nope", "note not found: nope"},
	}
	for _, c := range bad {
		_, err := v.AddressNote(c.in)
		if err == nil || err.Error() != c.want {
			t.Errorf("AddressNote(%q) err = %v; want %q", c.in, err, c.want)
		}
	}
	var amb *AmbiguousNoteError
	if _, err := v.AddressNote("Dup"); !errors.As(err, &amb) || strings.Join(amb.Candidates, ",") != "a/Dup.md,b/Dup.md" {
		t.Errorf("AddressNote(Dup) = %v; want *AmbiguousNoteError with both candidates", err)
	}

	if _, err := v.AddressNote(".obsidian/x.md"); err == nil || !strings.Contains(err.Error(), "path_violation") {
		t.Errorf("AddressNote(.obsidian/x.md) err = %v; want path_violation", err)
	}

	abs := []string{filepath.Join(v.Root(), "v1.2 notes.md"), v.Root()}
	if runtime.GOOS == "windows" {
		abs = append(abs, `C:\x.md`, `\\srv\share\x.md`, `//srv/share/x.md`, `C:x.md`, "/"+filepath.ToSlash(v.Root())+"/v1.2 notes.md", `/C:/x.md`)
	}
	for _, in := range abs {
		if _, err := v.AddressNote(in); !errors.Is(err, ErrAbsolutePath) {
			t.Errorf("AddressNote(%q) err = %v; want ErrAbsolutePath", in, err)
		}
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		up := strings.ToUpper(filepath.Join(v.Root(), "v1.2 notes.md"))
		if _, err := v.AddressNote(up); !errors.Is(err, ErrAbsolutePath) {
			t.Errorf("AddressNote(case-folded root) err = %v; want ErrAbsolutePath", err)
		}
	}
	if _, err := v.AddressNote("a\x00.md"); err == nil {
		t.Error("AddressNote with NUL: want error")
	}

	all := append([]string{"Dup", "Deep Not", "img.png", "../x", ".obsidian/x.md", "../../etc/passwd", "a\x00b"}, abs...)
	for _, in := range all {
		_, err := v.AddressNote(in)
		if err != nil && strings.Contains(err.Error(), v.Root()) {
			t.Errorf("AddressNote(%q) leaks root: %q", in, err)
		}
	}
}

// The ambiguity message lists at most 10 candidates.
func TestAmbiguousNoteErrorTruncates(t *testing.T) {
	c := make([]string, 13)
	for i := range c {
		c[i] = fmt.Sprintf("d%02d/X.md", i)
	}
	got := (&AmbiguousNoteError{Arg: "X", Candidates: c}).Error()
	want := `ambiguous note name "X": matches ` + strings.Join(c[:10], ", ") + ", … (3 more); pass one of these paths"
	if got != want {
		t.Errorf("Error() = %q\nwant      %q", got, want)
	}
}

func TestSuggest(t *testing.T) {
	notes := []string{"Projects.md", "archive/Projects.md", "Other.md", "my projects list.md"}
	got := suggest("Projcts", notes, 5)
	if strings.Join(got, ",") != "Projects.md,archive/Projects.md" {
		t.Errorf("suggest(Projcts) = %v", got)
	}
	got = suggest("project", notes, 5)
	if strings.Join(got, ",") != "Projects.md,archive/Projects.md,my projects list.md" {
		t.Errorf("suggest(project) = %v", got)
	}
	if got := suggest("x", []string{"a.md"}, 5); len(got) != 0 {
		t.Errorf("suggest(x) = %v, want empty", got)
	}
	if got := suggest("Projcts", notes, 1); len(got) != 1 {
		t.Errorf("suggest max 1 = %v", got)
	}
}

func TestSuggestBounds(t *testing.T) {
	notes := make([]string, 5000)
	for i := range notes {
		notes[i] = fmt.Sprintf("dir%d/%s note %d.md", i%50, strings.Repeat("ab", 60), i)
	}
	q := strings.Repeat("ab", 5000)
	start := time.Now()
	_ = suggest(q, notes, 5)
	if d := time.Since(start); d > time.Second {
		t.Errorf("suggest took %v, want < 1s", d)
	}
}
