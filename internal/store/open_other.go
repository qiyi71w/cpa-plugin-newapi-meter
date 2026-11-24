//go:build !unix

package store

import "os"

// openDataFile opens path for reading. Windows has no FIFOs at a file path
// that an open would wait on.
func openDataFile(path string) (*os.File, error) {
	return os.Open(path)
}
