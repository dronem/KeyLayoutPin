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

	"keylayoutpin/internal/kbd"
)

const maxEntries = 1000

// entry is the remembered layout of a single field.
type entry struct {
	Layout string `json:"layout"` // HKL in hex, e.g. 0x04110411
	Seq    uint64 `json:"seq"`    // write order — used to drop old entries
}

// Store holds the "field hash → layout" memory and persists it to disk.
type Store struct {
	path    string
	Salt    string           `json:"salt"`
	Seq     uint64           `json:"seq"`
	Entries map[string]entry `json:"entries"`
}

// DataDir is the app's data folder under %LOCALAPPDATA%: unlike Roaming,
// it is not synced to servers along with the user profile.
func DataDir() string { return filepath.Join(localAppData(), "KeyLayoutPin") }

func localAppData() string {
	if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
		return dir
	}
	return "."
}

// DefaultPath is where the memory lives unless -db says otherwise.
func DefaultPath() string {
	return filepath.Join(DataDir(), "layouts.json")
}

// Open loads the memory at path; a missing file means an empty memory.
func Open(path string) (*Store, error) {
	s := &Store{path: path, Entries: map[string]entry{}}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if len(data) > 0 {
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
