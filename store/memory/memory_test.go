package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/swfrench/simple-session/store"
	"github.com/swfrench/simple-session/store/memory"
)

type fakeSession struct {
	SID string `json:"sid"`
}

const fakeSessionID = "boop"

func fakeSessionValue() *fakeSession {
	return &fakeSession{SID: "boop"}
}

func fakeSessionValueNew() *fakeSession {
	return &fakeSession{SID: "booop"}
}

func TestMemoryStoreGet(t *testing.T) {
	testCases := []struct {
		name    string
		arrange func(t *testing.T, s *memory.Store[fakeSession])
		get     func(s *memory.Store[fakeSession]) (*fakeSession, error)
		want    *fakeSession
		err     error
	}{
		{
			name: "found",
			arrange: func(t *testing.T, s *memory.Store[fakeSession]) {
				if err := s.Set(context.Background(), fakeSessionID, fakeSessionValue(), time.Hour); err != nil {
					t.Fatalf("Unexpected error initializing memory store: %v", err)
				}
			},
			get: func(s *memory.Store[fakeSession]) (*fakeSession, error) {
				return s.Get(context.Background(), fakeSessionID)
			},
			want: fakeSessionValue(),
		},
		{
			name: "not found",
			arrange: func(t *testing.T, s *memory.Store[fakeSession]) {
				if err := s.Set(context.Background(), fakeSessionID, fakeSessionValue(), time.Hour); err != nil {
					t.Fatalf("Unexpected error initializing memory store: %v", err)
				}
			},
			get: func(s *memory.Store[fakeSession]) (*fakeSession, error) {
				return s.Get(context.Background(), "beep")
			},
			err: store.ErrSessionNotFound,
		},
		{
			name: "not found evicted",
			arrange: func(t *testing.T, s *memory.Store[fakeSession]) {
				now := time.Now()
				if err := s.Set(context.Background(), fakeSessionID, fakeSessionValue(), time.Hour); err != nil {
					t.Fatalf("Unexpected error initializing memory store: %v", err)
				}
				s.Clock = func() time.Time { return now.Add(90 * time.Minute) }
			},
			get: func(s *memory.Store[fakeSession]) (*fakeSession, error) {
				return s.Get(context.Background(), fakeSessionID)
			},
			err: store.ErrSessionNotFound,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ms := memory.New[fakeSession]()
			tc.arrange(t, ms)
			fs, err := tc.get(ms)
			if gotErr, wantErr := err != nil, tc.err != nil; gotErr != wantErr {
				t.Fatalf("Get() returned unexpected error - got error: %t, want error: %t", gotErr, wantErr)
			}
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("Get() returned unexpected error type - got: %v, want: %v", err, tc.err)
				}
				return
			}
			if diff := cmp.Diff(tc.want, fs); diff != "" {
				t.Errorf("Get() returned incorrect content (+got, -want):\n%s", diff)
			}
		})
	}
}

func TestMemoryStoreSet(t *testing.T) {
	testCases := []struct {
		name    string
		arrange func(t *testing.T, s *memory.Store[fakeSession])
		set     func(s *memory.Store[fakeSession]) error
		assert  func(t *testing.T, s *memory.Store[fakeSession])
		err     error
	}{
		{
			name: "succeeds",
			arrange: func(t *testing.T, s *memory.Store[fakeSession]) {
			},
			set: func(s *memory.Store[fakeSession]) error {
				return s.Set(context.Background(), fakeSessionID, fakeSessionValue(), time.Hour)
			},
			assert: func(t *testing.T, s *memory.Store[fakeSession]) {
				val, err := s.Get(context.Background(), fakeSessionID)
				if err != nil {
					t.Errorf("Get() returned unexpected error during verification: %v", err)
				} else if diff := cmp.Diff(fakeSessionValue(), val); diff != "" {
					t.Errorf("Get() returned unexpected value during verification (+got, -want):\n%s", diff)
				}
			},
		},
		{
			name: "exists",
			arrange: func(t *testing.T, s *memory.Store[fakeSession]) {
				if err := s.Set(context.Background(), fakeSessionID, fakeSessionValue(), time.Hour); err != nil {
					t.Fatalf("Unexpected error initializing memory store: %v", err)
				}
			},
			set: func(s *memory.Store[fakeSession]) error {
				return s.Set(context.Background(), fakeSessionID, fakeSessionValue(), time.Hour)
			},
			err: store.ErrSessionExists,
		},
		{
			name: "succeeds evicted",
			arrange: func(t *testing.T, s *memory.Store[fakeSession]) {
				now := time.Now()
				if err := s.Set(context.Background(), fakeSessionID, fakeSessionValue(), time.Hour); err != nil {
					t.Fatalf("Unexpected error initializing memory store: %v", err)
				}
				s.Clock = func() time.Time { return now.Add(90 * time.Minute) }
			},
			set: func(s *memory.Store[fakeSession]) error {
				return s.Set(context.Background(), fakeSessionID, fakeSessionValueNew(), time.Hour)
			},
			assert: func(t *testing.T, s *memory.Store[fakeSession]) {
				val, err := s.Get(context.Background(), fakeSessionID)
				if err != nil {
					t.Errorf("Get() returned unexpected error during verification: %v", err)
				} else if diff := cmp.Diff(fakeSessionValueNew(), val); diff != "" {
					t.Errorf("Get() returned unexpected value during verification (+got, -want):\n%s", diff)
				}
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ms := memory.New[fakeSession]()
			tc.arrange(t, ms)
			err := tc.set(ms)
			if gotErr, wantErr := err != nil, tc.err != nil; gotErr != wantErr {
				t.Fatalf("Set() returned unexpected error - got error: %t, want error: %t", gotErr, wantErr)
			}
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("Set() returned unexpected error type - got: %v, want: %v", err, tc.err)
				}
				return
			}
			tc.assert(t, ms)
		})
	}
}

func TestMemoryStoreDel(t *testing.T) {
	testCases := []struct {
		name    string
		arrange func(t *testing.T, s *memory.Store[fakeSession])
		del     func(s *memory.Store[fakeSession]) error
		err     error
	}{
		{
			name: "found",
			arrange: func(t *testing.T, s *memory.Store[fakeSession]) {
				if err := s.Set(context.Background(), fakeSessionID, fakeSessionValue(), time.Hour); err != nil {
					t.Fatalf("Unexpected error initializing memory store: %v", err)
				}
			},
			del: func(s *memory.Store[fakeSession]) error {
				return s.Del(context.Background(), fakeSessionID)
			},
		},
		{
			name: "not found",
			arrange: func(t *testing.T, s *memory.Store[fakeSession]) {
				if err := s.Set(context.Background(), fakeSessionID, fakeSessionValue(), time.Hour); err != nil {
					t.Fatalf("Unexpected error initializing memory store: %v", err)
				}
			},
			del: func(s *memory.Store[fakeSession]) error {
				return s.Del(context.Background(), "beep")
			},
			err: store.ErrSessionNotFound,
		},
		{
			name: "not found evicted",
			arrange: func(t *testing.T, s *memory.Store[fakeSession]) {
				now := time.Now()
				if err := s.Set(context.Background(), fakeSessionID, fakeSessionValue(), time.Hour); err != nil {
					t.Fatalf("Unexpected error initializing memory store: %v", err)
				}
				s.Clock = func() time.Time { return now.Add(90 * time.Minute) }
			},
			del: func(s *memory.Store[fakeSession]) error {
				return s.Del(context.Background(), fakeSessionID)
			},
			err: store.ErrSessionNotFound,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ms := memory.New[fakeSession]()
			tc.arrange(t, ms)
			err := tc.del(ms)
			if gotErr, wantErr := err != nil, tc.err != nil; gotErr != wantErr {
				t.Fatalf("Del() returned unexpected error - got error: %t, want error: %t", gotErr, wantErr)
			}
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Errorf("Del() returned unexpected error type - got: %v, want: %v", err, tc.err)
				}
			}
		})
	}
}
