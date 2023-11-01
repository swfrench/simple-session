// Package token provides utilities for working with HMAC authenticated token
// strings.
package token

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
)

// TokenAuthenticator manages HMAC-SHA256 authenticated token strings. Token
// strings are concatenations of base64url encoded values (separated by '.').
type TokenAuthenticator struct {
	key []byte
}

// NewTokenAuthenticator returns a new TokenAuthenticator instance using the
// provided HMAC-SHA256 key.
func NewTokenAuthenticator(key []byte) *TokenAuthenticator {
	return &TokenAuthenticator{key: key}
}

// Create returns an authenticated token string for the provided payload byte
// sequence (e.g., an arbitrary identifier).
func (ta *TokenAuthenticator) Create(data []byte) string {
	h := hmac.New(sha256.New, ta.key)
	h.Write(data)
	return fmt.Sprintf("%s.%s",
		base64.URLEncoding.EncodeToString(h.Sum(nil)),
		base64.URLEncoding.EncodeToString(data))
}

var (
	// ErrBadToken indicates that the token string is structurally invalid.
	ErrBadToken = errors.New("bad token")
	// ErrInvalidToken indicates that the token string fails authenticity checks.
	ErrInvalidToken = errors.New("invalid token")
)

// Length of a base64-encoded 32 byte MAC.
const base64MACLen = 44

// Verify checks the authenticity of the provided token and extracts the payload
// byte sequence therein.
func (ta *TokenAuthenticator) Verify(token string) ([]byte, error) {
	if len(token) < base64MACLen+1 {
		return nil, fmt.Errorf("failed to parse raw token (too short): %w", ErrBadToken)
	}
	if token[base64MACLen] != '.' {
		return nil, fmt.Errorf("failed to parse raw token (incorrect separator): %w", ErrBadToken)
	}
	mac, err := base64.URLEncoding.DecodeString(token[:base64MACLen])
	if err != nil {
		return nil, fmt.Errorf("failed to decode MAC segment (error: %v): %w", err, ErrBadToken)
	}
	data, err := base64.URLEncoding.DecodeString(token[base64MACLen+1:])
	if err != nil {
		return nil, fmt.Errorf("failed to decode data segment (error: %v): %w", err, ErrBadToken)
	}
	h := hmac.New(sha256.New, ta.key)
	h.Write(data)
	if dmac := h.Sum(nil); !hmac.Equal(dmac, mac) {
		return nil, fmt.Errorf("token MAC verification failed: %w", ErrInvalidToken)
	}
	return data, nil
}
