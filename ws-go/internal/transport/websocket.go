package transport

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"ws-go/internal/hub"
	"ws-go/internal/metrics"
	"ws-go/internal/security"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// ServeWS handles WebSocket upgrade with API key + optional JWT validation.
func ServeWS(h *hub.Hub, apiKeys *security.APIKeyStore, aclManager *security.ACLManager, jwtAuth *security.JWTAuth, m *metrics.Metrics, w http.ResponseWriter, r *http.Request) {
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
	clientID := r.URL.Query().Get("client_id")

	var jwtClaims *security.JWTClaims
	if jwtAuth != nil {
		token := r.URL.Query().Get("token")
		if token == "" {
			token = r.Header.Get("Authorization")
		}
		if token != "" {
			claims, err := jwtAuth.ValidateToken(token)
			if err != nil {
				http.Error(w, fmt.Sprintf(`{"error":"invalid token: %s"}`, err.Error()), http.StatusUnauthorized)
				return
			}
			jwtClaims = claims
			clientID = claims.UserID

			if channel != "" && !jwtAuth.HasChannelAccess(claims, channel, "read") {
				http.Error(w, `{"error":"channel access denied by token"}`, http.StatusForbidden)
				return
			}
		}
	}

	if clientID == "" {
		clientID = fmt.Sprintf("anon-%d", time.Now().UnixNano())
	}

	if channel != "" && !aclManager.CanJoin(channel, apiKey, clientID) {
		http.Error(w, `{"error":"channel access denied by ACL"}`, http.StatusForbidden)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[server] upgrade error: %v", err)
		return
	}

	client := hub.NewClient(h, conn, clientID, apiKey)
	client.JWTClaims = jwtClaims
	client.ACLManager = aclManager
	client.Metrics = m
	h.Register <- client

	m.WsConnections.Add(1)

	if channel != "" {
		client.Rooms[channel] = true
		h.JoinRoom(channel, client)
		log.Printf("[ws] client %s auto-joined channel: %s", clientID, channel)
	}

	go client.WritePump()
	go client.ReadPump()
}
