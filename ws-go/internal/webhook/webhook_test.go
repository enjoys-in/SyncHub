package webhook

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestManager_Register(t *testing.T) {
	m := NewManager(nil)

	wh := &Webhook{
		ID:      "wh-1",
		URL:     "http://example.com/hook",
		Events:  []string{"message", "join"},
		Channel: "room-a",
		Secret:  "my-secret",
	}
	m.Register(wh)

	list := m.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 webhook, got %d", len(list))
	}
	if list[0].ID != "wh-1" {
		t.Error("ID mismatch")
	}
	if list[0].Secret != "***" {
		t.Error("secret should be masked in List()")
	}
	if !list[0].Active {
		t.Error("registered webhook should be active")
	}
}

func TestManager_Unregister(t *testing.T) {
	m := NewManager(nil)
	m.Register(&Webhook{ID: "wh-1", URL: "http://example.com"})

	ok := m.Unregister("wh-1")
	if !ok {
		t.Error("should return true for existing webhook")
	}

	ok = m.Unregister("wh-1")
	if ok {
		t.Error("should return false for already-removed webhook")
	}

	if len(m.List()) != 0 {
		t.Error("list should be empty")
	}
}

func TestManager_Dispatch(t *testing.T) {
	var received atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)

		var payload Payload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("failed to decode webhook payload: %v", err)
			return
		}
		if payload.Event != "message" {
			t.Errorf("expected event 'message', got '%s'", payload.Event)
		}
		if payload.Channel != "room-a" {
			t.Errorf("expected channel 'room-a', got '%s'", payload.Channel)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	m := NewManager(nil)
	m.Register(&Webhook{
		ID:      "wh-1",
		URL:     server.URL,
		Events:  []string{"message"},
		Channel: "room-a",
	})

	m.Dispatch("message", "room-a", map[string]string{"text": "hello"})

	// Wait for async delivery
	time.Sleep(100 * time.Millisecond)

	if received.Load() != 1 {
		t.Errorf("expected 1 delivery, got %d", received.Load())
	}
}

func TestManager_Dispatch_EventFilter(t *testing.T) {
	var received atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	m := NewManager(nil)
	m.Register(&Webhook{
		ID:     "wh-1",
		URL:    server.URL,
		Events: []string{"join"},
	})

	// Dispatch "message" - should NOT trigger "join" webhook
	m.Dispatch("message", "room-a", nil)
	time.Sleep(50 * time.Millisecond)

	if received.Load() != 0 {
		t.Error("webhook should not fire for non-matching event")
	}

	// Dispatch "join" - should trigger
	m.Dispatch("join", "room-a", nil)
	time.Sleep(50 * time.Millisecond)

	if received.Load() != 1 {
		t.Errorf("expected 1 delivery for matching event, got %d", received.Load())
	}
}

func TestManager_Dispatch_ChannelFilter(t *testing.T) {
	var received atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	m := NewManager(nil)
	m.Register(&Webhook{
		ID:      "wh-1",
		URL:     server.URL,
		Events:  []string{"message"},
		Channel: "room-a",
	})

	// Different channel - should not trigger
	m.Dispatch("message", "room-b", nil)
	time.Sleep(50 * time.Millisecond)

	if received.Load() != 0 {
		t.Error("webhook should not fire for different channel")
	}
}

func TestManager_Dispatch_InactiveWebhook(t *testing.T) {
	var received atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	m := NewManager(nil)
	wh := &Webhook{
		ID:     "wh-1",
		URL:    server.URL,
		Events: []string{"message"},
		Active: false, // explicitly inactive
	}
	m.Register(wh) // Register sets Active=true

	// Manually deactivate
	m.mu.Lock()
	m.webhooks["wh-1"].Active = false
	m.mu.Unlock()

	m.Dispatch("message", "room-a", nil)
	time.Sleep(50 * time.Millisecond)

	if received.Load() != 0 {
		t.Error("inactive webhook should not fire")
	}
}
