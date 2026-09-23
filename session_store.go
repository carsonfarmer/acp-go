package acp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// SessionStore keeps per-session state for an agent, keyed by the protocol
// version's session id type. Implementations must be safe for concurrent use.
//
// Every method takes the context of the request it serves and can fail, so a
// store backed by a database or a remote cache can report its errors; the
// session manager returns them to the client.
type SessionStore[ID comparable, T any] interface {
	// Get returns the session with the given id, or false if there is none.
	Get(ctx context.Context, id ID) (T, bool, error)
	// Set stores a session, replacing any session with the same id.
	Set(ctx context.Context, id ID, session T) error
	// Delete removes a session. Deleting an unknown id is not an error.
	Delete(ctx context.Context, id ID) error
	// List returns the stored session ids.
	List(ctx context.Context) ([]ID, error)
}

// MemoryStore keeps sessions in memory for the life of the process.
type MemoryStore[ID comparable, T any] struct {
	mu       sync.RWMutex
	sessions map[ID]T
}

// NewMemoryStore creates an empty in-memory store.
func NewMemoryStore[ID comparable, T any]() *MemoryStore[ID, T] {
	return &MemoryStore[ID, T]{sessions: make(map[ID]T)}
}

func (s *MemoryStore[ID, T]) Get(_ context.Context, id ID) (T, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[id]
	return session, ok, nil
}

func (s *MemoryStore[ID, T]) Set(_ context.Context, id ID, session T) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[id] = session
	return nil
}

func (s *MemoryStore[ID, T]) Delete(_ context.Context, id ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
	return nil
}

func (s *MemoryStore[ID, T]) List(context.Context) ([]ID, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]ID, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	return ids, nil
}

// GenerateSessionID returns a random id of the form "session_<32 hex chars>".
func GenerateSessionID() string { return GenerateID("session") }

// GenerateID returns a random id of the form "<prefix>_<32 hex chars>", for
// the ids an agent mints: sessions, messages, tool calls.
func GenerateID(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("acp: generate id: " + err.Error())
	}
	return prefix + "_" + hex.EncodeToString(b)
}
