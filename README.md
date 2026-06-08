# High-Scale WebSocket Message Broker

This repository contains two fully interoperable implementations of a high-performance, multi-tenant message broker designed to broadcast real-time messages between Backend services and Frontend clients.

*   **`ws-go`**: Written in Go (Gorilla WebSocket).
*   **`ws-elixir`**: Written in Elixir (Phoenix, Plug, Bandit).

Both servers act as centralized drop-in replacements for each other. They speak the exact same wire protocols, provide the same API endpoints, and enforce the same security constraints.

## Core Features

1.  **Multi-Tenant API Keys:** All connections must be authenticated with an API key (`/api/keys`).
2.  **Origin Enforcement:** API keys can be locked down to specific frontend domains (e.g., `*.example.com`).
3.  **Active Revocation:** If an API key is deleted, all active WebSocket and SSE connections using that key are forcefully and immediately terminated.
4.  **Raw WebSocket & SSE:** Clients can use standard `new WebSocket()` or `new EventSource()` without needing heavy framework-specific libraries like `phoenix.js`.
5.  **Robust Keep-Alives:** SSE connections automatically send `: keepalive` pings every 30s to prevent reverse proxy timeouts.
6.  **Rate Limiting:** Protects the endpoints from flood attacks.

## 🛠️ Tech Stack

### Go Implementation (`ws-go`)
*   **Language:** Go 1.22+
*   **WebSocket:** [gorilla/websocket](https://github.com/gorilla/websocket)
*   **Routing:** Standard `net/http` `ServeMux`
*   **Clustering (Optional):** [valkey-go](https://github.com/valkey-io/valkey-go)

### Elixir Implementation (`ws-elixir`)
*   **Language:** Elixir 1.17+ / OTP 26+
*   **Framework:** Phoenix (Headless API mode)
*   **Server/WebSocket:** Bandit & WebSockAdapter
*   **State / PubSub:** Erlang ETS, GenServer, `Phoenix.PubSub`

---

## 🚀 Quick Start

### Start the Elixir Server (Port 4000)
```bash
docker compose up -d
```

### Start the Go Server (Port 8080)
```bash
cd ws-go
go run .
```

### Test Console
Open `client/index.html` in your browser. This UI allows you to instantly test both the Go and Elixir servers, generate keys, and stream messages.

---

## 📡 API Reference

### 1. Provisioning API Keys
Your backend calls this to generate access keys for your frontend.

**`POST /api/keys`**
```json
// Request
{
  "name": "Production App",
  "domains": ["*.my-app.com", "localhost"]
}

// Response
{
  "id": "65ce93...",
  "name": "Production App",
  "domains": ["*.my-app.com", "localhost"],
  "active": true
}
```

**`DELETE /api/keys/revoke?key=ID`**
Revokes the key and immediately forcefully drops all connected clients using it.

### 2. Frontend Connecting

**WebSocket (Bidirectional)**
```javascript
// Connect with api_key, channel to join, and a unique client_id
const ws = new WebSocket('ws://localhost:8080/ws?api_key=xxx&channel=my-room&client_id=user123');

ws.onmessage = (event) => {
    const msg = JSON.parse(event.data);
    console.log("Received:", msg);
};

// Send a message back to everyone in the room
ws.send(JSON.stringify({
    type: "room_message",
    payload: { text: "Hello everyone!" }
}));
```

**SSE (Read-Only Stream)**
```javascript
// Useful for lightweight read-only clients
const sse = new EventSource('http://localhost:8080/subscribe?api_key=xxx&channel=my-room');

sse.addEventListener('message', (event) => {
    const data = JSON.parse(event.data);
    console.log("Stream received:", data);
});
```

### 3. Backend Publishing
Your primary backend APIs should push data to the broker using this HTTP endpoint, which instantly delivers the payload to all WebSocket and SSE subscribers.

**`POST /publish`**
```bash
curl -X POST http://localhost:8080/publish \
  -H "X-API-Key: your_api_key_here" \
  -H "Content-Type: application/json" \
  -d '{
    "channel": "my-room",
    "event": "alert",
    "data": { "message": "Server maintenance in 5 minutes!" }
  }'
```

---

## Architecture details

*   **Message Format:** All socket and SSE messages use a unified JSON envelope: `{"type": "...", "payload": {}, "timestamp": 123456789}`.
*   **Scaling:** The Go implementation includes a stubbed `ValkeyBridge`. When enabled, it broadcasts messages over a Redis/Valkey cluster, allowing horizontal scaling across thousands of nodes to support millions of concurrent connections.
*   **Persistence:** Elixir API keys are persisted via Docker volumes in `./ws-elixir/data`. Go persists directly to `./ws-go/api_keys.json`.
