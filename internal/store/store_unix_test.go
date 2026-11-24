//go:build unix

package store

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A FIFO with no writer at the data file's path (or a symlink to one) is
// refused promptly instead of blocking the store, its mode is left alone, and
// the store keeps working once a regular file is back.
func TestFIFODataFileIsRefusedWithoutBlocking(t *testing.T) {
	cases := []struct {
		name string
		// place puts a FIFO (or a link to one) at path and returns the FIFO.
		place func(t *testing.T, path string) string
	}{
		{"fifo", func(t *testing.T, path string) string {
			if err := syscall.Mkfifo(path, 0o644); err != nil {
				t.Fatal(err)
			}
			return path
		}},
		{"symlink to a fifo", func(t *testing.T, path string) string {
			target := filepath.Join(t.TempDir(), "pipe")
			if err := syscall.Mkfifo(target, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
			return target
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := openTemp(t)
			fifo := tc.place(t, s.Path())
			if err := os.Chmod(fifo, 0o644); err != nil {
				t.Fatal(err)
			}
			var err error
			within(t, fifo, func() { _, err = s.List() })
			var fe *FileError
			if !errors.As(err, &fe) || fe.Kind != FileUnavailable || fe.Path != s.Path() {
				t.Fatalf("list: err = %v, want %s", err, FileUnavailable)
			}
			within(t, fifo, func() { _, err = s.Create(patInput("x")) })
			if !errors.As(err, &fe) || fe.Kind != FileUnavailable {
				t.Fatalf("create: err = %v, want %s", err, FileUnavailable)
			}
			info, err := os.Stat(fifo)
			if err != nil || info.Mode().Type() != os.ModeNamedPipe || info.Mode().Perm() != 0o644 {
				t.Fatalf("fifo after refusal: %v %v, want a 0644 named pipe", info, err)
			}

			if err := os.Remove(s.Path()); err != nil {
				t.Fatal(err)
			}
			within(t, "", func() { _, err = s.Create(patInput("after")) })
			if err != nil {
				t.Fatal(err)
			}
			if list, err := s.List(); err != nil || len(list) != 1 {
				t.Fatalf("list = %+v %v", list, err)
			}
		})
	}
}

// within fails the test when op does not return in time. A call stuck
// opening fifo is released by briefly opening its write end, so the store
// mutex it holds does not outlive the test.
func within(t *testing.T, fifo string, op func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		op()
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		if fifo != "" {
			if w, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
				_ = w.Close()
			}
			<-done
		}
		t.Fatal("store operation still blocked after 2s")
	}
}
