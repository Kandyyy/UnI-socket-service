package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"uni-backend/internal/auth"
	"uni-backend/internal/models"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// In production, restrict this to your app's origin.
		return true
	},
}

// Handler handles WebSocket connections for the Tap and Hold feature.
type Handler struct {
	firebaseAuth   *auth.FirebaseAuth
	sessionManager *SessionManager
}

// NewHandler creates a new WebSocket handler.
func NewHandler(fa *auth.FirebaseAuth, sm *SessionManager) *Handler {
	return &Handler{
		firebaseAuth:   fa,
		sessionManager: sm,
	}
}

// ServeHTTP handles the initial WebSocket upgrade request.
// It verifies the Firebase token, looks up the relationship, and
// upgrades the connection to a WebSocket.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract token from query parameter.
	token := r.URL.Query().Get("token")
	if token == "" {
		log.Println("[ws] connection rejected: missing token")
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	// Verify Firebase token.
	uid, err := h.firebaseAuth.VerifyToken(ctx, token)
	if err != nil {
		log.Printf("[ws] connection rejected: invalid token: %v", err)
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	log.Printf("[ws] authenticated user %s", uid)

	// Look up user's relationship.
	relInfo, err := h.firebaseAuth.GetRelationship(ctx, uid)
	if err != nil {
		log.Printf("[ws] connection rejected: %v", err)
		http.Error(w, "relationship not found", http.StatusForbidden)
		return
	}

	log.Printf("[ws] user %s belongs to relationship %s (partner: %s)",
		uid, relInfo.RelationshipID, relInfo.PartnerUID)

	// Upgrade to WebSocket.
	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ws] upgrade failed: %v", err)
		return
	}

	conn := &models.UserConnection{
		UID:            uid,
		RelationshipID: relInfo.RelationshipID,
		Conn:           wsConn,
	}

	// Register connection in session manager.
	h.sessionManager.Register(conn)

	// Start read and ping goroutines.
	go h.readPump(conn)
	go h.pingPump(conn)
}

// readPump reads messages from the WebSocket connection.
// It runs in its own goroutine and exits when the connection closes.
func (h *Handler) readPump(conn *models.UserConnection) {
	defer func() {
		h.sessionManager.Unregister(conn)
		conn.Conn.Close()
		log.Printf("[ws] read pump closed for user %s", conn.UID)
	}()

	conn.Conn.SetReadLimit(maxMessageSize)
	conn.Conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.Conn.SetPongHandler(func(string) error {
		conn.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := conn.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[ws] unexpected close for user %s: %v", conn.UID, err)
			} else {
				log.Printf("[ws] connection closed for user %s: %v", conn.UID, err)
			}
			return
		}

		h.handleMessage(conn, message)
	}
}

// handleMessage processes an incoming client message.
func (h *Handler) handleMessage(conn *models.UserConnection, data []byte) {
	var msg models.ClientMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("[ws] invalid message from user %s: %v", conn.UID, err)
		return
	}

	switch msg.Type {
	case models.MsgTypeHoldStart:
		h.sessionManager.HandleHoldStart(conn)

	case models.MsgTypeHoldEnd:
		h.sessionManager.HandleHoldEnd(conn)

	default:
		log.Printf("[ws] unknown message type from user %s: %s", conn.UID, msg.Type)
	}
}

// pingPump sends periodic pings to the client to detect disconnections.
// It runs in its own goroutine.
func (h *Handler) pingPump(conn *models.UserConnection) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		<-ticker.C
		conn.Conn.SetWriteDeadline(time.Now().Add(writeWait))
		if err := conn.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
			log.Printf("[ws] ping failed for user %s: %v", conn.UID, err)
			return
		}
	}
}

// HealthHandler returns server health status including active session count.
func (h *Handler) HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.Context()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":          "ok",
			"active_sessions": h.sessionManager.ActiveSessions(),
			"timestamp":       time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// NewServeMux creates and returns a configured HTTP mux with the
// WebSocket and health endpoints.
func NewServeMux(fa *auth.FirebaseAuth) *http.ServeMux {
	sm := NewSessionManager()
	handler := NewHandler(fa, sm)

	mux := http.NewServeMux()
	mux.Handle("/ws", handler)
	mux.HandleFunc("/health", handler.HealthHandler())

	// Log active sessions periodically.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			count := sm.ActiveSessions()
			if count > 0 {
				log.Printf("[server] active relationship sessions: %d", count)
			}
		}
	}()

	return mux
}
