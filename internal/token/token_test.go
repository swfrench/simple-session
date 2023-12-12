package token_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/swfrench/simple-session/internal/testutil"
	"github.com/swfrench/simple-session/internal/token"
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
			name:  "basic",
			token: ta.Create([]byte("hello")),
			want:  []byte("hello"),
		},
		{
			name:  "empty data",
			token: ta.Create([]byte{}),
			want:  []byte{},
		},
		{
			name:  "bad length",
			token: "fqyW83c2iDA=.aGVsbG8=",
			err:   token.ErrBadToken,
		},
		{
			name:  "bad separator",
			token: "w3JvhP0BiPuA-o2DiO3vK_V0ue_mY3miHY8p-8YJo90=!aGVsbG8=",
			err:   token.ErrBadToken,
		},
		{
			name:  "bad mac segment encoding",
			token: "********************************************.hcsmQ6zRX6A=",
			err:   token.ErrBadToken,
		},
		{
			name:  "bad data segment encoding",
			token: "ttuK2tNow-JZ6Bau92G819jtBqCo90G0ud2QAuxaeKc=.************",
			err:   token.ErrBadToken,
		},
		{
			name:  "invalid",
			token: "ttuK2tNow-JZ6Bau92G819jtBqCo90G0ud2QAuxaeKc=.hcsmQ6zRX6A=",
			err:   token.ErrInvalidToken,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ta.Verify(tc.token)
			if gotErr, wantErr := err != nil, tc.err != nil; gotErr != wantErr {
				t.Fatalf("Verify() returned incorrect error status - got: %v want: %v", err, tc.err)
			}
			if err != nil {
				if !errors.Is(err, tc.err) {
					t.Errorf("Verify() returned incorrect error type - got: %v want: %v", err, tc.err)
				}
				return
			}
			if !bytes.Equal(got, tc.want) {
				t.Errorf("Verify() returned incorrect byte sequence - got: %v want: %v", got, tc.want)
			}
		})
	}
}
