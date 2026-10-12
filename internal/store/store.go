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

	"github.com/dronem/keylayoutpin/internal/kbd"
)

const maxEntries = 1000

// entry is the remembered layout of a single field.
type entry struct {
	Layout string `json:"layout"` // HKL in hex, e.g. 0x04110411
	Seq    uint64 `json:"seq"`    // write order — used to drop old entries
}

// fileFormat is what is written to disk.
type fileFormat struct {
	Salt    string           `json:"salt"`
	Seq     uint64           `json:"seq"`
	Entries map[string]entry `json:"entries"` // field hash → entry
}

// Store holds the "field hash → layout" memory and persists it to disk.
// It is not safe for concurrent use.
type Store struct {
	path    string
	salt    string
	seq     uint64
	entries map[string]entry
}

// Open loads the memory at path; a missing file means an empty memory.
func Open(path string) (*Store, error) {
	s := &Store{path: path, entries: map[string]entry{}}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if len(data) > 0 {
		var f fileFormat
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, err
		}
		s.salt, s.seq = f.Salt, f.Seq
		if f.Entries != nil {
			s.entries = f.Entries
		}
	}
	if s.salt == "" {
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
	s.salt = hex.EncodeToString(b)
	return nil
}

func (s *Store) hash(key string) string {
	sum := sha256.Sum256([]byte(s.salt + "\x00" + key))
	return hex.EncodeToString(sum[:16])
}

// Len is the number of remembered fields.
func (s *Store) Len() int { return len(s.entries) }

// Get returns the layout remembered for a field key.
func (s *Store) Get(key string) (kbd.HKL, bool) {
	e, ok := s.entries[s.hash(key)]
	if !ok {
		return 0, false
	}
	return kbd.Parse(e.Layout)
}

// Set remembers the layout of a field and saves the file.
func (s *Store) Set(key string, layout kbd.HKL) error {
	k := s.hash(key)
	if old, ok := s.entries[k]; ok && old.Layout == layout.String() {
		return nil
	}
	s.seq++
	s.entries[k] = entry{Layout: layout.String(), Seq: s.seq}
	s.trim()
	return s.save()
}

// Forget removes a field's entry and saves the file.
func (s *Store) Forget(key string) error {
	k := s.hash(key)
	if _, ok := s.entries[k]; !ok {
		return nil
	}
	delete(s.entries, k)
	return s.save()
}

// trim drops the oldest entries beyond maxEntries.
func (s *Store) trim() {
	for len(s.entries) > maxEntries {
		var oldest string
		var oldestSeq uint64
		for k, e := range s.entries {
			if oldest == "" || e.Seq < oldestSeq {
				oldest, oldestSeq = k, e.Seq
			}
		}
		delete(s.entries, oldest)
	}
}

// save writes to a temp file and renames it, so the memory is not
// corrupted if the process is killed mid-write.
func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(fileFormat{Salt: s.salt, Seq: s.seq, Entries: s.entries}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
