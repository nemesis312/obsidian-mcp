//go:build unix

package vault

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// within runs fn and fails the test if it does not return within d.
func within(t *testing.T, d time.Duration, name string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s hung for %v", name, d)
	}
}

// A FIFO named *.md, directly or behind a symlink, must neither hang a
// Build/Refresh nor be read as a note.
func TestFIFONoteDoesNotHang(t *testing.T) {
	v := newTestVault(t)
	writeRaw(t, v, "A.md", "[[B]]")
	writeRaw(t, v, "B.md", "")
	fifo := filepath.Join(v.Root(), "pipe.md")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	// Release any reader stuck in open(2) so no goroutine outlives the test.
	t.Cleanup(func() {
		if f, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			f.Close()
		}
	})
	mustSymlink(t, fifo, filepath.Join(v.Root(), "via.md"))

	notes, err := v.StatNotes()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range notes {
		if n.Path == "pipe.md" {
			t.Errorf("StatNotes lists the FIFO: %+v", notes)
		}
	}
	for _, p := range []string{"pipe.md", "via.md"} {
		within(t, 5*time.Second, "ReadNote("+p+")", func() {
			if _, err := v.ReadNote(p); err == nil {
				t.Errorf("ReadNote(%s): want an error for a non-regular file", p)
			}
		})
	}
	within(t, 5*time.Second, "GetOrphanedNotes", func() {
		if _, err := v.GetOrphanedNotes(); err != nil {
			t.Errorf("GetOrphanedNotes: %v", err)
		}
	})
}

// A FIFO named *.canvas is refused quickly instead of blocking get_canvas.
func TestFIFOCanvasDoesNotHang(t *testing.T) {
	v := newTestVault(t)
	fifo := filepath.Join(v.Root(), "x.canvas")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	t.Cleanup(func() {
		if f, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			f.Close()
		}
	})
	within(t, 2*time.Second, "ReadCanvas(x.canvas)", func() {
		if _, err := v.ReadCanvas("x.canvas"); err == nil {
			t.Error("ReadCanvas of a FIFO: want an error")
		}
	})
}
