package main

import "time"

// MessageType constants for the WebSocket protocol
const (
	// Client -> Server
	MsgTypeUserInfo    = "user_info"    // FE sends user info to BE
	MsgTypePing        = "ping"         // Heartbeat ping
	MsgTypeJoinRoom    = "join_room"    // Join a room/channel
	MsgTypeLeaveRoom   = "leave_room"   // Leave a room/channel
	MsgTypeDirect      = "direct"       // 1:1 message to specific user
	MsgTypeRoomMessage = "room_message" // Message to a room (1:many)
	MsgTypeBroadcast   = "broadcast"    // Broadcast to all (1:all)

	// Server -> Client
	MsgTypePong        = "pong"         // Heartbeat pong
	MsgTypeError       = "error"        // Error response
	MsgTypeSystem      = "system"       // System notification
	MsgTypeAck         = "ack"          // Acknowledgement
	MsgTypeUserJoined  = "user_joined"  // User joined notification
	MsgTypeUserLeft    = "user_left"    // User left notification
	MsgTypePresence    = "presence"     // Presence update (who's online)
)

// Message is the universal wire protocol for all WebSocket communication.
// Every message between FE and BE uses this envelope.
type Message struct {
	Type      string      `json:"type"`                // Message type (see constants above)
	ID        string      `json:"id,omitempty"`        // Unique message ID for dedup/ack
	UserID    string      `json:"user_id,omitempty"`   // Sender's user ID
	TargetID  string      `json:"target_id,omitempty"` // Target user ID (for direct messages)
	Room      string      `json:"room,omitempty"`      // Room/channel name
	Payload   interface{} `json:"payload,omitempty"`   // Arbitrary payload data
	Timestamp int64       `json:"timestamp"`           // Unix timestamp (ms)
}

// UserInfo represents user information sent from FE to BE or pushed from BE to FE.
type UserInfo struct {
	UserID      string            `json:"user_id"`
	DisplayName string            `json:"display_name,omitempty"`
	Avatar      string            `json:"avatar,omitempty"`
	Status      string            `json:"status,omitempty"` // online, away, busy, offline
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// NewMessage creates a new Message with a timestamp.
func NewMessage(msgType string, payload interface{}) Message {
	return Message{
		Type:      msgType,
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}
}

// NewSystemMessage creates a system notification message.
func NewSystemMessage(text string) Message {
	return Message{
		Type:      MsgTypeSystem,
		Payload:   map[string]string{"text": text},
		Timestamp: time.Now().UnixMilli(),
	}
}

// NewErrorMessage creates an error response message.
func NewErrorMessage(err string) Message {
	return Message{
		Type:      MsgTypeError,
		Payload:   map[string]string{"error": err},
		Timestamp: time.Now().UnixMilli(),
	}
}

// NewAckMessage creates an acknowledgement for a received message.
func NewAckMessage(originalID string) Message {
	return Message{
		Type:      MsgTypeAck,
		ID:        originalID,
		Payload:   map[string]string{"status": "ok"},
		Timestamp: time.Now().UnixMilli(),
	}
}
