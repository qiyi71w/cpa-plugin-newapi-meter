// Package store owns the plugin data file: one JSON document holding every
// account and its credentials (ADR 0002). It is the only reader and writer.
package store

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
)

// DirName is the default data directory name under the user config directory.
const DirName = "cpa-plugin-newapi-meter"

// FileName is the data file inside the data directory.
const FileName = "accounts.json"

// DefaultDir returns <user config dir>/cpa-plugin-newapi-meter for the user
// running CPA (%APPDATA% on Windows, ~/.config on Linux).
func DefaultDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, DirName), nil
}

// FileErrorKind tells why the data file is refused.
type FileErrorKind string

const (
	// FileCorrupt: the file exists but is not a valid data file.
	FileCorrupt FileErrorKind = "data_file_corrupt"
	// FileNewer: the file was written by a newer plugin (higher schema_version).
	FileNewer FileErrorKind = "data_file_newer"
	// FileUnavailable: the directory or file cannot be read or written.
	FileUnavailable FileErrorKind = "data_file_unavailable"
)

// Errors a FileUnavailable error wraps when the data directory or file is
// open to other users. Their text is the reason reported to the page.
var (
	// ErrDirNotPrivate: a configured data directory grants group or other
	// users access; the plugin refuses it instead of changing its mode.
	ErrDirNotPrivate = errors.New("dir_not_private")
	// ErrFileNotPrivate: the data file grants group or other users access and
	// could not be restricted to its owner.
	ErrFileNotPrivate = errors.New("file_not_private")
)

// FileError reports a data file the plugin will not overwrite or cannot use.
type FileError struct {
	Kind FileErrorKind
	Path string
	Err  error
}

func (e *FileError) Error() string { return fmt.Sprintf("%s: %s: %v", e.Kind, e.Path, e.Err) }
func (e *FileError) Unwrap() error { return e.Err }

// Reason names a cause the user can fix ("dir_not_private",
// "file_not_private"), or is empty.
func (e *FileError) Reason() string {
	for _, r := range []error{ErrDirNotPrivate, ErrFileNotPrivate} {
		if errors.Is(e.Err, r) {
			return r.Error()
		}
	}
	return ""
}

type document struct {
	SchemaVersion int       `json:"schema_version"`
	Accounts      []Account `json:"accounts"`
}

// Store reads the data file on every operation, so a file restored from a
// backup or repaired by hand takes effect without restarting CPA, and a file
// that cannot be understood is never replaced.
type Store struct {
	mu   sync.Mutex
	dir  string
	path string
	// owned: the directory belongs to the plugin alone (the default one), so
	// the plugin may restrict it; a configured directory is only checked.
	owned bool
}

// Open prepares dir (created 0700 when missing) and returns a store for its
// data file. On Unix an existing dir must already be owner-only: every
// operation refuses one that grants group or other access, and restricts a
// data file that does to 0600 before reading it.
func Open(dir string) (*Store, error) {
	if !filepath.IsAbs(dir) {
		return nil, &FileError{FileUnavailable, dir, errors.New("data directory must be an absolute path")}
	}
	dir = filepath.Clean(dir)
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, &FileError{FileUnavailable, dir, err}
		}
		// MkdirAll is subject to umask; pin the leaf to owner-only.
		if err := chmodOwnerOnly(dir, 0o700); err != nil {
			return nil, &FileError{FileUnavailable, dir, err}
		}
	case err != nil:
		return nil, &FileError{FileUnavailable, dir, err}
	case !info.IsDir():
		return nil, &FileError{FileUnavailable, dir, errors.New("not a directory")}
	}
	return &Store{dir: dir, path: filepath.Join(dir, FileName)}, nil
}

// OpenDefault opens DefaultDir and sets it to exactly 0700 right away, and
// every operation keeps it so, even when it already existed or was changed
// later, because that directory belongs to the plugin alone.
func OpenDefault() (*Store, error) {
	dir, err := DefaultDir()
	if err != nil {
		return nil, &FileError{FileUnavailable, DirName, err}
	}
	s, err := Open(dir)
	if err != nil {
		return nil, err
	}
	s.owned = true
	if err := s.checkDir(); err != nil {
		return nil, err
	}
	return s, nil
}

func chmodOwnerOnly(path string, mode fs.FileMode) error {
	if runtime.GOOS == "windows" {
		// %APPDATA% is already per-user; Windows ignores Unix mode bits.
		return nil
	}
	return os.Chmod(path, mode)
}

// groupOrOther are the permission bits that let anyone but the owner in.
const groupOrOther fs.FileMode = 0o077

// checkDir keeps the plugin's own default directory at exactly 0700 (a
// loosened one and one that lost owner bits alike) and refuses a configured
// directory open to group or other users without changing it. Windows has
// no mode bits.
func (s *Store) checkDir() error {
	if runtime.GOOS == "windows" {
		return nil
	}
	info, err := os.Stat(s.dir)
	if err != nil {
		return &FileError{FileUnavailable, s.dir, err}
	}
	mode := info.Mode().Perm()
	if !s.owned {
		if mode&groupOrOther != 0 {
			return &FileError{FileUnavailable, s.dir, ErrDirNotPrivate}
		}
		return nil
	}
	if mode == 0o700 {
		return nil
	}
	if err := os.Chmod(s.dir, 0o700); err != nil {
		if mode&groupOrOther != 0 {
			err = fmt.Errorf("%w: %v", ErrDirNotPrivate, err)
		}
		return &FileError{FileUnavailable, s.dir, err}
	}
	return nil
}

// readFile reads the data file, first restricting it to 0600 when it grants
// group or other access (for example a restored backup). It reports
// fs.ErrNotExist when there is no file yet.
func (s *Store) readFile() ([]byte, error) {
	f, err := os.Open(s.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if runtime.GOOS != "windows" {
		info, err := f.Stat()
		if err != nil {
			return nil, err
		}
		if info.Mode().Perm()&groupOrOther != 0 {
			if err := f.Chmod(0o600); err != nil {
				return nil, fmt.Errorf("%w: %v", ErrFileNotPrivate, err)
			}
		}
	}
	return io.ReadAll(f)
}

// syncDir makes the rename durable on Unix; Windows has no directory fsync.
func syncDir(dir string) {
	if runtime.GOOS == "windows" {
		return
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

// Path is the data file location.
func (s *Store) Path() string { return s.path }

// Dir is the data directory.
func (s *Store) Dir() string { return s.dir }

// load returns the accounts sorted by order. A missing file is an empty list.
// It runs before every read and write, so no credential is read from, and no
// change is written to, a directory or file other users can reach.
func (s *Store) load() ([]Account, error) {
	if err := s.checkDir(); err != nil {
		return nil, err
	}
	raw, err := s.readFile()
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, &FileError{FileUnavailable, s.path, err}
	}
	var head struct {
		SchemaVersion *int `json:"schema_version"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, &FileError{FileCorrupt, s.path, err}
	}
	if head.SchemaVersion == nil || *head.SchemaVersion < 1 {
		return nil, &FileError{FileCorrupt, s.path, errors.New("missing schema_version")}
	}
	if *head.SchemaVersion > SchemaVersion {
		return nil, &FileError{FileNewer, s.path, fmt.Errorf("schema_version %d is newer than %d", *head.SchemaVersion, SchemaVersion)}
	}
	var doc document
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&doc); err != nil {
		return nil, &FileError{FileCorrupt, s.path, err}
	}
	seen := make(map[string]bool, len(doc.Accounts))
	for i := range doc.Accounts {
		a := &doc.Accounts[i]
		if a.ID == "" || seen[a.ID] {
			return nil, &FileError{FileCorrupt, s.path, errors.New("account id missing or duplicated")}
		}
		seen[a.ID] = true
		// A built-in account saved by an older build may still carry the
		// custom-query secrets it had before switching; they never apply.
		if a.Source != SourceCustom {
			a.CustomPlaceholders.APIKey, a.CustomPlaceholders.AccessToken = "", ""
		}
	}
	sort.SliceStable(doc.Accounts, func(i, j int) bool { return doc.Accounts[i].Order < doc.Accounts[j].Order })
	return doc.Accounts, nil
}

// save renumbers order and atomically replaces the data file: write a 0600
// temporary file in the same directory, flush it, then rename over the target.
func (s *Store) save(accounts []Account) error {
	for i := range accounts {
		accounts[i].Order = i
	}
	if accounts == nil {
		accounts = []Account{}
	}
	raw, err := json.MarshalIndent(document{SchemaVersion: SchemaVersion, Accounts: accounts}, "", "  ")
	if err != nil {
		return &FileError{FileUnavailable, s.path, err}
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(s.dir, ".accounts-*.tmp")
	if err != nil {
		return &FileError{FileUnavailable, s.path, err}
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()
	if err := chmodOwnerOnly(tmpName, 0o600); err != nil {
		tmp.Close()
		return &FileError{FileUnavailable, s.path, err}
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return &FileError{FileUnavailable, s.path, err}
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return &FileError{FileUnavailable, s.path, err}
	}
	if err := tmp.Close(); err != nil {
		return &FileError{FileUnavailable, s.path, err}
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return &FileError{FileUnavailable, s.path, err}
	}
	committed = true
	syncDir(s.dir)
	return nil
}

// List returns all accounts in display order.
func (s *Store) List() ([]Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

// Get returns one account.
func (s *Store) Get(id string) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	accounts, err := s.load()
	if err != nil {
		return Account{}, err
	}
	for _, a := range accounts {
		if a.ID == id {
			return a, nil
		}
	}
	return Account{}, ErrNotFound
}

// Create validates in and appends a new account.
func (s *Store) Create(in Input) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	accounts, err := s.load()
	if err != nil {
		return Account{}, err
	}
	a, err := apply(in, nil)
	if err != nil {
		return Account{}, err
	}
	a.ID, err = newID()
	if err != nil {
		return Account{}, &FileError{FileUnavailable, s.path, err}
	}
	accounts = append(accounts, a)
	if err := s.save(accounts); err != nil {
		return Account{}, err
	}
	return accounts[len(accounts)-1], nil
}

// Update replaces the account's fields with in; blank secrets keep their value.
func (s *Store) Update(id string, in Input) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	accounts, err := s.load()
	if err != nil {
		return Account{}, err
	}
	for i := range accounts {
		if accounts[i].ID != id {
			continue
		}
		next, err := apply(in, &accounts[i])
		if err != nil {
			return Account{}, err
		}
		next.ID = id
		accounts[i] = next
		if err := s.save(accounts); err != nil {
			return Account{}, err
		}
		return accounts[i], nil
	}
	return Account{}, ErrNotFound
}

// Delete removes the account.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	accounts, err := s.load()
	if err != nil {
		return err
	}
	for i := range accounts {
		if accounts[i].ID == id {
			return s.save(append(accounts[:i], accounts[i+1:]...))
		}
	}
	return ErrNotFound
}

func newID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "acc_" + hex.EncodeToString(b[:]), nil
}
