package main

import (
	"encoding/json"
	"log"
	"sync"
	"time"
)

// Hub maintains the set of active clients and broadcasts messages.
// It is the central coordination point — all client registration,
// unregistration, and message routing flows through the Hub.
type Hub struct {
	// Registered clients, keyed by user ID.
	clients map[string]*Client

	// Room memberships: room name -> set of clients.
	rooms map[string]map[*Client]bool

	// Inbound channel: register a new client.
	register chan *Client

	// Inbound channel: unregister a client.
	unregister chan *Client

	// Inbound channel: broadcast a message to all clients.
	broadcast chan []byte

	// Mutex for thread-safe client/room access.
	mu sync.RWMutex

	// Valkey pub/sub bridge (nil if running in standalone mode).
	valkey *ValkeyBridge
}

// NewHub creates a new Hub instance.
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		rooms:      make(map[string]map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan []byte),
	}
}

// SetValkey attaches a Valkey pub/sub bridge for cross-node messaging.
func (h *Hub) SetValkey(v *ValkeyBridge) {
	h.valkey = v
}

// Run starts the Hub's main event loop. This should be run as a goroutine.
func (h *Hub) Run() {
	log.Println("[hub] started")

	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			// If there's an existing connection with the same user ID, kick it
			if existing, ok := h.clients[client.userID]; ok {
				existing.sendMessage(NewSystemMessage("connected from another location"))
				close(existing.send)
				delete(h.clients, existing.userID)
				log.Printf("[hub] kicked existing connection for user: %s", client.userID)
			}
			h.clients[client.userID] = client
			h.mu.Unlock()

			log.Printf("[hub] registered user: %s (total: %d)", client.userID, h.ClientCount())

			// Send welcome message
			client.sendMessage(Message{
				Type:      MsgTypeSystem,
				Payload:   map[string]interface{}{"text": "connected", "user_id": client.userID},
				Timestamp: time.Now().UnixMilli(),
			})

			// Broadcast presence to all
			h.broadcastMessage(Message{
				Type:      MsgTypeUserJoined,
				UserID:    client.userID,
				Payload:   map[string]string{"user_id": client.userID},
				Timestamp: time.Now().UnixMilli(),
			})

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.userID]; ok {
				delete(h.clients, client.userID)

				// Remove from all rooms
				for room := range client.rooms {
					if members, ok := h.rooms[room]; ok {
						delete(members, client)
						if len(members) == 0 {
							delete(h.rooms, room)
						}
					}
				}

				close(client.send)
			}
			h.mu.Unlock()

			log.Printf("[hub] unregistered user: %s (total: %d)", client.userID, h.ClientCount())

			// Broadcast departure
			h.broadcastMessage(Message{
				Type:      MsgTypeUserLeft,
				UserID:    client.userID,
				Payload:   map[string]string{"user_id": client.userID},
				Timestamp: time.Now().UnixMilli(),
			})

		case message := <-h.broadcast:
			h.mu.RLock()
			for _, client := range h.clients {
				select {
				case client.send <- message:
				default:
					// Slow client — will be cleaned up
					log.Printf("[hub] slow client, dropping message for user: %s", client.userID)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// SendToUser sends a message directly to a specific user (1:1).
// Returns false if the user is not connected to this node.
func (h *Hub) SendToUser(userID string, msg Message) bool {
	h.mu.RLock()
	client, ok := h.clients[userID]
	h.mu.RUnlock()

	if !ok {
		// If Valkey is connected, publish for other nodes to deliver.
		if h.valkey != nil {
			h.valkey.PublishDirect(userID, msg)
			return true // Optimistic — another node may have the user.
		}
		return false
	}

	client.sendMessage(msg)
	return true
}

// JoinRoom adds a client to a room.
func (h *Hub) JoinRoom(room string, client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.rooms[room] == nil {
		h.rooms[room] = make(map[*Client]bool)
	}
	h.rooms[room][client] = true
}

// LeaveRoom removes a client from a room.
func (h *Hub) LeaveRoom(room string, client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if members, ok := h.rooms[room]; ok {
		delete(members, client)
		if len(members) == 0 {
			delete(h.rooms, room)
		}
	}
}

// BroadcastToRoom sends a message to all members of a room (1:many).
// If excludeUserID is non-empty, that user is skipped.
func (h *Hub) BroadcastToRoom(room string, msg Message, excludeUserID string) {
	h.mu.RLock()
	members, ok := h.rooms[room]
	h.mu.RUnlock()

	if !ok {
		return
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[hub] marshal error: %v", err)
		return
	}

	h.mu.RLock()
	for client := range members {
		if client.userID == excludeUserID {
			continue
		}
		select {
		case client.send <- data:
		default:
			log.Printf("[hub] slow client in room %s, dropping for user: %s", room, client.userID)
		}
	}
	h.mu.RUnlock()

	// Also publish to Valkey for cross-node delivery
	if h.valkey != nil {
		h.valkey.PublishRoom(room, msg)
	}
}

// broadcastMessage sends a message to all connected clients.
func (h *Hub) broadcastMessage(msg Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[hub] marshal error: %v", err)
		return
	}

	h.broadcast <- data

	// Also publish to Valkey for cross-node delivery
	if h.valkey != nil {
		h.valkey.PublishBroadcast(msg)
	}
}

// ClientCount returns the number of connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// RoomCount returns the number of active rooms.
func (h *Hub) RoomCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.rooms)
}

// DisconnectKey drops all connected WebSocket clients using the specified API key.
func (h *Hub) DisconnectKey(apiKey string) int {
	h.mu.Lock()
	defer h.mu.Unlock()

	count := 0
	for _, client := range h.clients {
		if client.apiKey == apiKey {
			client.conn.Close() // This will trigger the read/write pumps to error and unregister
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
	h.mu.RLock()
	defer h.mu.RUnlock()

	users := make([]string, 0, len(h.clients))
	for uid := range h.clients {
		users = append(users, uid)
	}
	return users
}

// GetRoomMembers returns a list of user IDs in a specific room.
func (h *Hub) GetRoomMembers(room string) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	members, ok := h.rooms[room]
	if !ok {
		return nil
	}

	users := make([]string, 0, len(members))
	for client := range members {
		users = append(users, client.userID)
	}
	return users
}
