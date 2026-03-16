package models

// Client → Server message types.
const (
	MsgTypeHoldStart = "HOLD_START"
	MsgTypeHoldEnd   = "HOLD_END"
)

// Server → Client message types.
const (
	MsgTypeStartVibration = "START_VIBRATION"
	MsgTypeStopVibration  = "STOP_VIBRATION"
)

// ClientMessage represents a message sent from the iOS client to the server.
type ClientMessage struct {
	Type string `json:"type"`
}

// ServerMessage represents a message sent from the server to the iOS client.
type ServerMessage struct {
	Type string `json:"type"`
}
