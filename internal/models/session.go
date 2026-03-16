package models

import (
	"sync"

	"github.com/gorilla/websocket"
)

// UserConnection represents an authenticated user's WebSocket connection.
type UserConnection struct {
	UID            string
	RelationshipID string
	Conn           *websocket.Conn
}

// RelationshipSession tracks the state of a "Tap and Hold" session
// between two partners in a relationship.
type RelationshipSession struct {
	mu          sync.Mutex
	Connections map[string]*UserConnection // UID → connection
	HoldState   map[string]bool            // UID → holding
}

// NewRelationshipSession creates a new empty session.
func NewRelationshipSession() *RelationshipSession {
	return &RelationshipSession{
		Connections: make(map[string]*UserConnection),
		HoldState:   make(map[string]bool),
	}
}

// Lock acquires the session mutex.
func (s *RelationshipSession) Lock() {
	s.mu.Lock()
}

// Unlock releases the session mutex.
func (s *RelationshipSession) Unlock() {
	s.mu.Unlock()
}

// BothHolding returns true if at least two users are connected
// and all connected users have holdState = true.
func (s *RelationshipSession) BothHolding() bool {
	if len(s.HoldState) < 2 {
		return false
	}
	for _, holding := range s.HoldState {
		if !holding {
			return false
		}
	}
	return true
}

// IsEmpty returns true if no connections remain in the session.
func (s *RelationshipSession) IsEmpty() bool {
	return len(s.Connections) == 0
}
