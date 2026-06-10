package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateRandomKey(t *testing.T) {
	key, err := GenerateRandomKey(32)
	if err != nil {
		t.Fatalf("GenerateRandomKey failed: %v", err)
	}
	if len(key) != 64 { // 32 bytes = 64 hex chars
		t.Errorf("expected 64 hex chars, got %d", len(key))
	}

	key2, _ := GenerateRandomKey(32)
	if key == key2 {
		t.Error("two generated keys should not be equal")
	}
}

func TestAPIKeyStore_GenerateKey(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "keys.json")
	store := NewAPIKeyStore(tmpFile)

	apiKey, err := store.GenerateKey("Test App", []string{"localhost", "*.example.com"})
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	if apiKey.Name != "Test App" {
		t.Errorf("expected name 'Test App', got '%s'", apiKey.Name)
	}
	if !apiKey.Active {
		t.Error("new key should be active")
	}
	if len(apiKey.AllowedDomains) != 2 {
		t.Errorf("expected 2 domains, got %d", len(apiKey.AllowedDomains))
	}
	if apiKey.CreatedAt == 0 {
		t.Error("CreatedAt should be set")
	}
}

func TestAPIKeyStore_GenerateKey_Validation(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "keys.json")
	store := NewAPIKeyStore(tmpFile)

	_, err := store.GenerateKey("", []string{"localhost"})
	if err == nil {
		t.Error("should reject empty name")
	}

	_, err = store.GenerateKey("App", nil)
	if err == nil {
		t.Error("should reject nil domains")
	}

	_, err = store.GenerateKey("App", []string{})
	if err == nil {
		t.Error("should reject empty domains")
	}
}

func TestAPIKeyStore_Validate(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "keys.json")
	store := NewAPIKeyStore(tmpFile)

	apiKey, _ := store.GenerateKey("Test", []string{"localhost", "*.example.com"})

	// Valid key, no origin
	result, err := store.Validate(apiKey.Key, "")
	if err != nil {
		t.Fatalf("Validate with no origin failed: %v", err)
	}
	if result.Key != apiKey.Key {
		t.Error("returned key mismatch")
	}

	// Valid key, matching origin
	_, err = store.Validate(apiKey.Key, "http://localhost")
	if err != nil {
		t.Errorf("Validate with localhost origin failed: %v", err)
	}

	// Valid key, wildcard domain match
	_, err = store.Validate(apiKey.Key, "http://app.example.com")
	if err != nil {
		t.Errorf("Validate with wildcard domain failed: %v", err)
	}

	// Valid key, non-matching origin
	_, err = store.Validate(apiKey.Key, "http://evil.com")
	if err == nil {
		t.Error("should reject non-matching origin")
	}

	// Invalid key
	_, err = store.Validate("nonexistent", "")
	if err == nil {
		t.Error("should reject invalid key")
	}
}

func TestAPIKeyStore_Revoke(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "keys.json")
	store := NewAPIKeyStore(tmpFile)

	apiKey, _ := store.GenerateKey("Test", []string{"localhost"})

	ok := store.Revoke(apiKey.Key)
	if !ok {
		t.Error("Revoke should return true for existing key")
	}

	// Validate should now fail
	_, err := store.Validate(apiKey.Key, "")
	if err == nil {
		t.Error("should reject revoked key")
	}

	// Revoke non-existent
	ok = store.Revoke("nonexistent")
	if ok {
		t.Error("Revoke should return false for non-existent key")
	}
}

func TestAPIKeyStore_Update(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "keys.json")
	store := NewAPIKeyStore(tmpFile)

	apiKey, _ := store.GenerateKey("Original", []string{"localhost"})

	newName := "Updated"
	updated, err := store.Update(apiKey.Key, &newName, []string{"*.new.com"}, nil)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if updated.Name != "Updated" {
		t.Errorf("expected name 'Updated', got '%s'", updated.Name)
	}
	if updated.AllowedDomains[0] != "*.new.com" {
		t.Error("domains not updated")
	}
	if updated.UpdatedAt == 0 {
		t.Error("UpdatedAt should be set after update")
	}

	// Update non-existent
	_, err = store.Update("nonexistent", &newName, nil, nil)
	if err == nil {
		t.Error("should fail for non-existent key")
	}
}

func TestAPIKeyStore_Persistence(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "keys.json")
	store := NewAPIKeyStore(tmpFile)

	apiKey, _ := store.GenerateKey("Persistent", []string{"localhost"})

	// Load a new store from the same file
	store2 := NewAPIKeyStore(tmpFile)
	result := store2.Get(apiKey.Key)
	if result == nil {
		t.Fatal("key should be persisted and reloaded")
	}
	if result.Name != "Persistent" {
		t.Errorf("persisted name mismatch: got '%s'", result.Name)
	}

	// Verify file exists
	if _, err := os.Stat(tmpFile); os.IsNotExist(err) {
		t.Error("keys file should exist on disk")
	}
}

func TestAPIKeyStore_List(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "keys.json")
	store := NewAPIKeyStore(tmpFile)

	store.GenerateKey("App1", []string{"localhost"})
	store.GenerateKey("App2", []string{"example.com"})

	list := store.List()
	if len(list) != 2 {
		t.Errorf("expected 2 keys, got %d", len(list))
	}

	// Keys should be masked
	for _, k := range list {
		if len(k.Key) > 12 && k.Key[8:11] != "..." {
			t.Errorf("key should be masked, got: %s", k.Key)
		}
	}
}

func TestMatchDomainPattern(t *testing.T) {
	tests := []struct {
		pattern string
		host    string
		want    bool
	}{
		{"localhost", "localhost", true},
		{"example.com", "example.com", true},
		{"example.com", "other.com", false},
		{"*.example.com", "sub.example.com", true},
		{"*.example.com", "example.com", true},
		{"*.example.com", "deep.sub.example.com", true},
		{"*.example.com", "other.com", false},
		// Note: bare "*" wildcard is handled by matchesDomain, not MatchDomainPattern
	}

	for _, tt := range tests {
		got := MatchDomainPattern(tt.pattern, tt.host)
		if got != tt.want {
			t.Errorf("MatchDomainPattern(%q, %q) = %v, want %v", tt.pattern, tt.host, got, tt.want)
		}
	}
}

func TestExtractHost(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"http://localhost", "localhost"},
		{"http://localhost:3000", "localhost"},
		{"https://app.example.com", "app.example.com"},
		{"https://app.example.com:443/path", "app.example.com"},
		{"", ""},
		{"just-a-host", "just-a-host"},
	}

	for _, tt := range tests {
		got := ExtractHost(tt.input)
		if got != tt.want {
			t.Errorf("ExtractHost(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
