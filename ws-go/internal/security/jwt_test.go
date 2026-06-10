package security

import (
	"testing"
	"time"
)

func TestNewJWTAuth(t *testing.T) {
	auth := NewJWTAuth("my-secret-key-for-testing", "synchub")
	if auth == nil {
		t.Fatal("NewJWTAuth should return non-nil for valid secret")
	}

	nilAuth := NewJWTAuth("", "synchub")
	if nilAuth != nil {
		t.Error("NewJWTAuth should return nil for empty secret")
	}
}

func TestJWTAuth_GenerateAndValidate(t *testing.T) {
	auth := NewJWTAuth("my-secret-key-for-testing", "synchub")

	token, err := auth.GenerateToken("user123", "John", []string{"admin"}, []string{"room-a", "room-b"}, map[string]string{"room-a": "write"}, time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}
	if token == "" {
		t.Fatal("token should not be empty")
	}

	claims, err := auth.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}

	if claims.UserID != "user123" {
		t.Errorf("expected user_id 'user123', got '%s'", claims.UserID)
	}
	if claims.DisplayName != "John" {
		t.Errorf("expected display_name 'John', got '%s'", claims.DisplayName)
	}
	if len(claims.Roles) != 1 || claims.Roles[0] != "admin" {
		t.Errorf("unexpected roles: %v", claims.Roles)
	}
	if len(claims.Channels) != 2 {
		t.Errorf("expected 2 channels, got %d", len(claims.Channels))
	}
	if claims.Permissions["room-a"] != "write" {
		t.Error("permission for room-a should be 'write'")
	}
}

func TestJWTAuth_GenerateToken_Validation(t *testing.T) {
	auth := NewJWTAuth("my-secret-key-for-testing", "synchub")

	_, err := auth.GenerateToken("", "Name", nil, nil, nil, time.Hour)
	if err == nil {
		t.Error("should reject empty user_id")
	}
}

func TestJWTAuth_ValidateToken_Expired(t *testing.T) {
	auth := NewJWTAuth("my-secret-key-for-testing", "synchub")

	token, _ := auth.GenerateToken("user1", "", nil, nil, nil, -time.Hour)

	_, err := auth.ValidateToken(token)
	if err == nil {
		t.Error("should reject expired token")
	}
}

func TestJWTAuth_ValidateToken_WrongSecret(t *testing.T) {
	auth1 := NewJWTAuth("secret-one-for-testing-1", "synchub")
	auth2 := NewJWTAuth("secret-two-for-testing-2", "synchub")

	token, _ := auth1.GenerateToken("user1", "", nil, nil, nil, time.Hour)

	_, err := auth2.ValidateToken(token)
	if err == nil {
		t.Error("should reject token signed with different secret")
	}
}

func TestJWTAuth_ValidateToken_BearerPrefix(t *testing.T) {
	auth := NewJWTAuth("my-secret-key-for-testing", "synchub")
	token, _ := auth.GenerateToken("user1", "", nil, nil, nil, time.Hour)

	claims, err := auth.ValidateToken("Bearer " + token)
	if err != nil {
		t.Fatalf("should accept Bearer prefix: %v", err)
	}
	if claims.UserID != "user1" {
		t.Error("user_id mismatch")
	}
}

func TestJWTAuth_HasChannelAccess(t *testing.T) {
	auth := NewJWTAuth("my-secret-key-for-testing", "synchub")

	tests := []struct {
		name       string
		claims     *JWTClaims
		channel    string
		permission string
		want       bool
	}{
		{
			name:       "nil claims",
			claims:     nil,
			channel:    "room-a",
			permission: "read",
			want:       false,
		},
		{
			name:       "no channels/permissions = full access",
			claims:     &JWTClaims{UserID: "u1"},
			channel:    "any-room",
			permission: "write",
			want:       true,
		},
		{
			name:       "explicit channel permission",
			claims:     &JWTClaims{UserID: "u1", Permissions: map[string]string{"room-a": "write"}},
			channel:    "room-a",
			permission: "read",
			want:       true,
		},
		{
			name:       "insufficient permission level",
			claims:     &JWTClaims{UserID: "u1", Permissions: map[string]string{"room-a": "read"}},
			channel:    "room-a",
			permission: "write",
			want:       false,
		},
		{
			name:       "wildcard permission",
			claims:     &JWTClaims{UserID: "u1", Permissions: map[string]string{"*": "admin"}},
			channel:    "any-room",
			permission: "write",
			want:       true,
		},
		{
			name:       "channel list match",
			claims:     &JWTClaims{UserID: "u1", Channels: []string{"room-a", "room-b"}},
			channel:    "room-b",
			permission: "read",
			want:       true,
		},
		{
			name:       "channel list no match",
			claims:     &JWTClaims{UserID: "u1", Channels: []string{"room-a"}},
			channel:    "room-c",
			permission: "read",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := auth.HasChannelAccess(tt.claims, tt.channel, tt.permission)
			if got != tt.want {
				t.Errorf("HasChannelAccess() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPermissionLevel(t *testing.T) {
	if PermissionLevel("read") != 1 {
		t.Error("read should be 1")
	}
	if PermissionLevel("write") != 2 {
		t.Error("write should be 2")
	}
	if PermissionLevel("admin") != 3 {
		t.Error("admin should be 3")
	}
	if PermissionLevel("unknown") != 0 {
		t.Error("unknown should be 0")
	}
}
