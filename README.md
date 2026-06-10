# SyncHub — High-Scale WebSocket Message Broker

A high-performance, multi-tenant message broker designed to broadcast real-time messages between Backend services and Frontend clients. Two fully interoperable implementations:

*   **`ws-go`**: Written in Go (Gorilla WebSocket) — feature-complete with JWT, ACL, webhooks, metrics.
*   **`ws-elixir`**: Written in Elixir (Phoenix, Plug, Bandit).

Both servers act as centralized drop-in replacements for each other. They speak the exact same wire protocols, provide the same API endpoints, and enforce the same security constraints.

## Core Features

1.  **Multi-Tenant API Keys:** All connections must be authenticated with an API key (`/api/keys`).
2.  **Origin Enforcement:** API keys can be locked down to specific frontend domains (e.g., `*.example.com`).
3.  **Active Revocation:** If an API key is deleted, all active WebSocket and SSE connections using that key are forcefully and immediately terminated.
4.  **Raw WebSocket & SSE:** Clients can use standard `new WebSocket()` or `new EventSource()` without needing heavy framework-specific libraries like `phoenix.js`.
5.  **Robust Keep-Alives:** SSE connections automatically send `: keepalive` pings every 30s to prevent reverse proxy timeouts.
6.  **Rate Limiting:** Per-key and global rate limiting protects endpoints from flood attacks.
7.  **JWT Authentication:** Optional token-based auth with channel-level permissions (publish/subscribe/admin).
8.  **Channel ACL:** Fine-grained access control per channel with max member limits.
9.  **Webhooks:** Register HTTP callbacks that fire on channel events (message, join, leave).
10. **Prometheus Metrics:** `/metrics` endpoint exposes connection counts, message throughput, errors.
11. **Message Replay:** SSE clients can replay missed messages from a configurable buffer.
12. **Direct Messaging:** Send messages to specific users via `client_id` targeting.

## 🛠️ Tech Stack

### Go Implementation (`ws-go`)
*   **Language:** Go 1.25+
*   **WebSocket:** [gorilla/websocket](https://github.com/gorilla/websocket)
*   **Auth:** [golang-jwt/jwt/v5](https://github.com/golang-jwt/jwt)
*   **Routing:** Standard `net/http` `ServeMux`
*   **Clustering (Optional):** [valkey-go](https://github.com/valkey-io/valkey-go)

### Elixir Implementation (`ws-elixir`)
*   **Language:** Elixir 1.17+ / OTP 26+
*   **Framework:** Phoenix (Headless API mode)
*   **Server/WebSocket:** Bandit & WebSockAdapter
*   **State / PubSub:** Erlang ETS, GenServer, `Phoenix.PubSub`

---

## 📂 Project Structure (Go)

```
ws-go/
├── cmd/server/main.go        # Entry point & HTTP route wiring
├── internal/
│   ├── hub/                   # Client registry, message routing, WebSocket lifecycle
│   │   ├── hub.go
│   │   ├── client.go
│   │   └── message.go
│   ├── transport/             # SSE broker & WebSocket upgrade handler
│   │   ├── sse.go
│   │   └── websocket.go
│   ├── security/              # API keys, JWT, ACL
│   │   ├── apikey.go
│   │   ├── jwt.go
│   │   └── acl.go
│   ├── middleware/            # CORS, rate limiting, request logging
│   │   └── middleware.go
│   ├── metrics/               # Prometheus counters & gauges
│   │   └── metrics.go
│   ├── webhook/               # Webhook registration & async delivery
│   │   └── webhook.go
│   └── valkey/                # Redis/Valkey pub/sub bridge for clustering
│       └── valkey.go
├── data/api_keys.json         # Persisted API keys
├── go.mod
└── go.sum
```

---

## 🚀 Quick Start

### Start the Elixir Server (Port 4000)
```bash
docker compose up -d
```

### Start the Go Server (Port 8080)
```bash
cd ws-go
go run ./cmd/server
```

### Test Console
Open http://localhost:8080 in your browser — the Go server serves the test console directly. You can generate keys, connect WebSocket/SSE clients, and stream messages in real time.

---

## 📡 API Reference

### 1. Provisioning API Keys
Your backend calls this to generate access keys for your frontend.

**`POST /api/keys`** — Create a new key
```bash
curl -X POST http://localhost:8080/api/keys \
  -H "Content-Type: application/json" \
  -d '{"name": "Production App", "domains": ["*.my-app.com", "localhost"]}'
```
```json
{
  "key": "65ce930724544c3117f33baec3d5cd6e...",
  "name": "Production App",
  "allowed_domains": ["*.my-app.com", "localhost"],
  "active": true,
  "created_at": 1718000000000
}
```

**`GET /api/keys`** — List all keys (keys are partially masked)
```bash
curl http://localhost:8080/api/keys
```

**`PUT /api/keys/update?key=KEY`** — Update a key's name, domains, or active status
```bash
curl -X PUT "http://localhost:8080/api/keys/update?key=65ce9307..." \
  -H "Content-Type: application/json" \
  -d '{"name": "Renamed App", "domains": ["*.new-domain.com"], "active": true}'
```

**`DELETE /api/keys/revoke?key=KEY`** — Revoke and drop all connected clients
```bash
curl -X DELETE "http://localhost:8080/api/keys/revoke?key=65ce9307..."
# Immediately disconnects all WS and SSE clients using this key
```

### 2. JWT Token (Optional)

**`POST /auth/token`**
```json
// Request (X-API-Key header required)
{
  "user_id": "user123",
  "channels": ["room-a", "room-b"],
  "permissions": "publish"
}

// Response
{
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "expires_at": "2026-06-11T00:00:00Z"
}
```

Pass the token as `&token=xxx` query param on WebSocket/SSE connections for channel-level access control.

### 3. Frontend Connecting

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

**SSE with Message Replay** — reconnect and get missed messages:
```javascript
// After disconnect, reconnect with Last-Event-ID to replay missed messages
const sse = new EventSource('http://localhost:8080/subscribe?api_key=xxx&channel=my-room&last_event_id=42');
// Server replays all messages after event ID 42, then streams live
```

### 4. Backend Publishing
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

### 5. Webhooks

**`POST /webhooks`**
```json
// Register a webhook
{
  "url": "https://your-server.com/hook",
  "events": ["message", "join", "leave"],
  "channel": "my-room"
}
```

**`DELETE /webhooks?id=WEBHOOK_ID`**

**`GET /webhooks`** — List all registered webhooks.

### 6. Monitoring

**`GET /health`** — Returns `200 OK` with connection stats.

**`GET /stats`** — Detailed server statistics.
```bash
curl http://localhost:8080/stats
```
```json
{
  "connections": 12,
  "rooms": 3,
  "online_users": ["alice", "bob", "charlie"]
}
```

**`GET /channels/{name}/presence`** — List members of a room.
```bash
curl http://localhost:8080/channels/chat/presence
```
```json
{
  "channel": "chat",
  "members": ["alice", "bob"],
  "count": 2
}
```

**`GET /metrics`** — Prometheus-compatible metrics (connections, messages, errors).

---

## Architecture Details

*   **Message Format:** All socket and SSE messages use a unified JSON envelope: `{"type": "...", "payload": {}, "timestamp": 123456789}`.
*   **Scaling:** The Go implementation includes a `ValkeyBridge`. When enabled via `VALKEY_ADDR`, it broadcasts messages over a Redis/Valkey cluster, allowing horizontal scaling across multiple nodes.
*   **Persistence:** Elixir API keys are persisted via Docker volumes in `./ws-elixir/data`. Go persists to `./ws-go/data/api_keys.json`.
*   **Security Layers:** API key → Origin check → Optional JWT → Channel ACL. All layers are composable.
*   **Graceful Shutdown:** The Go server handles `SIGINT`/`SIGTERM` with a 10-second drain period for active connections.

---

## 📖 Step-by-Step Usage Guide

### Step 1: Start the Server

```bash
cd ws-go
go run ./cmd/server
```

The server starts on `http://localhost:8080`. Open it in your browser to use the built-in test console.

### Step 2: Create an API Key

Every client needs an API key to connect. Generate one via HTTP:

```bash
curl -X POST http://localhost:8080/api/keys \
  -H "Content-Type: application/json" \
  -d '{"name": "My App", "domains": ["*"]}'
```

Response:
```json
{
  "key": "a1b2c3d4e5f6...",
  "name": "My App",
  "allowed_domains": ["*"],
  "active": true,
  "created_at": 1718000000000
}
```

Save the `key` value — you'll use it for all connections.

### Step 3: Connect a WebSocket Client

```javascript
const API_KEY = "a1b2c3d4e5f6...";  // from Step 2
const ws = new WebSocket(`ws://localhost:8080/ws?api_key=${API_KEY}&channel=chat&client_id=user1`);

ws.onopen = () => console.log("Connected!");
ws.onmessage = (e) => console.log("Received:", JSON.parse(e.data));
```

Query parameters:
| Param | Required | Description |
|-------|----------|-------------|
| `api_key` | Yes | Your API key |
| `channel` | No | Auto-join this room on connect |
| `client_id` | No | Unique user ID (auto-generated if omitted) |
| `token` | No | JWT token for channel-level permissions |

### Step 4: Send Messages

Once connected, send JSON messages via WebSocket. Every message has this shape:

```json
{
  "type": "room_message",
  "room": "chat",
  "payload": { "text": "Hello!" },
  "id": "optional-for-ack",
  "target_id": "only-for-direct",
  "timestamp": 1718000000000
}
```

---

## 💬 Message Types Reference

### `room_message` — Send to everyone in a room

Both clients must be in the **same room/channel**.

```javascript
ws.send(JSON.stringify({
  type: "room_message",
  room: "chat",
  payload: { text: "Hello room!" }
}));
```

| Field | Value |
|-------|-------|
| `type` | `"room_message"` |
| `room` | Room name (must match joined channel) |
| `target_id` | Not used |

### `broadcast` — Send to ALL connected clients

No room needed. Every client on the server receives it.

```javascript
ws.send(JSON.stringify({
  type: "broadcast",
  payload: { text: "Global announcement!" }
}));
```

| Field | Value |
|-------|-------|
| `type` | `"broadcast"` |
| `room` | Not used |
| `target_id` | Not used |

### `direct` — Send to one specific user (1:1)

Only the target user receives the message.

```javascript
ws.send(JSON.stringify({
  type: "direct",
  target_id: "user2",
  payload: { text: "Private message for you" }
}));
```

| Field | Value |
|-------|-------|
| `type` | `"direct"` |
| `room` | Not used |
| `target_id` | The recipient's `client_id` |

### `join_room` — Join a room after connecting

```javascript
ws.send(JSON.stringify({ type: "join_room", room: "new-room" }));
```

### `leave_room` — Leave a room

```javascript
ws.send(JSON.stringify({ type: "leave_room", room: "old-room" }));
```

### `ping` — Keep-alive

```javascript
ws.send(JSON.stringify({ type: "ping" }));
// Server responds with: { "type": "pong" }
```

### `user_info` — Set your display info

```javascript
ws.send(JSON.stringify({
  type: "user_info",
  payload: { display_name: "John", status: "online", avatar: "https://..." }
}));
```

---

## 📤 Publishing from Your Backend

Your server-side code pushes events to all subscribers via HTTP:

```bash
curl -X POST http://localhost:8080/publish \
  -H "X-API-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "channel": "chat",
    "event": "notification",
    "data": { "message": "New order received!" },
    "publisher": "order-service"
  }'
```

Response:
```json
{
  "status": "published",
  "channel": "chat",
  "ws_delivered": 3,
  "sse_delivered": 1
}
```

The `publisher` field is optional — if omitted, it defaults to your API key's name. The value is sent as `user_id` in the broadcast so receivers know who sent it.

---

## 🔄 Complete Example: Two Clients Chatting

### Terminal 1 — Start Server
```bash
cd ws-go && go run ./cmd/server
```

### Terminal 2 — Create a key
```bash
curl -s -X POST http://localhost:8080/api/keys \
  -H "Content-Type: application/json" \
  -d '{"name":"Test","domains":["*"]}' | jq .key
# Output: "abc123..."
```

### Browser Tab 1 — Client A
```javascript
const ws = new WebSocket('ws://localhost:8080/ws?api_key=abc123&channel=chat&client_id=alice');
ws.onmessage = (e) => console.log("Alice got:", JSON.parse(e.data));

// After connected, send:
ws.send(JSON.stringify({ type: "room_message", room: "chat", payload: { text: "Hi Bob!" } }));
```

### Browser Tab 2 — Client B
```javascript
const ws = new WebSocket('ws://localhost:8080/ws?api_key=abc123&channel=chat&client_id=bob');
ws.onmessage = (e) => console.log("Bob got:", JSON.parse(e.data));

// Bob receives: {"type":"room_message","user_id":"alice","room":"chat","payload":{"text":"Hi Bob!"},"timestamp":...}
```

### Backend pushes to both:
```bash
curl -X POST http://localhost:8080/publish \
  -H "X-API-Key: abc123" -H "Content-Type: application/json" \
  -d '{"channel":"chat","event":"alert","data":{"text":"Server restarting!"},"publisher":"admin"}'
# Both Alice and Bob receive it
```

---

## 🔑 Cross API-Key Communication

**Different API keys CAN communicate with each other.** The hub routes by room/channel membership, not by API key.

```
Client A (Key 1) ──joins──→ "notifications"
Client B (Key 2) ──joins──→ "notifications"
                                    ↓
              Both receive messages sent to "notifications"
```

API keys control:
- ✅ Authentication (is the key valid?)
- ✅ Origin enforcement (allowed domains)
- ✅ Rate limiting (per-key throttle)
- ❌ Message isolation (keys share rooms by default)

### Isolating Tenants with ACL

If you need Key A clients to NOT see Key B messages, use Channel ACL:

```bash
# Create a restricted channel that only key1 can join
curl -X POST http://localhost:8080/acl \
  -H "Content-Type: application/json" \
  -d '{
    "channel": "tenant-a-private",
    "public": false,
    "allowed_keys": ["key1-value-here"],
    "max_members": 100
  }'

# List all ACL rules
curl http://localhost:8080/acl

# Delete an ACL rule (makes channel public again)
curl -X DELETE "http://localhost:8080/acl?channel=tenant-a-private"
```

Now only clients using `key1` can join `tenant-a-private`.

---

## 🔐 Security Layers

Connections go through these checks in order:

```
1. API Key valid?          → 403 if invalid/revoked
2. Origin allowed?         → 403 if domain mismatch  
3. Rate limit OK?          → 429 if exceeded
4. JWT valid? (optional)   → 401 if token expired/invalid
5. Channel ACL?            → 403 if not in allowed_keys
6. Max members?            → 403 if room is full
```

### Enable JWT Authentication

Set the environment variable before starting:
```bash
JWT_SECRET=my-super-secret-key go run ./cmd/server
```

Generate tokens:
```bash
curl -X POST http://localhost:8080/auth/token \
  -H "X-API-Key: YOUR_KEY" -H "Content-Type: application/json" \
  -d '{
    "user_id": "user123",
    "channels": ["chat", "alerts"],
    "permissions": {"chat": "write", "alerts": "read"}
  }'
```

Connect with the token:
```javascript
const ws = new WebSocket('ws://localhost:8080/ws?api_key=KEY&token=JWT_TOKEN&client_id=user123');
```

---

## 🪝 Webhooks

Get notified on your server when events happen:

```bash
# Register a webhook
curl -X POST http://localhost:8080/webhooks \
  -H "Content-Type: application/json" \
  -d '{
    "id": "my-hook-1",
    "url": "https://your-server.com/webhook",
    "events": ["message", "join", "leave"],
    "channel": "chat",
    "secret": "webhook-signing-secret"
  }'

# List webhooks
curl http://localhost:8080/webhooks

# Remove a webhook
curl -X DELETE "http://localhost:8080/webhooks?id=my-hook-1"
```

Your server receives POST requests like:
```json
{
  "event": "message",
  "channel": "chat",
  "data": { "text": "Hello!" },
  "timestamp": 1718000000000
}
```

---

## 📊 Monitoring

### Health Check
```bash
curl http://localhost:8080/health
```
```json
{
  "status": "ok",
  "connections": 42,
  "rooms": 5,
  "uptime": "2h15m"
}
```

### Prometheus Metrics
```bash
curl http://localhost:8080/metrics
```
Exposes: `ws_connections`, `ws_messages_in`, `ws_messages_out`, `messages_published`, `rate_limit_hits`, `webhook_deliveries`, etc.

---

## ⚙️ Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | HTTP server port |
| `JWT_SECRET` | (disabled) | Secret key to enable JWT auth |
| `JWT_ISSUER` | `synchub` | Issuer claim in JWT tokens |
| `VALKEY_URL` | (standalone) | Valkey/Redis URL for clustering |

---

## 🖥️ Test Console UI

Visit `http://localhost:8080` for a built-in test interface that lets you:
- Generate API keys
- Connect WebSocket/SSE clients
- Send all message types (room, broadcast, direct)
- Publish via HTTP POST with custom publisher name
- View real-time message log
- Get browser notifications when tab is in background

---

## 🐳 Docker Deployment

### Run with Docker Compose (both servers)
```bash
docker compose up -d
# Go server → http://localhost:8080
# Elixir server → http://localhost:4000
```

### Run only the Go server
```bash
docker compose up ws-go -d
```

### Build manually
```bash
cd ws-go
docker build -t synchub .
docker run -p 8080:8080 -v ./data:/app/data synchub
```

### With JWT and Valkey enabled
```bash
docker run -p 8080:8080 \
  -e JWT_SECRET=my-secret-key \
  -e VALKEY_URL=redis://valkey:6379 \
  -v ./data:/app/data \
  synchub
```

### Security hardening (built-in)
- Runs as non-root user (`synchub`, UID 1000)
- Read-only root filesystem
- No new privileges (`no-new-privileges:true`)
- Stripped binary, minimal Alpine base
- Built-in health check on `/health`

---

## 📋 Complete API Endpoint Summary

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/keys` | Create API key |
| `GET` | `/api/keys` | List all keys (masked) |
| `PUT` | `/api/keys/update?key=X` | Update key properties |
| `DELETE` | `/api/keys/revoke?key=X` | Revoke key + drop clients |
| `GET` | `/ws` | WebSocket upgrade |
| `GET` | `/subscribe` | SSE stream |
| `POST` | `/publish` | Publish to channel |
| `POST` | `/auth/token` | Generate JWT token |
| `POST` | `/acl` | Set channel ACL |
| `GET` | `/acl` | List all ACLs |
| `DELETE` | `/acl?channel=X` | Remove channel ACL |
| `POST` | `/webhooks` | Register webhook |
| `GET` | `/webhooks` | List webhooks |
| `DELETE` | `/webhooks?id=X` | Remove webhook |
| `GET` | `/channels/{name}/presence` | Room members |
| `GET` | `/health` | Health check |
| `GET` | `/stats` | Server statistics |
| `GET` | `/metrics` | Prometheus metrics |
