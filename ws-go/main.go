package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true // Origin validated at API key level
	},
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Initialize stores
	apiKeyStore := NewAPIKeyStore("api_keys.json")
	hub := NewHub()
	sseBroker := NewSSEBroker()
	go hub.Run()

	// Initialize Valkey bridge
	valkeyBridge := NewValkeyBridge(hub)
	hub.SetValkey(valkeyBridge)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go valkeyBridge.Subscribe(ctx)

	rateLimiter := NewRateLimiter(100, time.Minute)

	mux := http.NewServeMux()

	// ─── API Key Management ───────────────────────────────────
	// POST /api/keys — Create a new API key
	mux.HandleFunc("/api/keys", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			var req struct {
				Name    string   `json:"name"`
				Domains []string `json:"domains"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
				return
			}
			apiKey, err := apiKeyStore.GenerateKey(req.Name, req.Domains)
			if err != nil {
				http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
				return
			}
			json.NewEncoder(w).Encode(apiKey)

		case http.MethodGet:
			json.NewEncoder(w).Encode(apiKeyStore.List())

		default:
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		}
	})

	// DELETE /api/keys/revoke?key=xxx
	mux.HandleFunc("/api/keys/revoke", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		key := r.URL.Query().Get("key")
		if key == "" {
			http.Error(w, `{"error":"key param required"}`, http.StatusBadRequest)
			return
		}
		if apiKeyStore.Revoke(key) {
			wsDropped := hub.DisconnectKey(key)
			sseDropped := sseBroker.DisconnectKey(key)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":      "revoked",
				"ws_dropped":  wsDropped,
				"sse_dropped": sseDropped,
			})
		} else {
			http.Error(w, `{"error":"key not found"}`, http.StatusNotFound)
		}
	})

	// ─── Publish (BE -> channel) ──────────────────────────────
	// POST /publish — Publish a message to a channel
	mux.HandleFunc("/publish", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"POST only"}`, http.StatusMethodNotAllowed)
			return
		}

		// Validate API key
		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" {
			apiKey = r.URL.Query().Get("api_key")
		}
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = r.Header.Get("Referer")
		}

		if _, err := apiKeyStore.Validate(apiKey, origin); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusForbidden)
			return
		}

		// Parse message
		var req struct {
			Channel string      `json:"channel"`
			Event   string      `json:"event"`
			Data    interface{} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
			return
		}
		if req.Channel == "" {
			http.Error(w, `{"error":"channel required"}`, http.StatusBadRequest)
			return
		}
		if req.Event == "" {
			req.Event = "message"
		}

		msg := Message{
			Type:      req.Event,
			Room:      req.Channel,
			Payload:   req.Data,
			Timestamp: time.Now().UnixMilli(),
		}

		// Deliver to WebSocket subscribers
		wsCount := 0
		hub.mu.RLock()
		if members, ok := hub.rooms[req.Channel]; ok {
			data, _ := json.Marshal(msg)
			for client := range members {
				select {
				case client.send <- data:
					wsCount++
				default:
				}
			}
		}
		hub.mu.RUnlock()

		// Deliver to SSE subscribers
		sseCount := sseBroker.Publish(req.Channel, msg)

		log.Printf("[publish] channel=%s event=%s ws=%d sse=%d", req.Channel, req.Event, wsCount, sseCount)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":         "published",
			"channel":        req.Channel,
			"ws_delivered":   wsCount,
			"sse_delivered":  sseCount,
		})
	})

	// ─── SSE Subscribe ────────────────────────────────────────
	// GET /subscribe?channel=xxx&api_key=xxx
	mux.HandleFunc("/subscribe", ServeSSE(sseBroker, apiKeyStore))

	// ─── WebSocket ────────────────────────────────────────────
	// GET /ws?api_key=xxx&channel=xxx
	mux.HandleFunc("/ws", RateLimitMiddleware(rateLimiter, func(w http.ResponseWriter, r *http.Request) {
		serveWS(hub, apiKeyStore, w, r)
	}))

	// ─── Health / Stats ───────────────────────────────────────
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":          "ok",
			"ws_connections":  hub.ClientCount(),
			"ws_channels":     hub.RoomCount(),
			"sse_subscribers": sseBroker.TotalSubscribers(),
			"valkey":          valkeyBridge.Stats(),
			"timestamp":       time.Now().UnixMilli(),
		})
	})

	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"online_users":    hub.GetOnlineUsers(),
			"ws_connections":  hub.ClientCount(),
			"ws_channels":     hub.RoomCount(),
			"sse_subscribers": sseBroker.TotalSubscribers(),
			"timestamp":       time.Now().UnixMilli(),
		})
	})

	// Middleware stack
	handler := LoggingMiddleware(CORSMiddleware(mux))

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // SSE needs no write timeout
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		log.Println("[server] shutting down...")
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		server.Shutdown(shutdownCtx)
	}()

	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║      ⚡ WebSocket Message Broker ⚡           ║")
	fmt.Println("╠═══════════════════════════════════════════════╣")
	fmt.Printf("║  WebSocket : ws://localhost:%s/ws             ║\n", port)
	fmt.Printf("║  SSE       : http://localhost:%s/subscribe    ║\n", port)
	fmt.Printf("║  Publish   : http://localhost:%s/publish      ║\n", port)
	fmt.Printf("║  API Keys  : http://localhost:%s/api/keys     ║\n", port)
	fmt.Printf("║  Health    : http://localhost:%s/health        ║\n", port)
	fmt.Println("╚═══════════════════════════════════════════════╝")

	log.Printf("[server] listening on :%s", port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("[server] fatal: %v", err)
	}
	log.Println("[server] stopped")
}

// serveWS handles WebSocket upgrade with API key validation.
func serveWS(hub *Hub, apiKeys *APIKeyStore, w http.ResponseWriter, r *http.Request) {
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

	// Channel from query (auto-join on connect)
	channel := r.URL.Query().Get("channel")

	// Client ID — use token or generate one
	clientID := r.URL.Query().Get("client_id")
	if clientID == "" {
		clientID = fmt.Sprintf("anon-%d", time.Now().UnixNano())
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[server] upgrade error: %v", err)
		return
	}

	client := NewClient(hub, conn, clientID, apiKey)
	hub.register <- client

	// Auto-join channel if specified
	if channel != "" {
		client.rooms[channel] = true
		hub.JoinRoom(channel, client)
		log.Printf("[ws] client %s auto-joined channel: %s", clientID, channel)
	}

	go client.WritePump()
	go client.ReadPump()
}
