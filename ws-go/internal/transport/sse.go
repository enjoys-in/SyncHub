package transport

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"ws-go/internal/hub"
	"ws-go/internal/metrics"
	"ws-go/internal/security"
)

// SSEClient represents a Server-Sent Events subscriber.
type SSEClient struct {
	Channel string
	APIKey  string
	Events  chan []byte
	Done    chan struct{}
}

// SSEBroker manages SSE subscriptions per channel with replay buffer.
type SSEBroker struct {
	mu      sync.RWMutex
	clients map[string]map[*SSEClient]bool

	replayMu    sync.RWMutex
	replayBuf   map[string][]ReplayEntry
	replayLimit int
	eventID     atomic.Uint64
}

// ReplayEntry stores a message for replay on reconnection.
type ReplayEntry struct {
	ID   uint64
	Data []byte
}

// NewSSEBroker creates a new SSE broker with replay support.
func NewSSEBroker() *SSEBroker {
	return &SSEBroker{
		clients:     make(map[string]map[*SSEClient]bool),
		replayBuf:   make(map[string][]ReplayEntry),
		replayLimit: 100,
	}
}

// Subscribe adds a new SSE client to a channel.
func (b *SSEBroker) Subscribe(channel, apiKey string) *SSEClient {
	client := &SSEClient{
		Channel: channel,
		APIKey:  apiKey,
		Events:  make(chan []byte, 64),
		Done:    make(chan struct{}),
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
	if subs, ok := b.clients[client.Channel]; ok {
		delete(subs, client)
		if len(subs) == 0 {
			delete(b.clients, client.Channel)
		}
	}
	b.mu.Unlock()

	close(client.Done)
	log.Printf("[sse] unsubscribed from channel '%s'", client.Channel)
}

// DisconnectKey drops all SSE clients using the specified API key.
func (b *SSEBroker) DisconnectKey(apiKey string) int {
	b.mu.Lock()
	defer b.mu.Unlock()

	count := 0
	for _, subs := range b.clients {
		for client := range subs {
			if client.APIKey == apiKey {
				close(client.Done)
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
func (b *SSEBroker) Publish(channel string, msg hub.Message) int {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[sse] marshal error: %v", err)
		return 0
	}

	eventID := b.eventID.Add(1)
	b.replayMu.Lock()
	buf := b.replayBuf[channel]
	if len(buf) >= b.replayLimit {
		buf = buf[1:]
	}
	b.replayBuf[channel] = append(buf, ReplayEntry{ID: eventID, Data: data})
	b.replayMu.Unlock()

	b.mu.RLock()
	subs, ok := b.clients[channel]
	if !ok {
		b.mu.RUnlock()
		return 0
	}

	delivered := 0
	for client := range subs {
		select {
		case client.Events <- data:
			delivered++
		default:
			log.Printf("[sse] slow subscriber on channel '%s', dropping", channel)
		}
	}
	b.mu.RUnlock()

	return delivered
}

// PublishAll sends a message to ALL SSE subscribers across all channels.
func (b *SSEBroker) PublishAll(msg hub.Message) int {
	data, err := json.Marshal(msg)
	if err != nil {
		return 0
	}

	b.mu.RLock()
	delivered := 0
	for _, subs := range b.clients {
		for client := range subs {
			select {
			case client.Events <- data:
				delivered++
			default:
			}
		}
	}
	b.mu.RUnlock()
	return delivered
}

// GetCurrentEventID returns the current event ID counter.
func (b *SSEBroker) GetCurrentEventID() uint64 {
	return b.eventID.Load()
}

// GetReplaySince returns all messages after the given event ID.
func (b *SSEBroker) GetReplaySince(channel string, lastEventID uint64) []ReplayEntry {
	b.replayMu.RLock()
	defer b.replayMu.RUnlock()

	buf, ok := b.replayBuf[channel]
	if !ok {
		return nil
	}

	var result []ReplayEntry
	for _, entry := range buf {
		if entry.ID > lastEventID {
			result = append(result, entry)
		}
	}
	return result
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
func ServeSSE(broker *SSEBroker, apiKeys *security.APIKeyStore, m *metrics.Metrics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

		channel := r.URL.Query().Get("channel")
		if channel == "" {
			http.Error(w, `{"error":"channel query param required"}`, http.StatusBadRequest)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, `{"error":"SSE not supported"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("X-Accel-Buffering", "no")

		client := broker.Subscribe(channel, apiKey)
		defer broker.Unsubscribe(client)

		if m != nil {
			m.SseConnections.Add(1)
			defer m.SseConnections.Add(-1)
		}

		lastEventIDStr := r.Header.Get("Last-Event-ID")
		if lastEventIDStr != "" {
			var lastID uint64
			fmt.Sscanf(lastEventIDStr, "%d", &lastID)
			if lastID > 0 {
				entries := broker.GetReplaySince(channel, lastID)
				for _, entry := range entries {
					fmt.Fprintf(w, "id: %d\nevent: message\ndata: %s\n\n", entry.ID, entry.Data)
				}
				flusher.Flush()
				log.Printf("[sse] replayed %d messages for channel '%s' from ID %d", len(entries), channel, lastID)
			}
		}

		currentID := broker.GetCurrentEventID()
		fmt.Fprintf(w, "id: %d\nevent: connected\ndata: {\"channel\":\"%s\",\"timestamp\":%d}\n\n", currentID, channel, time.Now().UnixMilli())
		flusher.Flush()

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case <-client.Done:
				return
			case data := <-client.Events:
				eventID := broker.GetCurrentEventID()
				fmt.Fprintf(w, "id: %d\nevent: message\ndata: %s\n\n", eventID, data)
				flusher.Flush()
				if m != nil {
					m.MessagesDelivered.Add(1)
				}
			case <-ticker.C:
				fmt.Fprintf(w, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	}
}
