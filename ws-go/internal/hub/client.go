package hub

import (
	"encoding/json"
	"log"
	"time"

	"ws-go/internal/metrics"
	"ws-go/internal/security"

	"github.com/gorilla/websocket"
)

const (
	WriteWait      = 10 * time.Second
	PongWait       = 60 * time.Second
	PingPeriod     = (PongWait * 9) / 10
	MaxMessageSize = 64 * 1024
)

// Client represents a single WebSocket connection.
type Client struct {
	Hub        *Hub
	Conn       *websocket.Conn
	Send       chan []byte
	UserID     string
	APIKey     string
	Rooms      map[string]bool
	Info       *UserInfo
	JWTClaims  *security.JWTClaims
	ACLManager *security.ACLManager
	Metrics    *metrics.Metrics
}

// NewClient creates a new Client instance.
func NewClient(h *Hub, conn *websocket.Conn, userID string, apiKey string) *Client {
	return &Client{
		Hub:    h,
		Conn:   conn,
		Send:   make(chan []byte, 256),
		UserID: userID,
		APIKey: apiKey,
		Rooms:  make(map[string]bool),
	}
}

// ReadPump pumps messages from the WebSocket connection to the Hub.
func (c *Client) ReadPump() {
	defer func() {
		c.Hub.Unregister <- c
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(MaxMessageSize)
	c.Conn.SetReadDeadline(time.Now().Add(PongWait))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(PongWait))
		return nil
	})

	for {
		_, rawMsg, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseNormalClosure,
				websocket.CloseAbnormalClosure,
			) {
				log.Printf("[client:%s] read error: %v", c.UserID, err)
			}
			break
		}

		if c.Metrics != nil {
			c.Metrics.WsMessagesIn.Add(1)
		}

		var msg Message
		if err := json.Unmarshal(rawMsg, &msg); err != nil {
			c.SendMessage(NewErrorMessage("invalid message format"))
			continue
		}

		msg.UserID = c.UserID
		c.handleMessage(msg)
	}
}

// WritePump pumps messages from the Hub to the WebSocket connection.
func (c *Client) WritePump() {
	ticker := time.NewTicker(PingPeriod)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(WriteWait))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

			if c.Metrics != nil {
				c.Metrics.WsMessagesOut.Add(1)
			}

			n := len(c.Send)
			for i := 0; i < n; i++ {
				if err := c.Conn.WriteMessage(websocket.TextMessage, <-c.Send); err != nil {
					return
				}
				if c.Metrics != nil {
					c.Metrics.WsMessagesOut.Add(1)
				}
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(WriteWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) handleMessage(msg Message) {
	switch msg.Type {
	case MsgTypePing:
		c.SendMessage(Message{Type: MsgTypePong, Timestamp: time.Now().UnixMilli()})

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
		c.SendMessage(NewErrorMessage("unknown message type: " + msg.Type))
	}
}

func (c *Client) handleUserInfo(msg Message) {
	payloadBytes, err := json.Marshal(msg.Payload)
	if err != nil {
		c.SendMessage(NewErrorMessage("invalid user_info payload"))
		return
	}

	var info UserInfo
	if err := json.Unmarshal(payloadBytes, &info); err != nil {
		c.SendMessage(NewErrorMessage("invalid user_info format"))
		return
	}

	info.UserID = c.UserID
	c.Info = &info

	log.Printf("[client:%s] user info updated: %s (status: %s)", c.UserID, info.DisplayName, info.Status)

	c.SendMessage(NewAckMessage(msg.ID))

	c.Hub.BroadcastMessage(Message{
		Type:      MsgTypePresence,
		UserID:    c.UserID,
		Payload:   info,
		Timestamp: time.Now().UnixMilli(),
	})
}

func (c *Client) handleDirect(msg Message) {
	if msg.TargetID == "" {
		c.SendMessage(NewErrorMessage("target_id is required for direct messages"))
		return
	}

	msg.UserID = c.UserID
	msg.Timestamp = time.Now().UnixMilli()

	if !c.Hub.SendToUser(msg.TargetID, msg) {
		c.SendMessage(NewErrorMessage("user " + msg.TargetID + " is not connected"))
		return
	}

	c.SendMessage(NewAckMessage(msg.ID))
}

func (c *Client) handleJoinRoom(msg Message) {
	if msg.Room == "" {
		c.SendMessage(NewErrorMessage("room name is required"))
		return
	}

	if c.ACLManager != nil && !c.ACLManager.CanJoin(msg.Room, c.APIKey, c.UserID) {
		c.SendMessage(NewErrorMessage("access denied: cannot join room " + msg.Room))
		return
	}

	if c.ACLManager != nil {
		currentCount := c.Hub.RoomMemberCount(msg.Room)
		if !c.ACLManager.CheckMaxMembers(msg.Room, currentCount) {
			c.SendMessage(NewErrorMessage("room " + msg.Room + " is full"))
			return
		}
	}

	if c.JWTClaims != nil {
		jwtAuth := &security.JWTAuth{}
		if !jwtAuth.HasChannelAccess(c.JWTClaims, msg.Room, "read") {
			c.SendMessage(NewErrorMessage("token does not grant access to room " + msg.Room))
			return
		}
	}

	c.Rooms[msg.Room] = true
	c.Hub.JoinRoom(msg.Room, c)

	log.Printf("[client:%s] joined room: %s", c.UserID, msg.Room)

	c.SendMessage(NewAckMessage(msg.ID))

	c.Hub.BroadcastToRoom(msg.Room, Message{
		Type:      MsgTypeUserJoined,
		UserID:    c.UserID,
		Room:      msg.Room,
		Payload:   map[string]string{"user_id": c.UserID},
		Timestamp: time.Now().UnixMilli(),
	}, c.UserID)
}

func (c *Client) handleLeaveRoom(msg Message) {
	if msg.Room == "" {
		c.SendMessage(NewErrorMessage("room name is required"))
		return
	}

	delete(c.Rooms, msg.Room)
	c.Hub.LeaveRoom(msg.Room, c)

	log.Printf("[client:%s] left room: %s", c.UserID, msg.Room)

	c.SendMessage(NewAckMessage(msg.ID))

	c.Hub.BroadcastToRoom(msg.Room, Message{
		Type:      MsgTypeUserLeft,
		UserID:    c.UserID,
		Room:      msg.Room,
		Payload:   map[string]string{"user_id": c.UserID},
		Timestamp: time.Now().UnixMilli(),
	}, c.UserID)
}

func (c *Client) handleRoomMessage(msg Message) {
	if msg.Room == "" {
		c.SendMessage(NewErrorMessage("room name is required"))
		return
	}

	if !c.Rooms[msg.Room] {
		c.SendMessage(NewErrorMessage("you are not a member of room: " + msg.Room))
		return
	}

	if c.ACLManager != nil && !c.ACLManager.CanWrite(msg.Room, c.UserID) {
		c.SendMessage(NewErrorMessage("write permission denied for room: " + msg.Room))
		return
	}

	msg.UserID = c.UserID
	msg.Timestamp = time.Now().UnixMilli()

	c.Hub.BroadcastToRoom(msg.Room, msg, "")

	c.SendMessage(NewAckMessage(msg.ID))
}

func (c *Client) handleBroadcast(msg Message) {
	msg.UserID = c.UserID
	msg.Timestamp = time.Now().UnixMilli()

	c.Hub.BroadcastMessage(msg)
}

// SendMessage marshals and sends a message to this client.
func (c *Client) SendMessage(msg Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[client:%s] marshal error: %v", c.UserID, err)
		return
	}

	select {
	case c.Send <- data:
	default:
		log.Printf("[client:%s] send buffer full, dropping message", c.UserID)
	}
}
