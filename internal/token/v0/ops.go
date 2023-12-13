// Package v0 implements operations supporting the v0 token version.
package v0

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/swfrench/simple-session/internal/token/common"
)

// Version is the version identifier prefix for this token implementation.
const Version = "v0"

// v0 tokens are defined by:
// * MAC: HMAC-SHA256
// * Format:
//     <version><VersionHeaderSeparator><base64url payload>.<base64url MAC>
//     [<--  "message" over which the MAC is computed  -->]

const macFooterSeparator = "."

// Create returns an authenticated token string for the provided payload byte
// sequence (e.g., an arbitrary identifier).
func Create(key []byte, data []byte) string {
	msg := fmt.Sprintf("%s%s%s", Version, common.VersionHeaderSeparator, base64.URLEncoding.EncodeToString(data))
	h := hmac.New(sha256.New, key)
	h.Write([]byte(msg))
	return fmt.Sprintf("%s%s%s", msg, macFooterSeparator, base64.URLEncoding.EncodeToString(h.Sum(nil)))
}

// Length of a base64-encoded 32 byte MAC.
const base64MACLen = 44

var (
	errNotFound  = errors.New("separator not found")
	errNotUnique = errors.New("separator not unique")
)

func uniqueIndex(s, sub string) (int, error) {
	i := strings.Index(s, sub)
	if i == -1 {
		return i, errNotFound
	}
	if strings.Contains(s[i+1:], sub) {
		return i, errNotUnique
	}
	return i, nil
}

// Verify checks the authenticity of the provided token given the associated key
// and extracts the payload byte sequence therein.
func Verify(key []byte, token string) ([]byte, error) {
	i, err := uniqueIndex(token, common.VersionHeaderSeparator)
	if err != nil {
		return nil, fmt.Errorf("failed to parse version header from raw token (error: %v): %w", err, common.ErrBadToken)
	}
	if token[:i] != Version {
		return nil, fmt.Errorf("failed to parse raw token: %w", common.ErrUnsupportedVersion)
	}
	j, err := uniqueIndex(token, macFooterSeparator)
	if err != nil {
		return nil, fmt.Errorf("failed to parse MAC footer from raw token (error: %v): %w", err, common.ErrBadToken)
	}
	if len(token)-j != base64MACLen+1 {
		return nil, fmt.Errorf("failed to parse raw token (incorrect MAC footer length): %w", common.ErrBadToken)
	}
	mac, err := base64.URLEncoding.DecodeString(token[j+1:])
	if err != nil {
		return nil, fmt.Errorf("failed to decode MAC footer (error: %v): %w", err, common.ErrBadToken)
	}
	h := hmac.New(sha256.New, key)
	h.Write([]byte(token[:j]))
	if dmac := h.Sum(nil); !hmac.Equal(dmac, mac) {
		return nil, fmt.Errorf("token MAC verification failed: %w", common.ErrInvalidToken)
	}
	data, err := base64.URLEncoding.DecodeString(token[i+1 : j])
	if err != nil {
		return nil, fmt.Errorf("failed to decode data segment (error: %v): %w", err, common.ErrBadToken)
	}
	return data, nil
}
