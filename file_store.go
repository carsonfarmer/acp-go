package acp

import (
	"context"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// fileStoreExt is the extension of a session file; fileStoreTemp prefixes a
// file being written, which a crash can leave behind.
const (
	fileStoreExt  = ".json"
	fileStoreTemp = ".tmp-"
)

// FileStore keeps sessions in memory like [MemoryStore] and writes each one to
// its own JSON file in a directory, so the sessions survive a restart: a new
// FileStore on the same directory loads them, and session/resume and
// session/list find them again.
//
// Get and List read the in-memory copy, so they return the same value every
// time, as [MemoryStore] does, and session state can be changed in place. Only
// Set writes to disk: the session manager sets a session when it creates it,
// so an agent that changes a session afterwards calls Set again to save it,
// typically when a turn ends:
//
//	if err := manager.Store().Set(ctx, id, session); err != nil { ... }
//
// A session is encoded with encoding/json/v2, which skips unexported fields,
// so session state with unexported fields implements [json.Marshaler] and
// [json.Unmarshaler], taking its own lock while it encodes.
//
// Each file is named after the hex-encoded session id, so any id is a safe
// file name; an id of up to 122 bytes fits common file name limits. A file is
// written to a temporary name, synced, renamed into place, and the directory
// synced, so a crash or power loss leaves either the previous version or the
// new one. Where a directory cannot be synced, as on Windows, the rename is as
// durable as the file system makes it. One process at a time may use a
// directory: the store does not see files another process writes.
type FileStore[ID ~string, T any] struct {
	dir string

	// writeMu orders the file writes, so the last Set of a session is the one
	// on disk; mu guards sessions alone, so Get never waits on the disk.
	writeMu  sync.Mutex
	mu       sync.RWMutex
	sessions map[ID]T
}

// NewFileStore opens a store in dir, creating the directory if needed, and
// loads every session saved there. A file that does not decode is an error,
// so a store never starts without sessions it was meant to keep.
func NewFileStore[ID ~string, T any](dir string) (*FileStore[ID, T], error) {
	_, statErr := os.Stat(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("acp: open file store: %w", err)
	}
	if errors.Is(statErr, fs.ErrNotExist) {
		// Keep the new directory's own entry, or its sessions go with it.
		if err := syncDir(filepath.Dir(dir)); err != nil {
			return nil, fmt.Errorf("acp: open file store: %w", err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("acp: open file store: %w", err)
	}
	s := &FileStore[ID, T]{dir: dir, sessions: make(map[ID]T)}
	for _, entry := range entries {
		name := entry.Name()
		switch {
		case entry.IsDir():
		case strings.HasPrefix(name, fileStoreTemp):
			// An interrupted write; the previous version, if any, is intact.
			_ = os.Remove(filepath.Join(dir, name))
		case strings.HasSuffix(name, fileStoreExt):
			id, session, err := s.load(name)
			if err != nil {
				return nil, fmt.Errorf("acp: open file store: %s: %w", name, err)
			}
			s.sessions[id] = session
		}
	}
	return s, nil
}

func (s *FileStore[ID, T]) load(name string) (ID, T, error) {
	var session T
	raw, err := hex.DecodeString(strings.TrimSuffix(name, fileStoreExt))
	if err != nil {
		return "", session, errors.New("file name is not a hex-encoded session id")
	}
	data, err := os.ReadFile(filepath.Join(s.dir, name))
	if err != nil {
		return "", session, err
	}
	if err := json.Unmarshal(data, &session); err != nil {
		return "", session, err
	}
	return ID(raw), session, nil
}

func (s *FileStore[ID, T]) path(id ID) string {
	return filepath.Join(s.dir, hex.EncodeToString([]byte(id))+fileStoreExt)
}

func (s *FileStore[ID, T]) Get(_ context.Context, id ID) (T, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[id]
	return session, ok, nil
}

// Set writes the session to its file, then makes it the in-memory copy. When
// the write fails, the store keeps the previous version in memory and on disk.
// When only the final directory sync fails, the new version is in place in
// both, but may not survive a power loss, and Set reports the error.
func (s *FileStore[ID, T]) Set(_ context.Context, id ID, session T) error {
	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("acp: encode session %s: %w", id, err)
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.write(s.path(id), data); err != nil {
		return fmt.Errorf("acp: save session %s: %w", id, err)
	}
	s.mu.Lock()
	s.sessions[id] = session
	s.mu.Unlock()
	if err := syncDir(s.dir); err != nil {
		return fmt.Errorf("acp: save session %s: %w", id, err)
	}
	return nil
}

// write replaces path with data through a synced temporary file, so a reader
// or a crash sees either the old contents or the new ones. The caller syncs
// the directory to make the rename itself durable.
func (s *FileStore[ID, T]) write(path string, data []byte) error {
	f, err := os.CreateTemp(s.dir, fileStoreTemp+"*")
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		_ = os.Remove(f.Name())
	}
	return err
}

func (s *FileStore[ID, T]) Delete(_ context.Context, id ID) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := os.Remove(s.path(id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("acp: delete session %s: %w", id, err)
	}
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
	if err := syncDir(s.dir); err != nil {
		return fmt.Errorf("acp: delete session %s: %w", id, err)
	}
	return nil
}

func (s *FileStore[ID, T]) List(context.Context) ([]ID, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]ID, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	return ids, nil
}
