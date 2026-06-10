package valkey

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strings"
	"time"

	"ws-go/internal/hub"
	"ws-go/internal/security"

	valkeylib "github.com/valkey-io/valkey-go"
)

// Channel prefixes for Valkey pub/sub.
const (
	ChannelBroadcast = "synchub:broadcast"
	ChannelRoom      = "synchub:room:"
	ChannelDirect    = "synchub:direct:"
)

// wrappedMessage wraps a message with origin node info to prevent echo.
type wrappedMessage struct {
	NodeID  string      `json:"node_id"`
	Message hub.Message `json:"message"`
}

// Bridge handles Valkey pub/sub for cross-node message delivery.
type Bridge struct {
	hub     *hub.Hub
	enabled bool
	client  valkeylib.Client
	nodeID  string
}

// NewBridge creates a new Valkey pub/sub bridge.
func NewBridge(h *hub.Hub) *Bridge {
	valkeyURL := os.Getenv("VALKEY_URL")

	nodeID, _ := security.GenerateRandomKey(8)

	vb := &Bridge{
		hub:     h,
		enabled: valkeyURL != "",
		nodeID:  nodeID,
	}

	if !vb.enabled {
		log.Println("[valkey] VALKEY_URL not set — running in standalone mode (no cross-node messaging)")
		return vb
	}

	log.Printf("[valkey] connecting to %s (node: %s)", valkeyURL, nodeID)

	addresses := strings.Split(valkeyURL, ",")
	for i := range addresses {
		addresses[i] = strings.TrimSpace(addresses[i])
	}

	client, err := valkeylib.NewClient(valkeylib.ClientOption{
		InitAddress: addresses,
	})
	if err != nil {
		log.Printf("[valkey] connection failed: %v — falling back to standalone", err)
		vb.enabled = false
		return vb
	}

	vb.client = client
	log.Println("[valkey] connected successfully")
	return vb
}

// Subscribe starts listening for messages from Valkey channels.
func (vb *Bridge) Subscribe(ctx context.Context) {
	if !vb.enabled || vb.client == nil {
		return
	}

	log.Println("[valkey] subscribing to channels...")

	go vb.subscribeChannel(ctx, ChannelBroadcast)
	go vb.subscribePatterned(ctx, ChannelRoom+"*")
	go vb.subscribePatterned(ctx, ChannelDirect+"*")

	<-ctx.Done()
	log.Println("[valkey] subscription stopped")
}

func (vb *Bridge) subscribeChannel(ctx context.Context, channel string) {
	err := vb.client.Receive(ctx, vb.client.B().Subscribe().Channel(channel).Build(),
		func(msg valkeylib.PubSubMessage) {
			vb.handleIncomingMessage(msg.Channel, msg.Message)
		},
	)
	if err != nil && ctx.Err() == nil {
		log.Printf("[valkey] subscribe error for %s: %v", channel, err)
	}
}

func (vb *Bridge) subscribePatterned(ctx context.Context, pattern string) {
	err := vb.client.Receive(ctx, vb.client.B().Psubscribe().Pattern(pattern).Build(),
		func(msg valkeylib.PubSubMessage) {
			vb.handleIncomingMessage(msg.Channel, msg.Message)
		},
	)
	if err != nil && ctx.Err() == nil {
		log.Printf("[valkey] psubscribe error for %s: %v", pattern, err)
	}
}

// PublishBroadcast publishes a broadcast message to all nodes.
func (vb *Bridge) PublishBroadcast(msg hub.Message) {
	if !vb.enabled || vb.client == nil {
		return
	}
	vb.publish(ChannelBroadcast, msg)
}

// PublishRoom publishes a room message to all nodes.
func (vb *Bridge) PublishRoom(room string, msg hub.Message) {
	if !vb.enabled || vb.client == nil {
		return
	}
	vb.publish(ChannelRoom+room, msg)
}

// PublishDirect publishes a direct message for a specific user.
func (vb *Bridge) PublishDirect(userID string, msg hub.Message) {
	if !vb.enabled || vb.client == nil {
		return
	}
	vb.publish(ChannelDirect+userID, msg)
}

func (vb *Bridge) publish(channel string, msg hub.Message) {
	wrapped := wrappedMessage{
		NodeID:  vb.nodeID,
		Message: msg,
	}

	data, err := json.Marshal(wrapped)
	if err != nil {
		log.Printf("[valkey] marshal error: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = vb.client.Do(ctx, vb.client.B().Publish().Channel(channel).Message(string(data)).Build()).Error()
	if err != nil {
		log.Printf("[valkey] publish error to %s: %v", channel, err)
	}
}

func (vb *Bridge) handleIncomingMessage(channel string, data string) {
	var wrapped wrappedMessage
	if err := json.Unmarshal([]byte(data), &wrapped); err != nil {
		log.Printf("[valkey] unmarshal error: %v", err)
		return
	}

	if wrapped.NodeID == vb.nodeID {
		return
	}

	msg := wrapped.Message

	switch {
	case channel == ChannelBroadcast:
		msgBytes, _ := json.Marshal(msg)
		vb.hub.Mu.RLock()
		for _, client := range vb.hub.Clients {
			select {
			case client.Send <- msgBytes:
			default:
			}
		}
		vb.hub.Mu.RUnlock()

	case strings.HasPrefix(channel, ChannelRoom):
		room := channel[len(ChannelRoom):]
		msgBytes, _ := json.Marshal(msg)
		vb.hub.Mu.RLock()
		if members, ok := vb.hub.Rooms[room]; ok {
			for client := range members {
				select {
				case client.Send <- msgBytes:
				default:
				}
			}
		}
		vb.hub.Mu.RUnlock()

	case strings.HasPrefix(channel, ChannelDirect):
		userID := channel[len(ChannelDirect):]
		vb.hub.Mu.RLock()
		if client, ok := vb.hub.Clients[userID]; ok {
			client.SendMessage(msg)
		}
		vb.hub.Mu.RUnlock()
	}
}

// HealthCheck verifies the Valkey connection is alive.
func (vb *Bridge) HealthCheck() bool {
	if !vb.enabled || vb.client == nil {
		return true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp := vb.client.Do(ctx, vb.client.B().Ping().Build())
	return resp.Error() == nil
}

// Stats returns Valkey bridge statistics.
func (vb *Bridge) Stats() map[string]interface{} {
	return map[string]interface{}{
		"enabled": vb.enabled,
		"healthy": vb.HealthCheck(),
		"node_id": vb.nodeID,
	}
}

// Close cleanly shuts down the Valkey connection.
func (vb *Bridge) Close() {
	if vb.client != nil {
		vb.client.Close()
	}
}
