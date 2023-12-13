// Package token provides utilities for creation and verification of
// authenticated token strings.
package token

import (
	"strings"

	"github.com/swfrench/simple-session/internal/token/common"
	v0 "github.com/swfrench/simple-session/internal/token/v0"
)

// Authenticator manages authenticated token strings.
type Authenticator struct {
	key []byte
}

// NewAuthenticator returns a new Authenticator instance using the provided key
// to compute token MACs.
func NewAuthenticator(key []byte) *Authenticator {
	return &Authenticator{key: key}
}

// Create returns an authenticated token string for the provided payload byte
// sequence (e.g., an arbitrary identifier).
func (a *Authenticator) Create(data []byte) string {
	return v0.Create(a.key, data)
}

// The only supported token version at this time is v0.
const v0Header = v0.Version + common.VersionHeaderSeparator

// Verify checks the authenticity of the provided token and extracts the payload
// byte sequence therein.
func (a *Authenticator) Verify(token string) ([]byte, error) {
	if strings.HasPrefix(token, v0Header) {
		return v0.Verify(a.key, token)
	}
	return nil, common.ErrUnsupportedVersion
}
