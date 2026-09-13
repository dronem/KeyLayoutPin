// Package store persists the "field → keyboard layout" memory.
//
// The memory keeps only what it cannot do without:
//   - instead of the field key (app, field label) — its salted SHA-256,
//     because field labels can be personal (a payment code, a contact's name);
//   - no timestamps or names: only the layout and a sequence number,
//     used to drop the oldest entries once there are more than maxEntries.
//
// The salt is random per installation, so precomputed hash tables
// are useless; but whoever has the file can still test a specific
// guess ("was there field X in app Y?") — hashing does not prevent that.
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"change_lang/internal/kbd"
)

const (
	version    = 2
	maxEntries = 1000
)

// entry is the remembered layout of a single field.
type entry struct {
	Layout string `json:"layout"` // HKL in hex, e.g. 0x04110411
	Seq    uint64 `json:"seq"`    // write order — used to drop old entries
}

// Store holds the "field hash → layout" memory and persists it to disk.
type Store struct {
	path    string
	Version int              `json:"version"`
	Salt    string           `json:"salt"`
	Seq     uint64           `json:"seq"`
	Entries map[string]entry `json:"entries"`
}

// DataDir is the app's data folder under %LOCALAPPDATA%: unlike Roaming,
// it is not synced to servers along with the user profile.
func DataDir() string {
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		dir = "."
	}
	return filepath.Join(dir, "change_lang")
}

// DefaultPath is where the memory lives unless -db says otherwise.
func DefaultPath() string {
	return filepath.Join(DataDir(), "layouts.json")
}

// legacyPath is where old versions kept the memory (in plain text).
func legacyPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "change_lang", "layouts.json")
}

// legacyStore is the old format: field keys in plain text.
type legacyStore struct {
	Entries map[string]struct {
		Layout string `json:"layout"`
	} `json:"entries"`
}

// Open loads the memory at path. On the very first run at the default
// path, the old plain-text memory is migrated and deleted.
func Open(path string) (*Store, error) {
	s := &Store{path: path, Version: version, Entries: map[string]entry{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) && path == DefaultPath() {
		if old := legacyPath(); old != "" {
			if data, err = os.ReadFile(old); err == nil {
				if err := s.migrate(data); err != nil {
					return nil, err
				}
				os.Remove(old)
				os.Remove(old + ".tmp")
				return s, nil
			}
		}
		err = nil
		data = nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if len(data) > 0 {
		var head struct{ Version int }
		if err := json.Unmarshal(data, &head); err != nil {
			return nil, err
		}
		if head.Version < version {
			return s, s.migrate(data) // an old-format file passed via -db
		}
		if err := json.Unmarshal(data, s); err != nil {
			return nil, err
		}
		if s.Entries == nil {
			s.Entries = map[string]entry{}
		}
	}
	if s.Salt == "" {
		if err := s.newSalt(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// migrate converts the old memory (plain-text keys) to hashes
// and rewrites the file right away so no plain text is left behind.
func (s *Store) migrate(data []byte) error {
	var old legacyStore
	if err := json.Unmarshal(data, &old); err != nil {
		return err
	}
	if err := s.newSalt(); err != nil {
		return err
	}
	for key, e := range old.Entries {
		s.Seq++
		s.Entries[s.hash(key)] = entry{Layout: e.Layout, Seq: s.Seq}
	}
	s.trim()
	return s.save()
}

func (s *Store) newSalt() error {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	s.Salt = hex.EncodeToString(b)
	return nil
}

func (s *Store) hash(key string) string {
	sum := sha256.Sum256([]byte(s.Salt + "\x00" + key))
	return hex.EncodeToString(sum[:16])
}

// Len is the number of remembered fields.
func (s *Store) Len() int { return len(s.Entries) }

// Get returns the layout remembered for a field key.
func (s *Store) Get(key string) (kbd.HKL, bool) {
	e, ok := s.Entries[s.hash(key)]
	if !ok {
		return 0, false
	}
	return kbd.Parse(e.Layout)
}

// Set remembers the layout of a field and saves the file.
func (s *Store) Set(key string, layout kbd.HKL) error {
	k := s.hash(key)
	if old, ok := s.Entries[k]; ok && old.Layout == layout.String() {
		return nil
	}
	s.Seq++
	s.Entries[k] = entry{Layout: layout.String(), Seq: s.Seq}
	s.trim()
	return s.save()
}

// Forget removes a field's entry and saves the file.
func (s *Store) Forget(key string) error {
	k := s.hash(key)
	if _, ok := s.Entries[k]; !ok {
		return nil
	}
	delete(s.Entries, k)
	return s.save()
}

// trim drops the oldest entries beyond maxEntries.
func (s *Store) trim() {
	for len(s.Entries) > maxEntries {
		var oldest string
		var min uint64
		for k, e := range s.Entries {
			if oldest == "" || e.Seq < min {
				oldest, min = k, e.Seq
			}
		}
		delete(s.Entries, oldest)
	}
}

// save writes to a temp file and renames it, so the memory is not
// corrupted if the process is killed mid-write.
func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
