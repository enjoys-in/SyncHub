package security

import (
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTAuth handles JWT token validation for end-user authentication.
type JWTAuth struct {
	secretKey []byte
	issuer    string
}

// JWTClaims represents the claims in a JWT token.
type JWTClaims struct {
	UserID      string            `json:"user_id"`
	DisplayName string            `json:"display_name,omitempty"`
	Roles       []string          `json:"roles,omitempty"`
	Channels    []string          `json:"channels,omitempty"`
	Permissions map[string]string `json:"permissions,omitempty"`
	jwt.RegisteredClaims
}

// NewJWTAuth creates a new JWT auth handler.
func NewJWTAuth(secretKey string, issuer string) *JWTAuth {
	if secretKey == "" {
		return nil
	}
	return &JWTAuth{
		secretKey: []byte(secretKey),
		issuer:    issuer,
	}
}

// GenerateToken creates a JWT token for a user.
func (j *JWTAuth) GenerateToken(userID, displayName string, roles, channels []string, permissions map[string]string, ttl time.Duration) (string, error) {
	if userID == "" {
		return "", errors.New("user_id is required")
	}

	claims := JWTClaims{
		UserID:      userID,
		DisplayName: displayName,
		Roles:       roles,
		Channels:    channels,
		Permissions: permissions,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    j.issuer,
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(j.secretKey)
}

// ValidateToken validates a JWT token and returns the claims.
func (j *JWTAuth) ValidateToken(tokenString string) (*JWTClaims, error) {
	if j == nil {
		return nil, errors.New("JWT auth not configured")
	}

	tokenString = strings.TrimPrefix(tokenString, "Bearer ")
	tokenString = strings.TrimSpace(tokenString)

	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return j.secretKey, nil
	})

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*JWTClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}

	return claims, nil
}

// HasChannelAccess checks if the JWT claims allow access to a specific channel.
func (j *JWTAuth) HasChannelAccess(claims *JWTClaims, channel string, requiredPermission string) bool {
	if claims == nil {
		return false
	}

	if len(claims.Channels) == 0 && len(claims.Permissions) == 0 {
		return true
	}

	if perm, ok := claims.Permissions[channel]; ok {
		return PermissionLevel(perm) >= PermissionLevel(requiredPermission)
	}

	if perm, ok := claims.Permissions["*"]; ok {
		return PermissionLevel(perm) >= PermissionLevel(requiredPermission)
	}

	for _, ch := range claims.Channels {
		if ch == channel || ch == "*" {
			return true
		}
	}

	return false
}

// PermissionLevel returns a numeric level for permission comparison.
func PermissionLevel(perm string) int {
	switch strings.ToLower(perm) {
	case "read":
		return 1
	case "write":
		return 2
	case "admin":
		return 3
	default:
		return 0
	}
}
