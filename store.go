//go:build windows

package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

// The memory keeps only what it cannot do without:
//   - instead of the field key (app, field label) — its salted SHA-256,
//     because field labels can be personal (a payment code, a contact's name);
//   - no timestamps or names: only the layout and a sequence number,
//     used to drop the oldest entries once there are more than maxEntries.
// The salt is random per installation, so precomputed hash tables
// are useless; but whoever has the file can still test a specific
// guess ("was there field X in app Y?") — hashing does not prevent that.

const (
	storeVersion = 2
	maxEntries   = 1000
)

// Entry is the remembered layout of a single field.
type Entry struct {
	Layout string `json:"layout"` // HKL in hex, e.g. 0x04110411
	Seq    uint64 `json:"seq"`    // write order — used to drop old entries
}

func (e Entry) HKL() (HKL, bool) {
	v, err := strconv.ParseUint(e.Layout, 0, 64)
	if err != nil {
		return 0, false
	}
	return HKL(v), true
}

// Store holds the "field hash → layout" memory and persists it to disk.
type Store struct {
	path    string
	Version int              `json:"version"`
	Salt    string           `json:"salt"`
	Seq     uint64           `json:"seq"`
	Entries map[string]Entry `json:"entries"`
}

// defaultStorePath is under %LOCALAPPDATA%: unlike Roaming, this folder
// is not synced to servers along with the user profile.
func defaultStorePath() string {
	return filepath.Join(localDataDir(), "layouts.json")
}

func localDataDir() string {
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		dir = "."
	}
	return filepath.Join(dir, "change_lang")
}

// legacyStorePath is where old versions kept the memory (in plain text).
func legacyStorePath() string {
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

func loadStore(path string) (*Store, error) {
	s := &Store{path: path, Version: storeVersion, Entries: map[string]Entry{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) && path == defaultStorePath() {
		// First run of the new version — migrate the old memory, if any.
		if old := legacyStorePath(); old != "" {
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
		if head.Version < storeVersion {
			return s, s.migrate(data) // an old-format file passed via -db
		}
		if err := json.Unmarshal(data, s); err != nil {
			return nil, err
		}
		if s.Entries == nil {
			s.Entries = map[string]Entry{}
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
		s.Entries[s.hash(key)] = Entry{Layout: e.Layout, Seq: s.Seq}
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

func (s *Store) Get(key string) (Entry, bool) {
	e, ok := s.Entries[s.hash(key)]
	return e, ok
}

func (s *Store) Set(key string, layout HKL) error {
	k := s.hash(key)
	if old, ok := s.Entries[k]; ok && old.Layout == layout.String() {
		return nil
	}
	s.Seq++
	s.Entries[k] = Entry{Layout: layout.String(), Seq: s.Seq}
	s.trim()
	return s.save()
}

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
