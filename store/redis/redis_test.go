package redis_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/go-cmp/cmp"
	goredis "github.com/redis/go-redis/v9"
	"github.com/swfrench/simple-session/store"
	"github.com/swfrench/simple-session/store/redis"
)

type fakeSession struct {
	SID string `json:"sid"`
}

const (
	fakeSessionData = `{"sid":"boop"}`
	fakeSessionID   = "boop"
	fakeSessionKey  = "session:boop"
)

func fakeSessionValue() *fakeSession {
	return &fakeSession{SID: "boop"}
}

type redisStoreBundle struct {
	mr *miniredis.Miniredis
	rc *goredis.Client
	rs *redis.Store[fakeSession]
}

func mustCreateStoreBundle(t *testing.T) *redisStoreBundle {
	mr := miniredis.RunT(t)
	rc := goredis.NewClient(&goredis.Options{
		Addr:     mr.Addr(),
		Password: "",
		DB:       0,
	})
	rs := redis.New[fakeSession](rc, "session")
	return &redisStoreBundle{mr: mr, rc: rc, rs: rs}
}

func (sb *redisStoreBundle) close() {
	sb.rc.Close()
	sb.mr.Close()
}

func TestStoreGet(t *testing.T) {
	testCases := []struct {
		name    string
		arrange func(t *testing.T, rc *goredis.Client)
		get     func(s *redis.Store[fakeSession]) (*fakeSession, error)
		want    *fakeSession
		err     error
	}{
		{
			name: "found",
			arrange: func(t *testing.T, rc *goredis.Client) {
				if err := rc.Set(context.Background(), fakeSessionKey, []byte(fakeSessionData), 0).Err(); err != nil {
					t.Fatalf("Unexpected error initializing Redis: %v", err)
				}
			},
			get: func(s *redis.Store[fakeSession]) (*fakeSession, error) {
				return s.Get(context.Background(), fakeSessionID)
			},
			want: fakeSessionValue(),
		},
		{
			name: "not found",
			arrange: func(t *testing.T, rc *goredis.Client) {
				if err := rc.Set(context.Background(), fakeSessionKey, []byte(fakeSessionData), 0).Err(); err != nil {
					t.Fatalf("Unexpected error initializing Redis: %v", err)
				}
			},
			get: func(s *redis.Store[fakeSession]) (*fakeSession, error) {
				return s.Get(context.Background(), "beep")
			},
			err: store.ErrSessionNotFound,
		},
		{
			name: "malformed",
			arrange: func(t *testing.T, rc *goredis.Client) {
				if err := rc.Set(context.Background(), fakeSessionKey, []byte(`invalid`), 0).Err(); err != nil {
					t.Fatalf("Unexpected error initializing Redis: %v", err)
				}
			},
			get: func(s *redis.Store[fakeSession]) (*fakeSession, error) {
				return s.Get(context.Background(), fakeSessionID)
			},
			err: store.ErrInvalidStoredSessionData,
		},
		{
			name: "redis error",
			arrange: func(t *testing.T, rc *goredis.Client) {
				rc.Close()
			},
			get: func(s *redis.Store[fakeSession]) (*fakeSession, error) {
				return s.Get(context.Background(), fakeSessionID)
			},
			err: redis.ErrRedisClient,
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

func TestStoreSet(t *testing.T) {
	testCases := []struct {
		name    string
		arrange func(t *testing.T, rc *goredis.Client)
		set     func(s *redis.Store[fakeSession]) error
		assert  func(t *testing.T, rc *goredis.Client)
		err     error
	}{
		{
			name: "succeeds",
			arrange: func(t *testing.T, rc *goredis.Client) {
			},
			set: func(s *redis.Store[fakeSession]) error {
				return s.Set(context.Background(), fakeSessionID, fakeSessionValue(), time.Hour)
			},
			assert: func(t *testing.T, rc *goredis.Client) {
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
			arrange: func(t *testing.T, rc *goredis.Client) {
				if err := rc.Set(context.Background(), fakeSessionKey, []byte(fakeSessionData), 0).Err(); err != nil {
					t.Fatalf("Unexpected error initializing Redis: %v", err)
				}
			},
			set: func(s *redis.Store[fakeSession]) error {
				return s.Set(context.Background(), fakeSessionID, fakeSessionValue(), time.Hour)
			},
			err: store.ErrSessionExists,
		},
		{
			name: "redis error",
			arrange: func(t *testing.T, rc *goredis.Client) {
				rc.Close()
			},
			set: func(s *redis.Store[fakeSession]) error {
				return s.Set(context.Background(), fakeSessionID, fakeSessionValue(), time.Hour)
			},
			err: redis.ErrRedisClient,
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

func TestStoreDel(t *testing.T) {
	testCases := []struct {
		name    string
		arrange func(t *testing.T, rc *goredis.Client)
		del     func(s *redis.Store[fakeSession]) error
		err     error
	}{
		{
			name: "found",
			arrange: func(t *testing.T, rc *goredis.Client) {
				if err := rc.Set(context.Background(), fakeSessionKey, []byte(fakeSessionData), 0).Err(); err != nil {
					t.Fatalf("Unexpected error initializing Redis: %v", err)
				}
			},
			del: func(s *redis.Store[fakeSession]) error {
				return s.Del(context.Background(), fakeSessionID)
			},
		},
		{
			name: "not found",
			arrange: func(t *testing.T, rc *goredis.Client) {
				if err := rc.Set(context.Background(), fakeSessionKey, []byte(fakeSessionData), 0).Err(); err != nil {
					t.Fatalf("Unexpected error initializing Redis: %v", err)
				}
			},
			del: func(s *redis.Store[fakeSession]) error {
				return s.Del(context.Background(), "beep")
			},
			err: store.ErrSessionNotFound,
		},
		{
			name: "redis error",
			arrange: func(t *testing.T, rc *goredis.Client) {
				rc.Close()
			},
			del: func(s *redis.Store[fakeSession]) error {
				return s.Del(context.Background(), "beep")
			},
			err: redis.ErrRedisClient,
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
