package acp

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// SessionStore keeps per-session state for an agent, keyed by the protocol
// version's session id type. Implementations must be safe for concurrent use.
type SessionStore[ID comparable, T any] interface {
	// Get returns the session with the given id, or false if there is none.
	Get(id ID) (T, bool)
	// Set stores a session, replacing any session with the same id.
	Set(id ID, session T)
	// Delete removes a session. Deleting an unknown id is not an error.
	Delete(id ID)
	// List returns the stored session ids.
	List() []ID
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

func (s *MemoryStore[ID, T]) Get(id ID) (T, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[id]
	return session, ok
}

func (s *MemoryStore[ID, T]) Set(id ID, session T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[id] = session
}

func (s *MemoryStore[ID, T]) Delete(id ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

func (s *MemoryStore[ID, T]) List() []ID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]ID, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	return ids
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
