package ws

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/gorilla/websocket"

	"uni-backend/internal/models"
)

// SessionManager manages all active relationship sessions.
// It is safe for concurrent use.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*models.RelationshipSession // relationshipID → session
}

// NewSessionManager creates a new SessionManager.
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*models.RelationshipSession),
	}
}

// Register adds a user connection to their relationship session.
// If no session exists for the relationship, one is created.
func (sm *SessionManager) Register(conn *models.UserConnection) {
	sm.mu.Lock()
	session, exists := sm.sessions[conn.RelationshipID]
	if !exists {
		session = models.NewRelationshipSession()
		sm.sessions[conn.RelationshipID] = session
	}
	sm.mu.Unlock()

	session.Lock()
	defer session.Unlock()

	// If the user already has a connection, close the old one.
	if existing, ok := session.Connections[conn.UID]; ok {
		log.Printf("[session] replacing existing connection for user %s in relationship %s", conn.UID, conn.RelationshipID)
		existing.Conn.Close()
	}

	session.Connections[conn.UID] = conn
	session.HoldState[conn.UID] = false

	log.Printf("[session] user %s registered in relationship %s (%d connected)",
		conn.UID, conn.RelationshipID, len(session.Connections))
}

// Unregister removes a user connection from their relationship session.
// It also resets hold state and notifies the partner if needed.
func (sm *SessionManager) Unregister(conn *models.UserConnection) {
	sm.mu.RLock()
	session, exists := sm.sessions[conn.RelationshipID]
	sm.mu.RUnlock()

	if !exists {
		return
	}

	session.Lock()

	// Only remove if it's the same connection (not a replacement).
	if current, ok := session.Connections[conn.UID]; ok && current == conn {
		// If user was holding, treat disconnect as HOLD_END.
		wasHolding := session.HoldState[conn.UID]
		delete(session.Connections, conn.UID)
		delete(session.HoldState, conn.UID)

		log.Printf("[session] user %s disconnected from relationship %s (%d remaining)",
			conn.UID, conn.RelationshipID, len(session.Connections))

		// If user was holding, notify partner to stop vibration.
		if wasHolding {
			sm.broadcastToSession(session, models.ServerMessage{
				Type: models.MsgTypeStopVibration,
			})
		}
	}

	empty := session.IsEmpty()
	session.Unlock()

	// Clean up empty sessions.
	if empty {
		sm.mu.Lock()
		// Double-check under write lock.
		if s, ok := sm.sessions[conn.RelationshipID]; ok {
			s.Lock()
			if s.IsEmpty() {
				delete(sm.sessions, conn.RelationshipID)
				log.Printf("[session] cleaned up empty session for relationship %s", conn.RelationshipID)
			}
			s.Unlock()
		}
		sm.mu.Unlock()
	}
}

// HandleHoldStart marks a user as holding and checks if both are holding.
// If both partners are holding, broadcasts START_VIBRATION.
func (sm *SessionManager) HandleHoldStart(conn *models.UserConnection) {
	sm.mu.RLock()
	session, exists := sm.sessions[conn.RelationshipID]
	sm.mu.RUnlock()

	if !exists {
		log.Printf("[session] no session found for relationship %s", conn.RelationshipID)
		return
	}

	session.Lock()
	defer session.Unlock()

	session.HoldState[conn.UID] = true
	log.Printf("[session] user %s started holding in relationship %s", conn.UID, conn.RelationshipID)

	if session.BothHolding() {
		log.Printf("[session] both users holding in relationship %s — broadcasting START_VIBRATION", conn.RelationshipID)
		sm.broadcastToSession(session, models.ServerMessage{
			Type: models.MsgTypeStartVibration,
		})
	}
}

// HandleHoldEnd marks a user as not holding and broadcasts STOP_VIBRATION
// to all connected users in the session.
func (sm *SessionManager) HandleHoldEnd(conn *models.UserConnection) {
	sm.mu.RLock()
	session, exists := sm.sessions[conn.RelationshipID]
	sm.mu.RUnlock()

	if !exists {
		return
	}

	session.Lock()
	defer session.Unlock()

	wasBothHolding := session.BothHolding()
	session.HoldState[conn.UID] = false

	log.Printf("[session] user %s stopped holding in relationship %s", conn.UID, conn.RelationshipID)

	// Only send STOP_VIBRATION if both were previously holding (vibration was active).
	if wasBothHolding {
		log.Printf("[session] vibration stopped in relationship %s — broadcasting STOP_VIBRATION", conn.RelationshipID)
		sm.broadcastToSession(session, models.ServerMessage{
			Type: models.MsgTypeStopVibration,
		})
	}
}

// broadcastToSession sends a message to all connections in a session.
// Caller must hold the session lock.
func (sm *SessionManager) broadcastToSession(session *models.RelationshipSession, msg models.ServerMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[session] failed to marshal message: %v", err)
		return
	}

	for uid, conn := range session.Connections {
		if err := conn.Conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("[session] failed to send message to user %s: %v", uid, err)
		}
	}
}

// ActiveSessions returns the count of active relationship sessions.
func (sm *SessionManager) ActiveSessions() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.sessions)
}
