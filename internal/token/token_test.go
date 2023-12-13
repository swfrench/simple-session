package token_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/swfrench/simple-session/internal/testutil"
	"github.com/swfrench/simple-session/internal/token"
	"github.com/swfrench/simple-session/internal/token/common"
)

func TestAuthenticator(t *testing.T) {
	ta := token.NewAuthenticator(testutil.MustDecodeBase64(t, "FjcKOUT10xuBXjijEMv/UvegOFPtu55WvvS3ChkcyL0="))
	testCases := []struct {
		name  string
		token string
		want  []byte
		err   error
	}{
		{
			name:  "create and verify",
			token: ta.Create([]byte("hello")),
			want:  []byte("hello"),
		},
		{
			name:  "unsupported version",
			token: "v42!aGVsbG8=.qNUfnzeKEil4dWAVjlDGU-ctorElKvIF4_tGEstbK80=",
			err:   common.ErrUnsupportedVersion,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ta.Verify(tc.token)
			if gotErr, wantErr := err != nil, tc.err != nil; gotErr != wantErr {
				t.Fatalf("Verify(%q) returned incorrect error status: got: %v want: %v", tc.token, err, tc.err)
			}
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Errorf("Verify(%q) returned incorrect error type: got: %v want: %v", tc.token, err, tc.err)
				}
				return
			}
			if !bytes.Equal(got, tc.want) {
				t.Errorf("Verify(%q) returned incorrect byte sequence: got: %v want: %v", tc.token, got, tc.want)
			}
		})
	}
}
