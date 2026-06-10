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

	"ws-go/internal/hub"
	"ws-go/internal/metrics"
	"ws-go/internal/middleware"
	"ws-go/internal/security"
	"ws-go/internal/transport"
	"ws-go/internal/valkey"
	"ws-go/internal/webhook"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Initialize core services
	apiKeyStore := security.NewAPIKeyStore("data/api_keys.json")
	h := hub.New()
	sseBroker := transport.NewSSEBroker()
	m := metrics.New()
	aclManager := security.NewACLManager()
	webhookManager := webhook.NewManager(m)

	// Initialize JWT auth (optional)
	jwtSecret := os.Getenv("JWT_SECRET")
	jwtIssuer := os.Getenv("JWT_ISSUER")
	if jwtIssuer == "" {
		jwtIssuer = "synchub"
	}
	jwtAuth := security.NewJWTAuth(jwtSecret, jwtIssuer)
	if jwtAuth != nil {
		log.Println("[auth] JWT authentication enabled")
	} else {
		log.Println("[auth] JWT not configured (set JWT_SECRET to enable)")
	}

	go h.Run()

	// Initialize Valkey bridge
	valkeyBridge := valkey.NewBridge(h)
	h.SetValkey(valkeyBridge)
	h.SetSSE(sseBroker)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go valkeyBridge.Subscribe(ctx)

	// Rate limiters
	globalLimiter := middleware.NewRateLimiter(100, time.Minute)
	publishLimiter := middleware.NewRateLimiter(200, time.Minute)
	keyLimiter := middleware.NewKeyRateLimiter(1000, time.Minute)

	mux := http.NewServeMux()

	// ─── API Key Management ───────────────────────────────────
	mux.HandleFunc("/api/keys", middleware.RateLimitMiddleware(globalLimiter, m, func(w http.ResponseWriter, r *http.Request) {
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
	}))

	// PUT /api/keys/update?key=xxx
	mux.HandleFunc("/api/keys/update", middleware.RateLimitMiddleware(globalLimiter, m, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPut && r.Method != http.MethodPatch {
			http.Error(w, `{"error":"PUT or PATCH only"}`, http.StatusMethodNotAllowed)
			return
		}

		key := r.URL.Query().Get("key")
		if key == "" {
			http.Error(w, `{"error":"key param required"}`, http.StatusBadRequest)
			return
		}

		var req struct {
			Name    *string  `json:"name,omitempty"`
			Domains []string `json:"domains,omitempty"`
			Active  *bool    `json:"active,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
			return
		}

		updated, err := apiKeyStore.Update(key, req.Name, req.Domains, req.Active)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(updated)
	}))

	// DELETE /api/keys/revoke?key=xxx
	mux.HandleFunc("/api/keys/revoke", middleware.RateLimitMiddleware(globalLimiter, m, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		key := r.URL.Query().Get("key")
		if key == "" {
			http.Error(w, `{"error":"key param required"}`, http.StatusBadRequest)
			return
		}
		if apiKeyStore.Revoke(key) {
			wsDropped := h.DisconnectKey(key)
			sseDropped := sseBroker.DisconnectKey(key)
			m.KeyRevocations.Add(1)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":      "revoked",
				"ws_dropped":  wsDropped,
				"sse_dropped": sseDropped,
			})
		} else {
			http.Error(w, `{"error":"key not found"}`, http.StatusNotFound)
		}
	}))

	// ─── Publish (BE -> channel) ──────────────────────────────
	mux.HandleFunc("/publish", middleware.RateLimitMiddleware(publishLimiter, m, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"POST only"}`, http.StatusMethodNotAllowed)
			return
		}

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

		if !keyLimiter.Allow(apiKey) {
			m.RateLimitHits.Add(1)
			http.Error(w, `{"error":"per-key rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}

		var req struct {
			Channel   string      `json:"channel"`
			Event     string      `json:"event"`
			Data      interface{} `json:"data"`
			Publisher string      `json:"publisher"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid JSON or payload too large (max 1MB)"}`, http.StatusBadRequest)
			return
		}
		if req.Channel == "" {
			http.Error(w, `{"error":"channel required"}`, http.StatusBadRequest)
			return
		}
		if req.Event == "" {
			req.Event = "message"
		}
		if req.Publisher == "" {
			req.Publisher = apiKeyStore.Get(apiKey).Name
		}

		if !aclManager.CanWrite(req.Channel, "") {
			http.Error(w, `{"error":"write permission denied for this channel"}`, http.StatusForbidden)
			return
		}

		msg := hub.Message{
			Type:      req.Event,
			Room:      req.Channel,
			UserID:    req.Publisher,
			Payload:   req.Data,
			Timestamp: time.Now().UnixMilli(),
		}

		// Deliver to WebSocket subscribers
		wsCount := 0
		h.Mu.RLock()
		if members, ok := h.Rooms[req.Channel]; ok {
			data, _ := json.Marshal(msg)
			for client := range members {
				select {
				case client.Send <- data:
					wsCount++
					m.WsMessagesOut.Add(1)
				default:
				}
			}
		}
		h.Mu.RUnlock()

		// Deliver to SSE subscribers
		sseCount := sseBroker.Publish(req.Channel, msg)

		m.MessagesPublished.Add(1)
		m.MessagesDelivered.Add(int64(wsCount + sseCount))

		webhookManager.Dispatch(req.Event, req.Channel, req.Data)

		log.Printf("[publish] channel=%s event=%s ws=%d sse=%d", req.Channel, req.Event, wsCount, sseCount)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":        "published",
			"channel":       req.Channel,
			"ws_delivered":  wsCount,
			"sse_delivered": sseCount,
		})
	}))

	// ─── SSE Subscribe ────────────────────────────────────────
	mux.HandleFunc("/subscribe", middleware.RateLimitMiddleware(globalLimiter, m, transport.ServeSSE(sseBroker, apiKeyStore, m)))

	// ─── WebSocket ────────────────────────────────────────────
	mux.HandleFunc("/ws", middleware.RateLimitMiddleware(globalLimiter, m, func(w http.ResponseWriter, r *http.Request) {
		transport.ServeWS(h, apiKeyStore, aclManager, jwtAuth, m, w, r)
	}))

	// ─── Channel Presence ─────────────────────────────────────
	mux.HandleFunc("/channels/", middleware.RateLimitMiddleware(globalLimiter, m, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		path := r.URL.Path[len("/channels/"):]
		if len(path) < 2 {
			http.Error(w, `{"error":"channel name required"}`, http.StatusBadRequest)
			return
		}

		suffix := "/presence"
		if len(path) > len(suffix) && path[len(path)-len(suffix):] == suffix {
			channelName := path[:len(path)-len(suffix)]

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

			members := h.GetRoomMembers(channelName)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"channel": channelName,
				"members": members,
				"count":   len(members),
			})
			return
		}

		http.Error(w, `{"error":"use /channels/{name}/presence"}`, http.StatusNotFound)
	}))

	// ─── Webhook Management ───────────────────────────────────
	mux.HandleFunc("/webhooks", middleware.RateLimitMiddleware(globalLimiter, m, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			var wh webhook.Webhook
			if err := json.NewDecoder(r.Body).Decode(&wh); err != nil {
				http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
				return
			}
			if wh.URL == "" {
				http.Error(w, `{"error":"url required"}`, http.StatusBadRequest)
				return
			}
			if wh.ID == "" {
				id, _ := security.GenerateRandomKey(16)
				wh.ID = id
			}
			webhookManager.Register(&wh)
			json.NewEncoder(w).Encode(wh)

		case http.MethodGet:
			json.NewEncoder(w).Encode(webhookManager.List())

		case http.MethodDelete:
			id := r.URL.Query().Get("id")
			if id == "" {
				http.Error(w, `{"error":"id param required"}`, http.StatusBadRequest)
				return
			}
			if webhookManager.Unregister(id) {
				json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
			} else {
				http.Error(w, `{"error":"webhook not found"}`, http.StatusNotFound)
			}

		default:
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		}
	}))

	// ─── ACL Management ───────────────────────────────────────
	mux.HandleFunc("/acl", middleware.RateLimitMiddleware(globalLimiter, m, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			var acl security.ChannelACL
			if err := json.NewDecoder(r.Body).Decode(&acl); err != nil {
				http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
				return
			}
			if acl.Channel == "" {
				http.Error(w, `{"error":"channel required"}`, http.StatusBadRequest)
				return
			}
			aclManager.SetChannelACL(&acl)
			json.NewEncoder(w).Encode(map[string]interface{}{"status": "set", "channel": acl.Channel})

		case http.MethodGet:
			channel := r.URL.Query().Get("channel")
			if channel != "" {
				acl := aclManager.GetChannelACL(channel)
				if acl == nil {
					json.NewEncoder(w).Encode(map[string]interface{}{"channel": channel, "public": true})
				} else {
					json.NewEncoder(w).Encode(acl)
				}
			} else {
				json.NewEncoder(w).Encode(aclManager.ListACLs())
			}

		case http.MethodDelete:
			channel := r.URL.Query().Get("channel")
			if channel == "" {
				http.Error(w, `{"error":"channel param required"}`, http.StatusBadRequest)
				return
			}
			if aclManager.RemoveChannelACL(channel) {
				json.NewEncoder(w).Encode(map[string]string{"status": "removed"})
			} else {
				http.Error(w, `{"error":"no ACL for channel"}`, http.StatusNotFound)
			}

		default:
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		}
	}))

	// ─── JWT Token Generation ─────────────────────────────────
	mux.HandleFunc("/auth/token", middleware.RateLimitMiddleware(globalLimiter, m, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"POST only"}`, http.StatusMethodNotAllowed)
			return
		}

		if jwtAuth == nil {
			http.Error(w, `{"error":"JWT auth not configured (set JWT_SECRET env var)"}`, http.StatusServiceUnavailable)
			return
		}

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

		var req struct {
			UserID      string            `json:"user_id"`
			DisplayName string            `json:"display_name"`
			Roles       []string          `json:"roles"`
			Channels    []string          `json:"channels"`
			Permissions map[string]string `json:"permissions"`
			TTL         int               `json:"ttl_seconds"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
			return
		}
		if req.UserID == "" {
			http.Error(w, `{"error":"user_id required"}`, http.StatusBadRequest)
			return
		}

		ttl := 24 * time.Hour
		if req.TTL > 0 {
			ttl = time.Duration(req.TTL) * time.Second
		}

		token, err := jwtAuth.GenerateToken(req.UserID, req.DisplayName, req.Roles, req.Channels, req.Permissions, ttl)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"token":      token,
			"expires_in": int(ttl.Seconds()),
			"user_id":    req.UserID,
		})
	}))

	// ─── Health / Stats / Metrics ─────────────────────────────
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":          "ok",
			"ws_connections":  h.ClientCount(),
			"ws_channels":     h.RoomCount(),
			"sse_subscribers": sseBroker.TotalSubscribers(),
			"valkey":          valkeyBridge.Stats(),
			"jwt_enabled":     jwtAuth != nil,
			"timestamp":       time.Now().UnixMilli(),
		})
	})

	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"online_users":    h.GetOnlineUsers(),
			"ws_connections":  h.ClientCount(),
			"ws_channels":     h.RoomCount(),
			"ws_rooms":        h.GetRooms(),
			"sse_subscribers": sseBroker.TotalSubscribers(),
			"timestamp":       time.Now().UnixMilli(),
		})
	})

	mux.HandleFunc("/metrics", m.ServeMetrics())

	// Serve client UI
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, "../client/index.html")
	})

	// Middleware stack
	handler := middleware.LoggingMiddleware(middleware.CORSMiddleware(mux))

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		log.Println("[server] shutting down gracefully...")
		cancel()
		valkeyBridge.Close()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		server.Shutdown(shutdownCtx)
	}()

	fmt.Println("╔════════════════════════════════════════════════════════╗")
	fmt.Println("║            ⚡ SyncHub Message Broker ⚡                ║")
	fmt.Println("╠════════════════════════════════════════════════════════╣")
	fmt.Printf("║  WebSocket  : ws://localhost:%s/ws                   ║\n", port)
	fmt.Printf("║  SSE        : http://localhost:%s/subscribe          ║\n", port)
	fmt.Printf("║  Publish    : http://localhost:%s/publish            ║\n", port)
	fmt.Printf("║  API Keys   : http://localhost:%s/api/keys           ║\n", port)
	fmt.Printf("║  Presence   : http://localhost:%s/channels/*/presence║\n", port)
	fmt.Printf("║  Webhooks   : http://localhost:%s/webhooks           ║\n", port)
	fmt.Printf("║  ACL        : http://localhost:%s/acl                ║\n", port)
	fmt.Printf("║  Auth Token : http://localhost:%s/auth/token         ║\n", port)
	fmt.Printf("║  Health     : http://localhost:%s/health             ║\n", port)
	fmt.Printf("║  Metrics    : http://localhost:%s/metrics            ║\n", port)
	fmt.Println("╚════════════════════════════════════════════════════════╝")

	log.Printf("[server] listening on :%s", port)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("[server] fatal: %v", err)
	}
	log.Println("[server] stopped")
}
