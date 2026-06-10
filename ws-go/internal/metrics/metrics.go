package metrics

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics collects Prometheus-compatible metrics for the broker.
type Metrics struct {
	WsConnections     atomic.Int64
	SseConnections    atomic.Int64
	MessagesPublished atomic.Int64
	MessagesDelivered atomic.Int64
	WsMessagesIn      atomic.Int64
	WsMessagesOut     atomic.Int64
	HttpRequests      sync.Map // method:path -> count
	HttpErrors        atomic.Int64
	RateLimitHits     atomic.Int64
	KeyRevocations    atomic.Int64
	WebhookDeliveries atomic.Int64
	WebhookFailures   atomic.Int64
	StartTime         time.Time
}

// New creates a new Metrics instance.
func New() *Metrics {
	return &Metrics{
		StartTime: time.Now(),
	}
}

// ServeMetrics returns a Prometheus-compatible /metrics endpoint.
func (m *Metrics) ServeMetrics() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		uptime := time.Since(m.StartTime).Seconds()

		fmt.Fprintf(w, "# HELP synchub_uptime_seconds Time since server start in seconds.\n")
		fmt.Fprintf(w, "# TYPE synchub_uptime_seconds gauge\n")
		fmt.Fprintf(w, "synchub_uptime_seconds %.2f\n\n", uptime)

		fmt.Fprintf(w, "# HELP synchub_ws_connections_active Current active WebSocket connections.\n")
		fmt.Fprintf(w, "# TYPE synchub_ws_connections_active gauge\n")
		fmt.Fprintf(w, "synchub_ws_connections_active %d\n\n", m.WsConnections.Load())

		fmt.Fprintf(w, "# HELP synchub_sse_connections_active Current active SSE connections.\n")
		fmt.Fprintf(w, "# TYPE synchub_sse_connections_active gauge\n")
		fmt.Fprintf(w, "synchub_sse_connections_active %d\n\n", m.SseConnections.Load())

		fmt.Fprintf(w, "# HELP synchub_messages_published_total Total messages published via /publish endpoint.\n")
		fmt.Fprintf(w, "# TYPE synchub_messages_published_total counter\n")
		fmt.Fprintf(w, "synchub_messages_published_total %d\n\n", m.MessagesPublished.Load())

		fmt.Fprintf(w, "# HELP synchub_messages_delivered_total Total messages delivered to subscribers.\n")
		fmt.Fprintf(w, "# TYPE synchub_messages_delivered_total counter\n")
		fmt.Fprintf(w, "synchub_messages_delivered_total %d\n\n", m.MessagesDelivered.Load())

		fmt.Fprintf(w, "# HELP synchub_ws_messages_received_total Total WebSocket messages received from clients.\n")
		fmt.Fprintf(w, "# TYPE synchub_ws_messages_received_total counter\n")
		fmt.Fprintf(w, "synchub_ws_messages_received_total %d\n\n", m.WsMessagesIn.Load())

		fmt.Fprintf(w, "# HELP synchub_ws_messages_sent_total Total WebSocket messages sent to clients.\n")
		fmt.Fprintf(w, "# TYPE synchub_ws_messages_sent_total counter\n")
		fmt.Fprintf(w, "synchub_ws_messages_sent_total %d\n\n", m.WsMessagesOut.Load())

		fmt.Fprintf(w, "# HELP synchub_http_errors_total Total HTTP error responses.\n")
		fmt.Fprintf(w, "# TYPE synchub_http_errors_total counter\n")
		fmt.Fprintf(w, "synchub_http_errors_total %d\n\n", m.HttpErrors.Load())

		fmt.Fprintf(w, "# HELP synchub_rate_limit_hits_total Total rate limit rejections.\n")
		fmt.Fprintf(w, "# TYPE synchub_rate_limit_hits_total counter\n")
		fmt.Fprintf(w, "synchub_rate_limit_hits_total %d\n\n", m.RateLimitHits.Load())

		fmt.Fprintf(w, "# HELP synchub_key_revocations_total Total API key revocations.\n")
		fmt.Fprintf(w, "# TYPE synchub_key_revocations_total counter\n")
		fmt.Fprintf(w, "synchub_key_revocations_total %d\n\n", m.KeyRevocations.Load())

		fmt.Fprintf(w, "# HELP synchub_webhook_deliveries_total Total webhook delivery attempts.\n")
		fmt.Fprintf(w, "# TYPE synchub_webhook_deliveries_total counter\n")
		fmt.Fprintf(w, "synchub_webhook_deliveries_total %d\n\n", m.WebhookDeliveries.Load())

		fmt.Fprintf(w, "# HELP synchub_webhook_failures_total Total webhook delivery failures.\n")
		fmt.Fprintf(w, "# TYPE synchub_webhook_failures_total counter\n")
		fmt.Fprintf(w, "synchub_webhook_failures_total %d\n\n", m.WebhookFailures.Load())
	}
}
