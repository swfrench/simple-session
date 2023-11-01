package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/redis/go-redis/v9"
	"github.com/swfrench/simple-session/store"
)

type fakeSession struct {
	SID string `json:"sid"`
}

const (
	fakeSessionData = `{"sid":"boop"}`
	fakeSessionID   = "boop"
	fakeSessionKey  = "session:boop"
)

type storeBundle struct {
	mr *miniredis.Miniredis
	rc *redis.Client
	rs *store.RedisStore[fakeSession]
}

func mustCreateStoreBundle(t *testing.T) *storeBundle {
	mr := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{
		Addr:     mr.Addr(),
		Password: "",
		DB:       0,
	})
	rs := store.NewRedisStore[fakeSession](rc, "session")
	return &storeBundle{mr: mr, rc: rc, rs: rs}
}

func (sb *storeBundle) close() {
	sb.rc.Close()
	sb.mr.Close()
}

func TestRedisStoreGet(t *testing.T) {
	testCases := []struct {
		name    string
		arrange func(t *testing.T, rc *redis.Client)
		get     func(s *store.RedisStore[fakeSession]) (*fakeSession, error)
		want    *fakeSession
		err     error
	}{
		{
			name: "found",
			arrange: func(t *testing.T, rc *redis.Client) {
				if err := rc.Set(context.Background(), fakeSessionKey, []byte(fakeSessionData), 0).Err(); err != nil {
					t.Fatalf("Unexpected error initializing Redis: %v", err)
				}
			},
			get: func(s *store.RedisStore[fakeSession]) (*fakeSession, error) {
				return s.Get(context.Background(), fakeSessionID)
			},
			want: &fakeSession{SID: fakeSessionID},
		},
		{
			name: "not found",
			arrange: func(t *testing.T, rc *redis.Client) {
				if err := rc.Set(context.Background(), fakeSessionKey, []byte(fakeSessionData), 0).Err(); err != nil {
					t.Fatalf("Unexpected error initializing Redis: %v", err)
				}
			},
			get: func(s *store.RedisStore[fakeSession]) (*fakeSession, error) {
				return s.Get(context.Background(), "beep")
			},
			err: store.ErrSessionNotFound,
		},
		{
			name: "malformed",
			arrange: func(t *testing.T, rc *redis.Client) {
				if err := rc.Set(context.Background(), fakeSessionKey, []byte(`invalid`), 0).Err(); err != nil {
					t.Fatalf("Unexpected error initializing Redis: %v", err)
				}
			},
			get: func(s *store.RedisStore[fakeSession]) (*fakeSession, error) {
				return s.Get(context.Background(), fakeSessionID)
			},
			err: store.ErrInvalidStoredSessionData,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sb := mustCreateStoreBundle(t)
			defer sb.close()
			tc.arrange(t, sb.rc)
			fs, err := tc.get(sb.rs)
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

func TestRedisStoreSet(t *testing.T) {
	testCases := []struct {
		name    string
		arrange func(t *testing.T, rc *redis.Client)
		set     func(s *store.RedisStore[fakeSession]) error
		assert  func(t *testing.T, rc *redis.Client)
		err     error
	}{
		{
			name: "succeeds",
			arrange: func(t *testing.T, rc *redis.Client) {
			},
			set: func(s *store.RedisStore[fakeSession]) error {
				return s.Set(context.Background(), fakeSessionID, &fakeSession{SID: fakeSessionID}, time.Hour)
			},
			assert: func(t *testing.T, rc *redis.Client) {
				r := rc.Get(context.Background(), fakeSessionKey)
				if r.Err() != nil {
					t.Errorf("Get() returned unexpected error during verification: %v", r.Err())
				} else if diff := cmp.Diff(fakeSessionData, r.Val()); diff != "" {
					t.Errorf("Get() returned unexpected value during verification (+got, -want):\n%s", diff)
				}
			},
		},
		{
			name: "exists",
			arrange: func(t *testing.T, rc *redis.Client) {
				if err := rc.Set(context.Background(), fakeSessionKey, []byte(fakeSessionData), 0).Err(); err != nil {
					t.Fatalf("Unexpected error initializing Redis: %v", err)
				}
			},
			set: func(s *store.RedisStore[fakeSession]) error {
				return s.Set(context.Background(), fakeSessionID, &fakeSession{SID: fakeSessionID}, time.Hour)
			},
			err: store.ErrSessionExists,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sb := mustCreateStoreBundle(t)
			defer sb.close()
			tc.arrange(t, sb.rc)
			err := tc.set(sb.rs)
			if gotErr, wantErr := err != nil, tc.err != nil; gotErr != wantErr {
				t.Fatalf("Set() returned unexpected error - got error: %t, want error: %t", gotErr, wantErr)
			}
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("Set() returned unexpected error type - got: %v, want: %v", err, tc.err)
				}
				return
			}
			tc.assert(t, sb.rc)
		})
	}
}

func TestRedisStoreDel(t *testing.T) {
	testCases := []struct {
		name    string
		arrange func(t *testing.T, rc *redis.Client)
		del     func(s *store.RedisStore[fakeSession]) error
		err     error
	}{
		{
			name: "found",
			arrange: func(t *testing.T, rc *redis.Client) {
				if err := rc.Set(context.Background(), fakeSessionKey, []byte(fakeSessionData), 0).Err(); err != nil {
					t.Fatalf("Unexpected error initializing Redis: %v", err)
				}
			},
			del: func(s *store.RedisStore[fakeSession]) error {
				return s.Del(context.Background(), fakeSessionID)
			},
		},
		{
			name: "not found",
			arrange: func(t *testing.T, rc *redis.Client) {
				if err := rc.Set(context.Background(), fakeSessionKey, []byte(fakeSessionData), 0).Err(); err != nil {
					t.Fatalf("Unexpected error initializing Redis: %v", err)
				}
			},
			del: func(s *store.RedisStore[fakeSession]) error {
				return s.Del(context.Background(), "beep")
			},
			err: store.ErrSessionNotFound,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sb := mustCreateStoreBundle(t)
			defer sb.close()
			tc.arrange(t, sb.rc)
			err := tc.del(sb.rs)
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
