package hub

import (
	"encoding/json"
	"log"
	"sync"
	"time"
)

// ValkeyPublisher is an interface for cross-node message delivery.
type ValkeyPublisher interface {
	PublishBroadcast(msg Message)
	PublishRoom(room string, msg Message)
	PublishDirect(userID string, msg Message)
}

// SSEPublisher is an interface for pushing messages to SSE subscribers.
type SSEPublisher interface {
	Publish(channel string, msg Message) int
	PublishAll(msg Message) int
}

// Hub maintains the set of active clients and broadcasts messages.
type Hub struct {
	Clients    map[string]*Client
	Rooms      map[string]map[*Client]bool
	Register   chan *Client
	Unregister chan *Client
	Broadcast  chan []byte
	Mu         sync.RWMutex
	valkey     ValkeyPublisher
	sse        SSEPublisher
}

// New creates a new Hub instance.
func New() *Hub {
	return &Hub{
		Clients:    make(map[string]*Client),
		Rooms:      make(map[string]map[*Client]bool),
		Register:   make(chan *Client),
		Unregister: make(chan *Client),
		Broadcast:  make(chan []byte),
	}
}

// SetValkey attaches a Valkey pub/sub bridge for cross-node messaging.
func (h *Hub) SetValkey(v ValkeyPublisher) {
	h.valkey = v
}

// SetSSE attaches the SSE broker so WS messages also reach SSE subscribers.
func (h *Hub) SetSSE(s SSEPublisher) {
	h.sse = s
}

// Run starts the Hub's main event loop.
func (h *Hub) Run() {
	log.Println("[hub] started")

	for {
		select {
		case client := <-h.Register:
			h.Mu.Lock()
			if existing, ok := h.Clients[client.UserID]; ok {
				existing.SendMessage(NewSystemMessage("connected from another location"))
				close(existing.Send)
				delete(h.Clients, existing.UserID)
				log.Printf("[hub] kicked existing connection for user: %s", client.UserID)
			}
			h.Clients[client.UserID] = client
			h.Mu.Unlock()

			log.Printf("[hub] registered user: %s (total: %d)", client.UserID, h.ClientCount())

			client.SendMessage(Message{
				Type:      MsgTypeSystem,
				Payload:   map[string]interface{}{"text": "connected", "user_id": client.UserID},
				Timestamp: time.Now().UnixMilli(),
			})

			h.BroadcastMessage(Message{
				Type:      MsgTypeUserJoined,
				UserID:    client.UserID,
				Payload:   map[string]string{"user_id": client.UserID},
				Timestamp: time.Now().UnixMilli(),
			})

		case client := <-h.Unregister:
			h.Mu.Lock()
			if _, ok := h.Clients[client.UserID]; ok {
				delete(h.Clients, client.UserID)

				for room := range client.Rooms {
					if members, ok := h.Rooms[room]; ok {
						delete(members, client)
						if len(members) == 0 {
							delete(h.Rooms, room)
						}
					}
				}

				close(client.Send)
			}
			h.Mu.Unlock()

			if client.Metrics != nil {
				client.Metrics.WsConnections.Add(-1)
			}

			log.Printf("[hub] unregistered user: %s (total: %d)", client.UserID, h.ClientCount())

			h.BroadcastMessage(Message{
				Type:      MsgTypeUserLeft,
				UserID:    client.UserID,
				Payload:   map[string]string{"user_id": client.UserID},
				Timestamp: time.Now().UnixMilli(),
			})

		case message := <-h.Broadcast:
			// Legacy channel for raw byte broadcasts (kept for compatibility)
			h.Mu.RLock()
			for _, client := range h.Clients {
				select {
				case client.Send <- message:
				default:
				}
			}
			h.Mu.RUnlock()
		}
	}
}

// SendToUser sends a message directly to a specific user.
func (h *Hub) SendToUser(userID string, msg Message) bool {
	h.Mu.RLock()
	client, ok := h.Clients[userID]
	h.Mu.RUnlock()

	if !ok {
		if h.valkey != nil {
			h.valkey.PublishDirect(userID, msg)
			return true
		}
		return false
	}

	client.SendMessage(msg)
	return true
}

// JoinRoom adds a client to a room.
func (h *Hub) JoinRoom(room string, client *Client) {
	h.Mu.Lock()
	defer h.Mu.Unlock()

	if h.Rooms[room] == nil {
		h.Rooms[room] = make(map[*Client]bool)
	}
	h.Rooms[room][client] = true
}

// LeaveRoom removes a client from a room.
func (h *Hub) LeaveRoom(room string, client *Client) {
	h.Mu.Lock()
	defer h.Mu.Unlock()

	if members, ok := h.Rooms[room]; ok {
		delete(members, client)
		if len(members) == 0 {
			delete(h.Rooms, room)
		}
	}
}

// BroadcastToRoom sends a message to all members of a room.
func (h *Hub) BroadcastToRoom(room string, msg Message, excludeUserID string) {
	h.Mu.RLock()
	members, ok := h.Rooms[room]
	h.Mu.RUnlock()

	if !ok {
		return
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[hub] marshal error: %v", err)
		return
	}

	h.Mu.RLock()
	for client := range members {
		if client.UserID == excludeUserID {
			continue
		}
		select {
		case client.Send <- data:
		default:
			log.Printf("[hub] slow client in room %s, dropping for user: %s", room, client.UserID)
		}
	}
	h.Mu.RUnlock()

	// Also deliver to SSE subscribers
	if h.sse != nil {
		h.sse.Publish(room, msg)
	}

	if h.valkey != nil {
		h.valkey.PublishRoom(room, msg)
	}
}

// BroadcastMessage sends a message to all connected clients.
func (h *Hub) BroadcastMessage(msg Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[hub] marshal error: %v", err)
		return
	}

	// Send directly to all clients (avoid channel to prevent deadlocks)
	h.Mu.RLock()
	for _, client := range h.Clients {
		select {
		case client.Send <- data:
		default:
			log.Printf("[hub] slow client, dropping broadcast for user: %s", client.UserID)
		}
	}
	h.Mu.RUnlock()

	// Also deliver to all SSE subscribers
	if h.sse != nil {
		h.sse.PublishAll(msg)
	}

	if h.valkey != nil {
		h.valkey.PublishBroadcast(msg)
	}
}

// ClientCount returns the number of connected clients.
func (h *Hub) ClientCount() int {
	h.Mu.RLock()
	defer h.Mu.RUnlock()
	return len(h.Clients)
}

// RoomCount returns the number of active rooms.
func (h *Hub) RoomCount() int {
	h.Mu.RLock()
	defer h.Mu.RUnlock()
	return len(h.Rooms)
}

// RoomMemberCount returns the member count for a specific room.
func (h *Hub) RoomMemberCount(room string) int {
	h.Mu.RLock()
	defer h.Mu.RUnlock()
	if members, ok := h.Rooms[room]; ok {
		return len(members)
	}
	return 0
}

// DisconnectKey drops all connected WebSocket clients using the specified API key.
func (h *Hub) DisconnectKey(apiKey string) int {
	h.Mu.Lock()
	defer h.Mu.Unlock()

	count := 0
	for _, client := range h.Clients {
		if client.APIKey == apiKey {
			client.Conn.Close()
			count++
		}
	}
	if count > 0 {
		log.Printf("[hub] dropped %d clients for revoked key: %s...", count, apiKey[:8])
	}
	return count
}

// GetOnlineUsers returns a list of all connected user IDs.
func (h *Hub) GetOnlineUsers() []string {
	h.Mu.RLock()
	defer h.Mu.RUnlock()

	users := make([]string, 0, len(h.Clients))
	for uid := range h.Clients {
		users = append(users, uid)
	}
	return users
}

// GetRoomMembers returns a list of user IDs in a specific room.
func (h *Hub) GetRoomMembers(room string) []string {
	h.Mu.RLock()
	defer h.Mu.RUnlock()

	members, ok := h.Rooms[room]
	if !ok {
		return []string{}
	}

	users := make([]string, 0, len(members))
	for client := range members {
		users = append(users, client.UserID)
	}
	return users
}

// GetRooms returns a list of all active room names.
func (h *Hub) GetRooms() []string {
	h.Mu.RLock()
	defer h.Mu.RUnlock()

	rooms := make([]string, 0, len(h.Rooms))
	for name := range h.Rooms {
		rooms = append(rooms, name)
	}
	return rooms
}
