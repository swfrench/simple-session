package v0_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/swfrench/simple-session/internal/testutil"
	"github.com/swfrench/simple-session/internal/token/common"
	v0 "github.com/swfrench/simple-session/internal/token/v0"
)

const testKey = "FjcKOUT10xuBXjijEMv/UvegOFPtu55WvvS3ChkcyL0="

func TestCreate(t *testing.T) {
	k := testutil.MustDecodeBase64(t, testKey)
	testCases := []struct {
		name string
		data []byte
		want string
	}{
		{
			name: "non-empty data",
			data: []byte("hello"),
			want: "v0!aGVsbG8=.qNUfnzeKEil4dWAVjlDGU-ctorElKvIF4_tGEstbK80=",
		},
		{
			name: "empty data",
			data: []byte{},
			want: "v0!.I-PfF4FjpVjMMLpczCmxUZTgR_fVrtQ9FUSYTX5zgJY=",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := v0.Create(k, tc.data); got != tc.want {
				t.Errorf("Create(%v, %v) returned incorrect token: got: %q, want: %q", k, tc.data, got, tc.want)
			}
		})
	}
}

func TestVerify(t *testing.T) {
	k := testutil.MustDecodeBase64(t, testKey)
	testCases := []struct {
		name  string
		token string
		want  []byte
		err   error
	}{
		{
			name:  "non-empty data",
			token: "v0!aGVsbG8=.qNUfnzeKEil4dWAVjlDGU-ctorElKvIF4_tGEstbK80=",
			want:  []byte("hello"),
		},
		{
			name:  "empty data",
			token: "v0!.I-PfF4FjpVjMMLpczCmxUZTgR_fVrtQ9FUSYTX5zgJY=",
			want:  []byte{},
		},
		{
			name:  "unsupported version",
			token: "v42!aGVsbG8=.qNUfnzeKEil4dWAVjlDGU-ctorElKvIF4_tGEstbK80=",
			err:   common.ErrUnsupportedVersion,
		},
		{
			name:  "no version",
			token: "aGVsbG8=.qNUfnzeKEil4dWAVjlDGU-ctorElKvIF4_tGEstbK80=",
			err:   common.ErrBadToken,
		},
		{
			name:  "extraneous version",
			token: "v0!v0!aGVsbG8=.qNUfnzeKEil4dWAVjlDGU-ctorElKvIF4_tGEstbK80=",
			err:   common.ErrBadToken,
		},
		{
			name:  "no MAC",
			token: "v0!aGVsbG8=",
			err:   common.ErrBadToken,
		},
		{
			name:  "extraneous MAC",
			token: "v0!aGVsbG8=.qNUfnzeKEil4dWAVjlDGU-ctorElKvIF4_tGEstbK80=.qNUfnzeKEil4dWAVjlDGU-ctorElKvIF4_tGEstbK80=",
			err:   common.ErrBadToken,
		},
		{
			name:  "truncated MAC",
			token: "v0!aGVsbG8=.qNUfnzeKEil4dWAVjlDG",
			err:   common.ErrBadToken,
		},
		{
			name:  "invalid MAC",
			token: "v0!aGVsbG8=.*NUfnzeKEil4dWAVjlDGU-ctorElKvIF4_tGEstbK80=",
			err:   common.ErrBadToken,
		},
		{
			name:  "invalid payload",
			token: "v0!*GVsbG8=.C1gdvMLdOajQT-xapWTEIJTvcknhM0a5CSPbd4N2-P8=",
			err:   common.ErrBadToken,
		},
		{
			name:  "MAC mismatch",
			token: "v0!bGVsbG8=.qNUfnzeKEil4dWAVjlDGU-ctorElKvIF4_tGEstbK80=",
			// bitflip ^
			err: common.ErrInvalidToken,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := v0.Verify(k, tc.token)
			if gotErr, wantErr := err != nil, tc.err != nil; gotErr != wantErr {
				t.Errorf("Verify(%v, %q) returned incorrect error status: got: %v, want: %v", k, tc.token, err, tc.err)
			}
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Errorf("Verify(%v, %q) returned incorrect error type: got: %v, want: %v", k, tc.token, err, tc.err)
				}
				return
			}
			if !bytes.Equal(got, tc.want) {
				t.Errorf("Verify(%v, %q) returned incorrect token: got: %v, want: %v", k, tc.token, got, tc.want)
			}
		})
	}

}
