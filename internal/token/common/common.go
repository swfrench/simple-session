package common

import "errors"

// VersionHeaderSeparator is the separator between the token version identifier
// prefix and token body.
// Tokens are versioned by their backing implemention, which includes details
// such as token structure and authentication scheme.
const VersionHeaderSeparator = "!"

var (
	// ErrUnsupportedVersion indicates that the version identifier embedded in
	// the token string is not supported by this implementation.
	ErrUnsupportedVersion = errors.New("unsupported version")
	// ErrBadToken indicates that the token string is structurally invalid.
	ErrBadToken = errors.New("bad token")
	// ErrInvalidToken indicates that the token string fails authenticity checks.
	ErrInvalidToken = errors.New("invalid token")
)
