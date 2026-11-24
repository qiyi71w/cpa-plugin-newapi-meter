//go:build unix

package customquery

import (
	"context"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A FIFO named by //# sourceMappingURL blocks whoever opens it until a
// writer appears, so a query that returns at once never opened it. This
// covers the run-time compiles (new Function, eval) too, whose error
// positions do not show a map.
func TestSourceMapCommentsNeverOpenFiles(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "map.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	srv := serveJSON(t, nil, `{}`)
	rn, _ := newRunner()
	ref := "file://" + fifo
	for name, script := range map[string]string{
		"top level":    `({ request: { url: "{{baseUrl}}" }, extractor: function () { return { remaining: 1 }; } })` + "\n//# sourceMappingURL=" + ref,
		"new Function": `({ request: { url: "{{baseUrl}}" }, extractor: function () { return { remaining: new Function("return 1\n//# sourceMappingURL=` + ref + `")() }; } })`,
		"eval":         `({ request: { url: "{{baseUrl}}" }, extractor: function () { return { remaining: (0, eval)("1\n//# sourceMappingURL=` + ref + `") }; } })`,
	} {
		done := make(chan Result, 1)
		go func() { done <- rn.Run(context.Background(), account(script, srv.URL)) }()
		select {
		case r := <-done:
			if r.Failure != nil {
				t.Errorf("%s: %+v %s", name, r.Failure, r.Message)
			}
		case <-time.After(3 * time.Second):
			// Unblock the reader so the abandoned query can end.
			if fd, err := syscall.Open(fifo, syscall.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
				_ = syscall.Close(fd)
			}
			t.Errorf("%s: the query opened the FIFO and blocked", name)
		}
	}
}
