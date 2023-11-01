package testutil

import (
	"encoding/base64"
	"testing"
)

// MustDecodeBase64 decodes the provided base64-encoded string.
func MustDecodeBase64(t *testing.T, encoded string) []byte {
	bs, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("Unexpected error decoding %q: %v", encoded, err)
	}
	return bs
}
