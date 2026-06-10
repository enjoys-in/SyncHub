// ─── SyncHub TypeScript SDK Types ─────────────────────────────────────────────
// Complete type definitions for all WebSocket messages and HTTP API payloads.

// ─── Common ──────────────────────────────────────────────────────────────────

/** Base message envelope for all WebSocket communication */
export interface BaseMessage {
  type: string;
  id?: string;
  user_id?: string;
  target_id?: string;
  room?: string;
  payload?: unknown;
  timestamp: number;
}

// ─── Client → Server (WebSocket Send) ────────────────────────────────────────

export interface RoomMessage {
  type: "room_message";
  room: string;
  payload: Record<string, unknown>;
  id?: string;
  timestamp?: number;
}

export interface BroadcastMessage {
  type: "broadcast";
  payload: Record<string, unknown>;
  id?: string;
  timestamp?: number;
}

export interface DirectMessage {
  type: "direct";
  target_id: string;
  payload: Record<string, unknown>;
  id?: string;
  timestamp?: number;
}

export interface JoinRoomMessage {
  type: "join_room";
  room: string;
  id?: string;
}

export interface LeaveRoomMessage {
  type: "leave_room";
  room: string;
  id?: string;
}

export interface PingMessage {
  type: "ping";
}

export interface UserInfoMessage {
  type: "user_info";
  payload: UserInfo;
  id?: string;
}

export interface UserInfo {
  user_id?: string;
  display_name?: string;
  avatar?: string;
  status?: string;
  metadata?: Record<string, string>;
}

/** Union of all client-to-server WebSocket message types */
export type ClientMessage =
  | RoomMessage
  | BroadcastMessage
  | DirectMessage
  | JoinRoomMessage
  | LeaveRoomMessage
  | PingMessage
  | UserInfoMessage;

// ─── Server → Client (WebSocket Receive) ─────────────────────────────────────

export interface SystemEvent {
  type: "system";
  payload: { text: string; user_id?: string };
  timestamp: number;
}

export interface ErrorEvent {
  type: "error";
  payload: { error: string };
  timestamp: number;
}

export interface AckEvent {
  type: "ack";
  id: string;
  payload: { status: "ok" };
  timestamp: number;
}

export interface PongEvent {
  type: "pong";
  timestamp: number;
}

export interface UserJoinedEvent {
  type: "user_joined";
  user_id: string;
  room?: string;
  payload: { user_id: string };
  timestamp: number;
}

export interface UserLeftEvent {
  type: "user_left";
  user_id: string;
  room?: string;
  payload: { user_id: string };
  timestamp: number;
}

export interface PresenceEvent {
  type: "presence";
  user_id: string;
  payload: UserInfo;
  timestamp: number;
}

export interface IncomingRoomMessage {
  type: "room_message";
  user_id: string;
  room: string;
  payload: Record<string, unknown>;
  timestamp: number;
}

export interface IncomingBroadcast {
  type: "broadcast";
  user_id: string;
  payload: Record<string, unknown>;
  timestamp: number;
}

export interface IncomingDirect {
  type: "direct";
  user_id: string;
  target_id: string;
  payload: Record<string, unknown>;
  timestamp: number;
}

/** Union of all server-to-client WebSocket event types */
export type ServerEvent =
  | SystemEvent
  | ErrorEvent
  | AckEvent
  | PongEvent
  | UserJoinedEvent
  | UserLeftEvent
  | PresenceEvent
  | IncomingRoomMessage
  | IncomingBroadcast
  | IncomingDirect;

// ─── HTTP API: Request Bodies ────────────────────────────────────────────────

/** POST /api/keys */
export interface CreateKeyRequest {
  name: string;
  domains: string[];
}

/** PUT /api/keys/update?key=X */
export interface UpdateKeyRequest {
  name?: string;
  domains?: string[];
  active?: boolean;
}

/** POST /publish */
export interface PublishRequest {
  channel: string;
  event?: string;
  data: unknown;
  publisher?: string;
}

/** POST /auth/token */
export interface TokenRequest {
  user_id: string;
  display_name?: string;
  roles?: string[];
  channels?: string[];
  permissions?: Record<string, "read" | "write" | "admin">;
  ttl?: string;
}

/** POST /acl */
export interface SetACLRequest {
  channel: string;
  public?: boolean;
  max_members?: number;
  allowed_keys?: string[];
  permissions?: Record<string, "read" | "write" | "admin">;
}

/** POST /webhooks */
export interface RegisterWebhookRequest {
  id: string;
  url: string;
  events: WebhookEventType[];
  channel?: string;
  secret?: string;
}

export type WebhookEventType = "message" | "join" | "leave" | "*";

// ─── HTTP API: Response Bodies ───────────────────────────────────────────────

/** Response from POST /api/keys */
export interface APIKey {
  key: string;
  name: string;
  allowed_domains: string[];
  channels?: string[];
  created_at: number;
  updated_at?: number;
  active: boolean;
}

/** Response from POST /publish */
export interface PublishResponse {
  status: "published";
  channel: string;
  ws_delivered: number;
  sse_delivered: number;
}

/** Response from POST /auth/token */
export interface TokenResponse {
  token: string;
  expires_at: string;
}

/** Response from GET /health */
export interface HealthResponse {
  status: "ok";
  connections: number;
  rooms: number;
  uptime: string;
}

/** Response from GET /stats */
export interface StatsResponse {
  connections: number;
  rooms: number;
  online_users: string[];
}

/** Response from GET /channels/{name}/presence */
export interface PresenceResponse {
  channel: string;
  members: string[];
  count: number;
}

/** Response from GET /acl */
export interface ChannelACL {
  channel: string;
  public: boolean;
  max_members: number;
  allowed_keys: string[];
  permissions: Record<string, string>;
}

/** Response from GET /webhooks */
export interface Webhook {
  id: string;
  url: string;
  events: string[];
  channel: string;
  secret: string; // masked as "***"
  active: boolean;
}

/** Webhook delivery payload (what your server receives) */
export interface WebhookPayload {
  event: string;
  channel: string;
  data: unknown;
  timestamp: number;
}

/** Error response from any endpoint */
export interface ErrorResponse {
  error: string;
}

// ─── SSE Event Types ─────────────────────────────────────────────────────────

/** SSE "connected" event data */
export interface SSEConnectedEvent {
  channel: string;
  timestamp: number;
}

/** SSE "message" event data (same shape as server broadcast) */
export type SSEMessageEvent = {
  type: string;
  user_id?: string;
  room?: string;
  payload: unknown;
  timestamp: number;
};

// ─── WebSocket Connection Parameters ─────────────────────────────────────────

/** Query params for ws://host/ws?... */
export interface WSConnectParams {
  api_key: string;
  channel?: string;
  client_id?: string;
  token?: string;
}

/** Query params for /subscribe?... */
export interface SSEConnectParams {
  api_key: string;
  channel: string;
  last_event_id?: string;
}

// ─── Helper: Type Guard ──────────────────────────────────────────────────────

/** Type guard to narrow ServerEvent by type */
export function isEventType<T extends ServerEvent["type"]>(
  event: ServerEvent,
  type: T
): event is Extract<ServerEvent, { type: T }> {
  return event.type === type;
}
