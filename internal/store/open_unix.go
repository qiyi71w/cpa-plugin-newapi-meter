//go:build unix

package store

import (
	"os"
	"syscall"
)

// openDataFile opens path for reading without waiting on it: O_NONBLOCK
// makes opening a FIFO that has no writer (or a device) return at once, so
// readFile can refuse it after fstat instead of holding the store mutex
// forever. Regular files ignore O_NONBLOCK, so reading one is unchanged.
func openDataFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
