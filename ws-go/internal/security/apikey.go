package security

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// APIKey represents a tenant's API key with domain restrictions.
type APIKey struct {
	Key            string   `json:"key"`
	Name           string   `json:"name"`
	AllowedDomains []string `json:"allowed_domains"`
	Channels       []string `json:"channels,omitempty"`
	CreatedAt      int64    `json:"created_at"`
	UpdatedAt      int64    `json:"updated_at,omitempty"`
	Active         bool     `json:"active"`
}

// APIKeyStore manages API keys in-memory with JSON file persistence.
type APIKeyStore struct {
	mu   sync.RWMutex
	keys map[string]*APIKey
	file string
}

// NewAPIKeyStore creates a new store and loads from disk if available.
func NewAPIKeyStore(filePath string) *APIKeyStore {
	store := &APIKeyStore{
		keys: make(map[string]*APIKey),
		file: filePath,
	}
	store.load()
	return store
}

// GenerateKey creates a new API key for a tenant.
func (s *APIKeyStore) GenerateKey(name string, domains []string) (*APIKey, error) {
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if len(domains) == 0 {
		return nil, fmt.Errorf("at least one allowed domain is required")
	}

	key, err := GenerateRandomKey(32)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}

	apiKey := &APIKey{
		Key:            key,
		Name:           name,
		AllowedDomains: domains,
		CreatedAt:      time.Now().UnixMilli(),
		Active:         true,
	}

	s.mu.Lock()
	s.keys[key] = apiKey
	s.mu.Unlock()

	s.save()

	log.Printf("[apikey] created key for '%s': %s... (domains: %v)", name, key[:8], domains)
	return apiKey, nil
}

// Update modifies an existing API key's properties.
func (s *APIKeyStore) Update(key string, name *string, domains []string, active *bool) (*APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	apiKey, exists := s.keys[key]
	if !exists {
		return nil, fmt.Errorf("key not found")
	}

	if name != nil {
		apiKey.Name = *name
	}
	if domains != nil {
		apiKey.AllowedDomains = domains
	}
	if active != nil {
		apiKey.Active = *active
	}
	apiKey.UpdatedAt = time.Now().UnixMilli()

	s.save()
	log.Printf("[apikey] updated key: %s...", key[:8])
	return apiKey, nil
}

// Validate checks if an API key is valid and the origin matches allowed domains.
func (s *APIKeyStore) Validate(key string, origin string) (*APIKey, error) {
	s.mu.RLock()
	apiKey, exists := s.keys[key]
	s.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("invalid API key")
	}

	if !apiKey.Active {
		return nil, fmt.Errorf("API key is deactivated")
	}

	if origin != "" && !s.matchesDomain(apiKey, origin) {
		return nil, fmt.Errorf("origin '%s' not allowed for this API key", origin)
	}

	return apiKey, nil
}

// Get returns an API key by its key string.
func (s *APIKeyStore) Get(key string) *APIKey {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.keys[key]
}

// List returns all API keys (with keys partially masked).
func (s *APIKeyStore) List() []*APIKey {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]*APIKey, 0, len(s.keys))
	for _, k := range s.keys {
		masked := *k
		if len(masked.Key) > 8 {
			masked.Key = masked.Key[:8] + "..." + masked.Key[len(masked.Key)-4:]
		}
		list = append(list, &masked)
	}
	return list
}

// Revoke deactivates an API key.
func (s *APIKeyStore) Revoke(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if apiKey, ok := s.keys[key]; ok {
		apiKey.Active = false
		s.save()
		log.Printf("[apikey] revoked key: %s...", key[:8])
		return true
	}
	return false
}

func (s *APIKeyStore) matchesDomain(apiKey *APIKey, origin string) bool {
	host := ExtractHost(origin)
	if host == "" {
		return false
	}

	for _, pattern := range apiKey.AllowedDomains {
		if pattern == "*" {
			return true
		}
		if MatchDomainPattern(pattern, host) {
			return true
		}
	}
	return false
}

// MatchDomainPattern supports exact match and wildcard subdomain matching.
func MatchDomainPattern(pattern, host string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	host = strings.ToLower(strings.TrimSpace(host))

	if pattern == host {
		return true
	}

	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:]
		return strings.HasSuffix(host, suffix) || host == pattern[2:]
	}

	return false
}

// ExtractHost gets the hostname from an origin URL or raw domain.
func ExtractHost(origin string) string {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return ""
	}

	if strings.Contains(origin, "://") {
		if u, err := url.Parse(origin); err == nil {
			return strings.Split(u.Host, ":")[0]
		}
	}

	return strings.Split(origin, ":")[0]
}

// GenerateRandomKey creates a cryptographically secure random hex key.
func GenerateRandomKey(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func (s *APIKeyStore) save() {
	data, err := json.MarshalIndent(s.keys, "", "  ")
	if err != nil {
		log.Printf("[apikey] save error: %v", err)
		return
	}

	if err := os.WriteFile(s.file, data, 0600); err != nil {
		log.Printf("[apikey] save error: %v", err)
	}
}

func (s *APIKeyStore) load() {
	data, err := os.ReadFile(s.file)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[apikey] load error: %v", err)
		}
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := json.Unmarshal(data, &s.keys); err != nil {
		log.Printf("[apikey] parse error: %v", err)
	}

	log.Printf("[apikey] loaded %d API keys from %s", len(s.keys), s.file)
}
