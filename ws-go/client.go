package main

import (
	"encoding/json"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer (64KB).
	maxMessageSize = 64 * 1024
)

// Client represents a single WebSocket connection.
// It acts as the intermediary between the WebSocket connection and the Hub.
type Client struct {
	hub    *Hub
	conn   *websocket.Conn
	send   chan []byte
	userID string // Unique identifier for the client connection
	apiKey string // The API key used to establish this connection
	rooms  map[string]bool // rooms this client has joined
	info   *UserInfo       // user info received from FE
	rateLimit chan struct{}
}

// NewClient creates a new Client instance.
func NewClient(hub *Hub, conn *websocket.Conn, userID string, apiKey string) *Client {
	return &Client{
		hub:       hub,
		conn:      conn,
		send:      make(chan []byte, 256),
		userID:    userID,
		apiKey:    apiKey,
		rooms:     make(map[string]bool),
		rateLimit: make(chan struct{}, 100),
	}
}

// ReadPump pumps messages from the WebSocket connection to the Hub.
//
// The application runs ReadPump in a per-connection goroutine. It ensures
// that there is at most one reader on a connection by executing all reads
// from this goroutine.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, rawMsg, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseNormalClosure,
				websocket.CloseAbnormalClosure,
			) {
				log.Printf("[client:%s] read error: %v", c.userID, err)
			}
			break
		}

		// Parse the message
		var msg Message
		if err := json.Unmarshal(rawMsg, &msg); err != nil {
			c.sendMessage(NewErrorMessage("invalid message format"))
			continue
		}

		// Stamp the sender
		msg.UserID = c.userID

		// Route based on message type
		c.handleMessage(msg)
	}
}

// WritePump pumps messages from the Hub to the WebSocket connection.
//
// A goroutine running WritePump is started for each connection. It ensures
// that there is at most one writer to a connection by executing all writes
// from this goroutine.
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

			// Drain queued messages into the current write batch, as distinct frames
			n := len(c.send)
			for i := 0; i < n; i++ {
				if err := c.conn.WriteMessage(websocket.TextMessage, <-c.send); err != nil {
					return
				}
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage routes an incoming message to the appropriate handler.
func (c *Client) handleMessage(msg Message) {
	switch msg.Type {
	case MsgTypePing:
		c.sendMessage(Message{Type: MsgTypePong, Timestamp: time.Now().UnixMilli()})

	case MsgTypeUserInfo:
		c.handleUserInfo(msg)

	case MsgTypeDirect:
		c.handleDirect(msg)

	case MsgTypeJoinRoom:
		c.handleJoinRoom(msg)

	case MsgTypeLeaveRoom:
		c.handleLeaveRoom(msg)

	case MsgTypeRoomMessage:
		c.handleRoomMessage(msg)

	case MsgTypeBroadcast:
		c.handleBroadcast(msg)

	default:
		c.sendMessage(NewErrorMessage("unknown message type: " + msg.Type))
	}
}

// handleUserInfo processes user info sent from FE.
func (c *Client) handleUserInfo(msg Message) {
	// Parse the payload into UserInfo
	payloadBytes, err := json.Marshal(msg.Payload)
	if err != nil {
		c.sendMessage(NewErrorMessage("invalid user_info payload"))
		return
	}

	var info UserInfo
	if err := json.Unmarshal(payloadBytes, &info); err != nil {
		c.sendMessage(NewErrorMessage("invalid user_info format"))
		return
	}

	info.UserID = c.userID
	c.info = &info

	log.Printf("[client:%s] user info updated: %s (status: %s)", c.userID, info.DisplayName, info.Status)

	// Acknowledge
	c.sendMessage(NewAckMessage(msg.ID))

	// Broadcast presence update to all connected clients
	c.hub.broadcastMessage(Message{
		Type:      MsgTypePresence,
		UserID:    c.userID,
		Payload:   info,
		Timestamp: time.Now().UnixMilli(),
	})
}

// handleDirect sends a 1:1 message to a specific user.
func (c *Client) handleDirect(msg Message) {
	if msg.TargetID == "" {
		c.sendMessage(NewErrorMessage("target_id is required for direct messages"))
		return
	}

	msg.UserID = c.userID
	msg.Timestamp = time.Now().UnixMilli()

	if !c.hub.SendToUser(msg.TargetID, msg) {
		c.sendMessage(NewErrorMessage("user " + msg.TargetID + " is not connected"))
		return
	}

	c.sendMessage(NewAckMessage(msg.ID))
}

// handleJoinRoom joins the client to a room.
func (c *Client) handleJoinRoom(msg Message) {
	if msg.Room == "" {
		c.sendMessage(NewErrorMessage("room name is required"))
		return
	}

	c.rooms[msg.Room] = true
	c.hub.JoinRoom(msg.Room, c)

	log.Printf("[client:%s] joined room: %s", c.userID, msg.Room)

	c.sendMessage(NewAckMessage(msg.ID))

	// Notify room members
	c.hub.BroadcastToRoom(msg.Room, Message{
		Type:      MsgTypeUserJoined,
		UserID:    c.userID,
		Room:      msg.Room,
		Payload:   map[string]string{"user_id": c.userID},
		Timestamp: time.Now().UnixMilli(),
	}, c.userID)
}

// handleLeaveRoom removes the client from a room.
func (c *Client) handleLeaveRoom(msg Message) {
	if msg.Room == "" {
		c.sendMessage(NewErrorMessage("room name is required"))
		return
	}

	delete(c.rooms, msg.Room)
	c.hub.LeaveRoom(msg.Room, c)

	log.Printf("[client:%s] left room: %s", c.userID, msg.Room)

	c.sendMessage(NewAckMessage(msg.ID))

	// Notify room members
	c.hub.BroadcastToRoom(msg.Room, Message{
		Type:      MsgTypeUserLeft,
		UserID:    c.userID,
		Room:      msg.Room,
		Payload:   map[string]string{"user_id": c.userID},
		Timestamp: time.Now().UnixMilli(),
	}, c.userID)
}

// handleRoomMessage sends a message to all members of a room.
func (c *Client) handleRoomMessage(msg Message) {
	if msg.Room == "" {
		c.sendMessage(NewErrorMessage("room name is required"))
		return
	}

	if !c.rooms[msg.Room] {
		c.sendMessage(NewErrorMessage("you are not a member of room: " + msg.Room))
		return
	}

	msg.UserID = c.userID
	msg.Timestamp = time.Now().UnixMilli()

	c.hub.BroadcastToRoom(msg.Room, msg, "") // include sender

	c.sendMessage(NewAckMessage(msg.ID))
}

// handleBroadcast broadcasts a message to all connected clients.
func (c *Client) handleBroadcast(msg Message) {
	msg.UserID = c.userID
	msg.Timestamp = time.Now().UnixMilli()

	c.hub.broadcastMessage(msg)
}

// sendMessage marshals and sends a message to this client.
func (c *Client) sendMessage(msg Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[client:%s] marshal error: %v", c.userID, err)
		return
	}

	select {
	case c.send <- data:
	default:
		// Client send buffer is full — slow client, drop the message.
		log.Printf("[client:%s] send buffer full, dropping message", c.userID)
	}
}
