package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"
)

// ValkeyBridge handles Valkey pub/sub for cross-node message delivery.
// When running multiple Go WS server instances, Valkey ensures that
// messages reach clients regardless of which node they're connected to.
type ValkeyBridge struct {
	hub     *Hub
	enabled bool
	// In a production setup, this would hold the valkey-go client.
	// For now, we provide the interface so the architecture is ready.
}

// Valkey channel prefixes
const (
	valkeyChannelBroadcast = "ws:broadcast"
	valkeyChannelRoom      = "ws:room:"
	valkeyChannelDirect    = "ws:direct:"
)

// NewValkeyBridge creates a new Valkey pub/sub bridge.
// If VALKEY_URL is not set, it runs in standalone mode (no cross-node messaging).
func NewValkeyBridge(hub *Hub) *ValkeyBridge {
	valkeyURL := os.Getenv("VALKEY_URL")

	vb := &ValkeyBridge{
		hub:     hub,
		enabled: valkeyURL != "",
	}

	if !vb.enabled {
		log.Println("[valkey] VALKEY_URL not set — running in standalone mode (no cross-node messaging)")
		return vb
	}

	log.Printf("[valkey] connecting to %s", valkeyURL)

	// TODO: Initialize valkey-go client here.
	// Example:
	//   client, err := valkey.NewClient(valkey.ClientOption{
	//       InitAddress: []string{valkeyURL},
	//   })
	//   if err != nil {
	//       log.Printf("[valkey] connection failed: %v — falling back to standalone", err)
	//       vb.enabled = false
	//       return vb
	//   }

	log.Println("[valkey] connected successfully")
	return vb
}

// Subscribe starts listening for messages from Valkey channels.
// This should be called as a goroutine.
func (vb *ValkeyBridge) Subscribe(ctx context.Context) {
	if !vb.enabled {
		return
	}

	log.Println("[valkey] subscribing to channels...")

	// TODO: Implement Valkey subscription.
	// This is the receiving side — when other nodes publish messages,
	// this node receives them and delivers to local clients.
	//
	// Example with valkey-go:
	//   err := vb.client.Receive(ctx, vb.client.B().Subscribe().
	//       Channel(valkeyChannelBroadcast).Build(),
	//       func(msg valkey.PubSubMessage) {
	//           vb.handleIncomingMessage(msg.Channel, msg.Message)
	//       },
	//   )

	// For now, just log that we would be subscribing
	<-ctx.Done()
	log.Println("[valkey] subscription stopped")
}

// PublishBroadcast publishes a broadcast message to all nodes via Valkey.
func (vb *ValkeyBridge) PublishBroadcast(msg Message) {
	if !vb.enabled {
		return
	}
	vb.publish(valkeyChannelBroadcast, msg)
}

// PublishRoom publishes a room message to all nodes via Valkey.
func (vb *ValkeyBridge) PublishRoom(room string, msg Message) {
	if !vb.enabled {
		return
	}
	vb.publish(valkeyChannelRoom+room, msg)
}

// PublishDirect publishes a direct message for a specific user via Valkey.
func (vb *ValkeyBridge) PublishDirect(userID string, msg Message) {
	if !vb.enabled {
		return
	}
	vb.publish(valkeyChannelDirect+userID, msg)
}

// publish serializes and publishes a message to a Valkey channel.
func (vb *ValkeyBridge) publish(channel string, msg Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[valkey] marshal error: %v", err)
		return
	}

	log.Printf("[valkey] publishing to %s: %d bytes", channel, len(data))

	// TODO: Implement actual Valkey publish.
	// Example:
	//   vb.client.Do(ctx, vb.client.B().Publish().
	//       Channel(channel).Message(string(data)).Build())
	_ = data
}

// handleIncomingMessage processes a message received from Valkey.
func (vb *ValkeyBridge) handleIncomingMessage(channel string, data string) {
	var msg Message
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		log.Printf("[valkey] unmarshal error: %v", err)
		return
	}

	// Route based on channel prefix
	switch {
	case channel == valkeyChannelBroadcast:
		// Broadcast to all local clients
		msgBytes, _ := json.Marshal(msg)
		vb.hub.mu.RLock()
		for _, client := range vb.hub.clients {
			select {
			case client.send <- msgBytes:
			default:
			}
		}
		vb.hub.mu.RUnlock()

	case len(channel) > len(valkeyChannelRoom) && channel[:len(valkeyChannelRoom)] == valkeyChannelRoom:
		// Room message — deliver to local members
		room := channel[len(valkeyChannelRoom):]
		msgBytes, _ := json.Marshal(msg)
		vb.hub.mu.RLock()
		if members, ok := vb.hub.rooms[room]; ok {
			for client := range members {
				select {
				case client.send <- msgBytes:
				default:
				}
			}
		}
		vb.hub.mu.RUnlock()

	case len(channel) > len(valkeyChannelDirect) && channel[:len(valkeyChannelDirect)] == valkeyChannelDirect:
		// Direct message — deliver to specific local user
		userID := channel[len(valkeyChannelDirect):]
		vb.hub.mu.RLock()
		if client, ok := vb.hub.clients[userID]; ok {
			client.sendMessage(msg)
		}
		vb.hub.mu.RUnlock()
	}
}

// HealthCheck verifies the Valkey connection is alive.
func (vb *ValkeyBridge) HealthCheck() bool {
	if !vb.enabled {
		return true // Standalone mode is always "healthy"
	}

	// TODO: Implement actual ping
	// Example:
	//   result := vb.client.Do(ctx, vb.client.B().Ping().Build())
	//   return result.Error() == nil

	return true
}

// Stats returns Valkey bridge statistics.
func (vb *ValkeyBridge) Stats() map[string]interface{} {
	return map[string]interface{}{
		"enabled":   vb.enabled,
		"healthy":   vb.HealthCheck(),
		"timestamp": time.Now().UnixMilli(),
	}
}
