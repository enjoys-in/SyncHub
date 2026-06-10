package hub

import (
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	h := New()
	if h == nil {
		t.Fatal("New() should return non-nil hub")
	}
	if h.Clients == nil {
		t.Error("Clients map should be initialized")
	}
	if h.Rooms == nil {
		t.Error("Rooms map should be initialized")
	}
	if h.Register == nil || h.Unregister == nil || h.Broadcast == nil {
		t.Error("channels should be initialized")
	}
}

func TestHub_ClientCount(t *testing.T) {
	h := New()
	if h.ClientCount() != 0 {
		t.Errorf("expected 0 clients, got %d", h.ClientCount())
	}

	h.Mu.Lock()
	h.Clients["user1"] = &Client{UserID: "user1", Send: make(chan []byte, 256)}
	h.Clients["user2"] = &Client{UserID: "user2", Send: make(chan []byte, 256)}
	h.Mu.Unlock()

	if h.ClientCount() != 2 {
		t.Errorf("expected 2 clients, got %d", h.ClientCount())
	}
}

func TestHub_JoinRoom(t *testing.T) {
	h := New()
	client := &Client{
		Hub:    h,
		UserID: "user1",
		Send:   make(chan []byte, 256),
		Rooms:  make(map[string]bool),
	}

	h.Mu.Lock()
	h.Clients["user1"] = client
	h.Mu.Unlock()

	h.JoinRoom("room-a", client)

	h.Mu.RLock()
	members, exists := h.Rooms["room-a"]
	h.Mu.RUnlock()

	if !exists {
		t.Fatal("room-a should exist")
	}
	if !members[client] {
		t.Error("client should be in room-a")
	}
}

func TestHub_LeaveRoom(t *testing.T) {
	h := New()
	client := &Client{
		Hub:    h,
		UserID: "user1",
		Send:   make(chan []byte, 256),
		Rooms:  make(map[string]bool),
	}

	h.Mu.Lock()
	h.Clients["user1"] = client
	h.Mu.Unlock()

	h.JoinRoom("room-a", client)
	h.LeaveRoom("room-a", client)

	h.Mu.RLock()
	_, exists := h.Rooms["room-a"]
	h.Mu.RUnlock()

	if exists {
		t.Error("room-a should be removed when empty")
	}
	if client.Rooms["room-a"] {
		t.Error("client should not have room-a")
	}
}

func TestHub_GetOnlineUsers(t *testing.T) {
	h := New()
	h.Mu.Lock()
	h.Clients["user1"] = &Client{UserID: "user1", Send: make(chan []byte, 256)}
	h.Clients["user2"] = &Client{UserID: "user2", Send: make(chan []byte, 256)}
	h.Mu.Unlock()

	users := h.GetOnlineUsers()
	if len(users) != 2 {
		t.Errorf("expected 2 users, got %d", len(users))
	}
}

func TestHub_GetRooms(t *testing.T) {
	h := New()
	client := &Client{
		Hub:    h,
		UserID: "user1",
		Send:   make(chan []byte, 256),
		Rooms:  make(map[string]bool),
	}

	h.Mu.Lock()
	h.Clients["user1"] = client
	h.Mu.Unlock()

	h.JoinRoom("room-a", client)
	h.JoinRoom("room-b", client)

	rooms := h.GetRooms()
	if len(rooms) != 2 {
		t.Errorf("expected 2 rooms, got %d", len(rooms))
	}
}

func TestHub_GetRoomMembers(t *testing.T) {
	h := New()
	c1 := &Client{Hub: h, UserID: "user1", Send: make(chan []byte, 256), Rooms: make(map[string]bool)}
	c2 := &Client{Hub: h, UserID: "user2", Send: make(chan []byte, 256), Rooms: make(map[string]bool)}

	h.Mu.Lock()
	h.Clients["user1"] = c1
	h.Clients["user2"] = c2
	h.Mu.Unlock()

	h.JoinRoom("room-a", c1)
	h.JoinRoom("room-a", c2)

	members := h.GetRoomMembers("room-a")
	if len(members) != 2 {
		t.Errorf("expected 2 members, got %d", len(members))
	}

	empty := h.GetRoomMembers("nonexistent")
	if len(empty) != 0 {
		t.Error("nonexistent room should have 0 members")
	}
}

func TestNewMessage(t *testing.T) {
	msg := NewMessage(MsgTypeBroadcast, map[string]string{"text": "hello"})
	if msg.Type != MsgTypeBroadcast {
		t.Errorf("expected type %s, got %s", MsgTypeBroadcast, msg.Type)
	}
	if msg.Timestamp == 0 {
		t.Error("timestamp should be set")
	}
	now := time.Now().UnixMilli()
	if msg.Timestamp > now || msg.Timestamp < now-1000 {
		t.Error("timestamp should be recent")
	}
}

func TestNewSystemMessage(t *testing.T) {
	msg := NewSystemMessage("hello world")
	if msg.Type != MsgTypeSystem {
		t.Errorf("expected type %s, got %s", MsgTypeSystem, msg.Type)
	}
	payload, ok := msg.Payload.(map[string]string)
	if !ok {
		t.Fatal("payload should be map[string]string")
	}
	if payload["text"] != "hello world" {
		t.Errorf("expected 'hello world', got '%s'", payload["text"])
	}
}

func TestNewErrorMessage(t *testing.T) {
	msg := NewErrorMessage("something broke")
	if msg.Type != MsgTypeError {
		t.Errorf("expected type %s, got %s", MsgTypeError, msg.Type)
	}
	payload, ok := msg.Payload.(map[string]string)
	if !ok {
		t.Fatal("payload should be map[string]string")
	}
	if payload["error"] != "something broke" {
		t.Error("error text mismatch")
	}
}

func TestNewAckMessage(t *testing.T) {
	msg := NewAckMessage("msg-123")
	if msg.Type != MsgTypeAck {
		t.Errorf("expected type %s, got %s", MsgTypeAck, msg.Type)
	}
	if msg.ID != "msg-123" {
		t.Errorf("expected ID 'msg-123', got '%s'", msg.ID)
	}
}
