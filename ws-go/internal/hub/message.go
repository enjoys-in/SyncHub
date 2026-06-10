package hub

import "time"

// MessageType constants for the WebSocket protocol.
const (
	// Client -> Server
	MsgTypeUserInfo    = "user_info"
	MsgTypePing        = "ping"
	MsgTypeJoinRoom    = "join_room"
	MsgTypeLeaveRoom   = "leave_room"
	MsgTypeDirect      = "direct"
	MsgTypeRoomMessage = "room_message"
	MsgTypeBroadcast   = "broadcast"

	// Server -> Client
	MsgTypePong       = "pong"
	MsgTypeError      = "error"
	MsgTypeSystem     = "system"
	MsgTypeAck        = "ack"
	MsgTypeUserJoined = "user_joined"
	MsgTypeUserLeft   = "user_left"
	MsgTypePresence   = "presence"
)

// Message is the universal wire protocol for all WebSocket communication.
type Message struct {
	Type      string      `json:"type"`
	ID        string      `json:"id,omitempty"`
	UserID    string      `json:"user_id,omitempty"`
	TargetID  string      `json:"target_id,omitempty"`
	Room      string      `json:"room,omitempty"`
	Payload   interface{} `json:"payload,omitempty"`
	Timestamp int64       `json:"timestamp"`
}

// UserInfo represents user information.
type UserInfo struct {
	UserID      string            `json:"user_id"`
	DisplayName string            `json:"display_name,omitempty"`
	Avatar      string            `json:"avatar,omitempty"`
	Status      string            `json:"status,omitempty"`
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
