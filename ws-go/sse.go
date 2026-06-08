package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// SSEClient represents a Server-Sent Events subscriber.
type SSEClient struct {
	channel string
	apiKey  string
	events  chan []byte
	done    chan struct{}
}

// SSEBroker manages SSE subscriptions per channel.
type SSEBroker struct {
	mu      sync.RWMutex
	clients map[string]map[*SSEClient]bool // channel -> set of SSEClients
}

// NewSSEBroker creates a new SSE broker.
func NewSSEBroker() *SSEBroker {
	return &SSEBroker{
		clients: make(map[string]map[*SSEClient]bool),
	}
}

// Subscribe adds a new SSE client to a channel.
func (b *SSEBroker) Subscribe(channel, apiKey string) *SSEClient {
	client := &SSEClient{
		channel: channel,
		apiKey:  apiKey,
		events:  make(chan []byte, 64),
		done:    make(chan struct{}),
	}

	b.mu.Lock()
	if b.clients[channel] == nil {
		b.clients[channel] = make(map[*SSEClient]bool)
	}
	b.clients[channel][client] = true
	count := len(b.clients[channel])
	b.mu.Unlock()

	log.Printf("[sse] subscribed to channel '%s' (subscribers: %d)", channel, count)
	return client
}

// Unsubscribe removes an SSE client from its channel.
func (b *SSEBroker) Unsubscribe(client *SSEClient) {
	b.mu.Lock()
	if subs, ok := b.clients[client.channel]; ok {
		delete(subs, client)
		if len(subs) == 0 {
			delete(b.clients, client.channel)
		}
	}
	b.mu.Unlock()

	close(client.done)
	log.Printf("[sse] unsubscribed from channel '%s'", client.channel)
}

// DisconnectKey drops all SSE clients that are using the specified API key.
func (b *SSEBroker) DisconnectKey(apiKey string) int {
	b.mu.Lock()
	defer b.mu.Unlock()

	count := 0
	for _, subs := range b.clients {
		for client := range subs {
			if client.apiKey == apiKey {
				close(client.done) // signals handler to exit
				delete(subs, client)
				count++
			}
		}
	}
	if count > 0 {
		log.Printf("[sse] dropped %d subscribers for revoked key: %s...", count, apiKey[:8])
	}
	return count
}

// Publish sends a message to all SSE subscribers of a channel.
func (b *SSEBroker) Publish(channel string, msg Message) int {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[sse] marshal error: %v", err)
		return 0
	}

	b.mu.RLock()
	subs, ok := b.clients[channel]
	if !ok {
		b.mu.RUnlock()
		return 0
	}

	delivered := 0
	for client := range subs {
		select {
		case client.events <- data:
			delivered++
		default:
			// Slow consumer — drop message
			log.Printf("[sse] slow subscriber on channel '%s', dropping", channel)
		}
	}
	b.mu.RUnlock()

	return delivered
}

// SubscriberCount returns the number of SSE subscribers for a channel.
func (b *SSEBroker) SubscriberCount(channel string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients[channel])
}

// TotalSubscribers returns total SSE subscribers across all channels.
func (b *SSEBroker) TotalSubscribers() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	total := 0
	for _, subs := range b.clients {
		total += len(subs)
	}
	return total
}

// ServeSSE is the HTTP handler for SSE subscriptions.
// GET /subscribe?channel=my-channel&api_key=xxx
func ServeSSE(broker *SSEBroker, apiKeys *APIKeyStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Validate API key
		apiKey := r.URL.Query().Get("api_key")
		if apiKey == "" {
			apiKey = r.Header.Get("X-API-Key")
		}
		if apiKey == "" {
			http.Error(w, `{"error":"api_key required"}`, http.StatusUnauthorized)
			return
		}

		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = r.Header.Get("Referer")
		}

		if _, err := apiKeys.Validate(apiKey, origin); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusForbidden)
			return
		}

		// Get channel
		channel := r.URL.Query().Get("channel")
		if channel == "" {
			http.Error(w, `{"error":"channel query param required"}`, http.StatusBadRequest)
			return
		}

		// Check if client supports SSE (flushing)
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, `{"error":"SSE not supported"}`, http.StatusInternalServerError)
			return
		}

		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

		// Subscribe
		client := broker.Subscribe(channel, apiKey)
		defer broker.Unsubscribe(client)

		// Send initial connection event
		fmt.Fprintf(w, "event: connected\ndata: {\"channel\":\"%s\",\"timestamp\":%d}\n\n", channel, time.Now().UnixMilli())
		flusher.Flush()

		// Stream events with keep-alive
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case <-client.done:
				// Forcefully disconnected (e.g., API key revoked)
				return
			case data := <-client.events:
				fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
				flusher.Flush()
			case <-ticker.C:
				fmt.Fprintf(w, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	}
}
