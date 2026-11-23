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

// FileError reports a data file the plugin will not overwrite or cannot use.
type FileError struct {
	Kind FileErrorKind
	Path string
	Err  error
}

func (e *FileError) Error() string { return fmt.Sprintf("%s: %s: %v", e.Kind, e.Path, e.Err) }
func (e *FileError) Unwrap() error { return e.Err }

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
}

// Open prepares dir (created 0700 when missing) and returns a store for its
// data file. An existing dir keeps its own permissions; the file is 0600.
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

// OpenDefault opens DefaultDir and enforces 0700 on it even when it already
// exists, because that directory belongs to the plugin alone.
func OpenDefault() (*Store, error) {
	dir, err := DefaultDir()
	if err != nil {
		return nil, &FileError{FileUnavailable, DirName, err}
	}
	s, err := Open(dir)
	if err != nil {
		return nil, err
	}
	if err := chmodOwnerOnly(dir, 0o700); err != nil {
		return nil, &FileError{FileUnavailable, dir, err}
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
func (s *Store) load() ([]Account, error) {
	raw, err := os.ReadFile(s.path)
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
	for _, a := range doc.Accounts {
		if a.ID == "" || seen[a.ID] {
			return nil, &FileError{FileCorrupt, s.path, errors.New("account id missing or duplicated")}
		}
		seen[a.ID] = true
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
