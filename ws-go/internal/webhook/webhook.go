package webhook

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"ws-go/internal/metrics"
)

// Webhook represents a registered webhook endpoint.
type Webhook struct {
	ID      string   `json:"id"`
	URL     string   `json:"url"`
	Events  []string `json:"events"`
	Channel string   `json:"channel"`
	Secret  string   `json:"secret"`
	Active  bool     `json:"active"`
}

// Payload is the payload sent to webhook endpoints.
type Payload struct {
	Event     string      `json:"event"`
	Channel   string      `json:"channel"`
	Data      interface{} `json:"data"`
	Timestamp int64       `json:"timestamp"`
}

// Manager manages webhook registrations and delivery.
type Manager struct {
	mu       sync.RWMutex
	webhooks map[string]*Webhook
	metrics  *metrics.Metrics
	client   *http.Client
}

// NewManager creates a new webhook manager.
func NewManager(m *metrics.Metrics) *Manager {
	return &Manager{
		webhooks: make(map[string]*Webhook),
		metrics:  m,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Register adds a new webhook.
func (wm *Manager) Register(webhook *Webhook) {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	webhook.Active = true
	wm.webhooks[webhook.ID] = webhook
	log.Printf("[webhook] registered: %s -> %s (events: %v)", webhook.ID, webhook.URL, webhook.Events)
}

// Unregister removes a webhook.
func (wm *Manager) Unregister(id string) bool {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	if _, ok := wm.webhooks[id]; ok {
		delete(wm.webhooks, id)
		log.Printf("[webhook] unregistered: %s", id)
		return true
	}
	return false
}

// List returns all registered webhooks.
func (wm *Manager) List() []*Webhook {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	list := make([]*Webhook, 0, len(wm.webhooks))
	for _, wh := range wm.webhooks {
		masked := *wh
		masked.Secret = "***"
		list = append(list, &masked)
	}
	return list
}

// Dispatch sends an event to all matching webhooks asynchronously.
func (wm *Manager) Dispatch(event string, channel string, data interface{}) {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	payload := Payload{
		Event:     event,
		Channel:   channel,
		Data:      data,
		Timestamp: time.Now().UnixMilli(),
	}

	for _, wh := range wm.webhooks {
		if !wh.Active {
			continue
		}
		if !wm.matchesEvent(wh, event) {
			continue
		}
		if wh.Channel != "" && wh.Channel != channel {
			continue
		}
		go wm.deliver(wh, payload)
	}
}

func (wm *Manager) matchesEvent(wh *Webhook, event string) bool {
	if len(wh.Events) == 0 {
		return true
	}
	for _, e := range wh.Events {
		if e == event || e == "*" {
			return true
		}
	}
	return false
}

func (wm *Manager) deliver(wh *Webhook, payload Payload) {
	if wm.metrics != nil {
		wm.metrics.WebhookDeliveries.Add(1)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[webhook] marshal error for %s: %v", wh.ID, err)
		return
	}

	req, err := http.NewRequest("POST", wh.URL, bytes.NewReader(body))
	if err != nil {
		log.Printf("[webhook] request error for %s: %v", wh.ID, err)
		if wm.metrics != nil {
			wm.metrics.WebhookFailures.Add(1)
		}
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "SyncHub-Webhook/1.0")
	req.Header.Set("X-Webhook-ID", wh.ID)

	if wh.Secret != "" {
		req.Header.Set("X-Webhook-Secret", wh.Secret)
	}

	resp, err := wm.client.Do(req)
	if err != nil {
		log.Printf("[webhook] delivery failed for %s -> %s: %v", wh.ID, wh.URL, err)
		if wm.metrics != nil {
			wm.metrics.WebhookFailures.Add(1)
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		log.Printf("[webhook] delivery error for %s -> %s: status %d", wh.ID, wh.URL, resp.StatusCode)
		if wm.metrics != nil {
			wm.metrics.WebhookFailures.Add(1)
		}
	}
}
