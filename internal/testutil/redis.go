package testutil

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// RedisBundle bundles together a miniredis instance and an associated Redis
// client.
type RedisBundle struct {
	mr *miniredis.Miniredis
	rc *redis.Client
}

// MustCreateRedisBundle returns a new RedisBundle.
func MustCreateRedisBundle(t *testing.T) *RedisBundle {
	mr := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	return &RedisBundle{mr: mr, rc: rc}
}

// Client returns the Redis client.
func (rb *RedisBundle) Client() *redis.Client {
	return rb.rc
}

// Flush flushes all keys from miniredis.
func (rb *RedisBundle) Flush() {
	rb.mr.FlushAll()
}

// Close shuts down the Redis client and miniredis instance.
func (rb *RedisBundle) Close() {
	rb.rc.Close()
	rb.mr.Close()
}
