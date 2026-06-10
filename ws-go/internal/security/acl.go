package security

import (
	"sync"
)

// ChannelPermission defines access levels for a channel.
type ChannelPermission string

const (
	PermRead  ChannelPermission = "read"
	PermWrite ChannelPermission = "write"
	PermAdmin ChannelPermission = "admin"
)

// ChannelACL defines access control rules for channels.
type ChannelACL struct {
	Channel     string            `json:"channel"`
	Public      bool              `json:"public"`
	MaxMembers  int               `json:"max_members"`
	AllowedKeys []string          `json:"allowed_keys"`
	Permissions map[string]string `json:"permissions"`
}

// ACLManager manages channel-level access control.
type ACLManager struct {
	mu       sync.RWMutex
	channels map[string]*ChannelACL
}

// NewACLManager creates a new ACL manager.
func NewACLManager() *ACLManager {
	return &ACLManager{
		channels: make(map[string]*ChannelACL),
	}
}

// SetChannelACL creates or updates ACL rules for a channel.
func (am *ACLManager) SetChannelACL(acl *ChannelACL) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.channels[acl.Channel] = acl
}

// GetChannelACL returns the ACL for a channel (nil if no ACL set = public).
func (am *ACLManager) GetChannelACL(channel string) *ChannelACL {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.channels[channel]
}

// RemoveChannelACL removes ACL rules for a channel.
func (am *ACLManager) RemoveChannelACL(channel string) bool {
	am.mu.Lock()
	defer am.mu.Unlock()
	if _, ok := am.channels[channel]; ok {
		delete(am.channels, channel)
		return true
	}
	return false
}

// ListACLs returns all channel ACL configurations.
func (am *ACLManager) ListACLs() []*ChannelACL {
	am.mu.RLock()
	defer am.mu.RUnlock()
	list := make([]*ChannelACL, 0, len(am.channels))
	for _, acl := range am.channels {
		list = append(list, acl)
	}
	return list
}

// CanJoin checks if a user/key can join a channel.
func (am *ACLManager) CanJoin(channel string, apiKey string, userID string) bool {
	am.mu.RLock()
	acl, exists := am.channels[channel]
	am.mu.RUnlock()

	if !exists || acl.Public {
		return true
	}

	if len(acl.AllowedKeys) > 0 {
		keyAllowed := false
		for _, k := range acl.AllowedKeys {
			if k == apiKey {
				keyAllowed = true
				break
			}
		}
		if !keyAllowed {
			return false
		}
	}

	if len(acl.Permissions) > 0 {
		_, hasPermission := acl.Permissions[userID]
		return hasPermission
	}

	return true
}

// CanWrite checks if a user can write to a channel.
func (am *ACLManager) CanWrite(channel string, userID string) bool {
	am.mu.RLock()
	acl, exists := am.channels[channel]
	am.mu.RUnlock()

	if !exists || acl.Public {
		return true
	}

	if len(acl.Permissions) == 0 {
		return true
	}

	perm, ok := acl.Permissions[userID]
	if !ok {
		return false
	}

	return PermissionLevel(perm) >= PermissionLevel("write")
}

// CheckMaxMembers returns true if the channel hasn't hit its member limit.
func (am *ACLManager) CheckMaxMembers(channel string, currentCount int) bool {
	am.mu.RLock()
	acl, exists := am.channels[channel]
	am.mu.RUnlock()

	if !exists || acl.MaxMembers == 0 {
		return true
	}

	return currentCount < acl.MaxMembers
}
