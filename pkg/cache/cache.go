// Package cache is the read cache and the namespacing convention over Redis (RF-10).
//
// What belongs here is data that is expensive to compute and cheap to be slightly stale:
// the catalog, entitlements. What does not belong here is anything whose staleness is a
// correctness problem -- a session revocation list is not a cache, it is state, and it
// lives in Redis directly rather than behind this interface.
package cache

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned by Get when the key is absent or expired.
//
// A cache miss is an expected outcome, not a failure: callers branch on it with errors.Is
// and fall through to the source of truth.
var ErrNotFound = errors.New("cache: key not found")

// CacheService is the contract the services depend on (RF-10).
//
//nolint:revive // the name is fixed by RF-10 of PRD-00 and is used across every service.
type CacheService interface {
	// Get decodes the value stored at key into dest, which must be a pointer.
	// It returns ErrNotFound on a miss.
	Get(ctx context.Context, key string, dest any) error

	// Set stores value serialized as JSON, with a time to live. A ttl of zero means the
	// entry does not expire, which should be rare and deliberate.
	Set(ctx context.Context, key string, value any, ttl time.Duration) error

	// Delete removes one or more keys. Deleting a key that does not exist is not an error.
	Delete(ctx context.Context, keys ...string) error

	// DeleteByPrefix removes every key under a prefix. It is how a whole namespace is
	// invalidated, such as every entry of a tour whose version changed.
	DeleteByPrefix(ctx context.Context, prefix string) error

	// Ping checks the connection. It backs the readiness probe.
	Ping(ctx context.Context) error

	// Close releases the connection.
	Close() error
}
