package cache_test

import (
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/nicodanke/dizen-v2-backend/pkg/cache"
)

// tour is a value with the shape of something worth caching.
type tour struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Version int    `json:"version"`
}

// newCache starts an in-memory Redis and returns a cache pointing at it. miniredis is used
// rather than a container because these tests exercise the cache logic, not Redis; the
// integration tests against a real server arrive with RF-18.
func newCache(t *testing.T) (*cache.RedisCache, *miniredis.Miniredis) {
	t.Helper()

	server := miniredis.RunT(t)

	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	return cache.NewRedisWithClient(client, cache.Config{DefaultTTL: time.Minute}, "tours"), server
}

func TestSetThenGetRoundTrips(t *testing.T) {
	t.Parallel()

	c, _ := newCache(t)

	want := tour{ID: "t1", Title: "Recoleta", Version: 3}

	if err := c.Set(t.Context(), "tours:tour:t1:v3", want, time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}

	var got tour
	if err := c.Get(t.Context(), "tours:tour:t1:v3", &got); err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// A miss is an expected outcome, not a failure: callers branch on it and fall through to
// the source of truth.
func TestGetOnAMissingKeyReturnsErrNotFound(t *testing.T) {
	t.Parallel()

	c, _ := newCache(t)

	var got tour

	err := c.Get(t.Context(), "tours:tour:missing:v1", &got)
	if !errors.Is(err, cache.ErrNotFound) {
		t.Errorf("the error is not ErrNotFound: %v", err)
	}
}

func TestAnExpiredKeyIsAMiss(t *testing.T) {
	t.Parallel()

	c, server := newCache(t)

	if err := c.Set(t.Context(), "k", tour{ID: "t1"}, 50*time.Millisecond); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// miniredis does not advance time on its own, which makes expiry deterministic.
	server.FastForward(100 * time.Millisecond)

	var got tour
	if err := c.Get(t.Context(), "k", &got); !errors.Is(err, cache.ErrNotFound) {
		t.Errorf("the expired key did not come back as a miss: %v", err)
	}
}

func TestATTLOfZeroUsesTheDefault(t *testing.T) {
	t.Parallel()

	c, server := newCache(t)

	if err := c.Set(t.Context(), "k", tour{ID: "t1"}, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	ttl := server.TTL("k")

	if ttl <= 0 {
		t.Fatal("the key was stored with no expiry: the default TTL was not applied")
	}

	if ttl > time.Minute {
		t.Errorf("TTL = %s, want the default of one minute", ttl)
	}
}

func TestDelete(t *testing.T) {
	t.Parallel()

	c, _ := newCache(t)

	for _, key := range []string{"a", "b"} {
		if err := c.Set(t.Context(), key, tour{ID: key}, time.Minute); err != nil {
			t.Fatalf("Set %s: %v", key, err)
		}
	}

	if err := c.Delete(t.Context(), "a", "b"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	var got tour
	if err := c.Get(t.Context(), "a", &got); !errors.Is(err, cache.ErrNotFound) {
		t.Error("the key survived the delete")
	}
}

func TestDeletingAMissingKeyIsNotAnError(t *testing.T) {
	t.Parallel()

	c, _ := newCache(t)

	if err := c.Delete(t.Context(), "never-existed"); err != nil {
		t.Errorf("deleting a missing key failed: %v", err)
	}

	if err := c.Delete(t.Context()); err != nil {
		t.Errorf("deleting no keys failed: %v", err)
	}
}

// Invalidating a whole namespace is the operation the catalog needs when a tour is
// republished.
func TestDeleteByPrefixRemovesOnlyItsNamespace(t *testing.T) {
	t.Parallel()

	c, _ := newCache(t)

	keys := []string{
		"tours:tour:t1:v1",
		"tours:tour:t1:v2",
		"tours:tour:t2:v1",
		"tours:destination:d1:v1",
	}

	for _, key := range keys {
		if err := c.Set(t.Context(), key, tour{ID: key}, time.Minute); err != nil {
			t.Fatalf("Set %s: %v", key, err)
		}
	}

	if err := c.DeleteByPrefix(t.Context(), "tours:tour:"); err != nil {
		t.Fatalf("DeleteByPrefix: %v", err)
	}

	var got tour

	for _, key := range []string{"tours:tour:t1:v1", "tours:tour:t1:v2", "tours:tour:t2:v1"} {
		if err := c.Get(t.Context(), key, &got); !errors.Is(err, cache.ErrNotFound) {
			t.Errorf("%s survived the prefix delete", key)
		}
	}

	// A key outside the prefix must be untouched.
	if err := c.Get(t.Context(), "tours:destination:d1:v1", &got); err != nil {
		t.Errorf("a key outside the prefix was deleted: %v", err)
	}
}

// Deleting under an empty prefix would wipe the whole database, which is never what a
// caller means.
func TestDeleteByPrefixRejectsAnEmptyPrefix(t *testing.T) {
	t.Parallel()

	c, _ := newCache(t)

	if err := c.DeleteByPrefix(t.Context(), ""); err == nil {
		t.Error("an empty prefix was accepted: it would have wiped the keyspace")
	}
}

// A value left by an older version of a struct must not fail the caller: it is dropped and
// reported as a miss so the value is recomputed.
func TestAnUndecodableValueIsTreatedAsAMissAndEvicted(t *testing.T) {
	t.Parallel()

	c, server := newCache(t)

	if err := server.Set("poisoned", "this is not the JSON of a tour"); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	var got tour

	if err := c.Get(t.Context(), "poisoned", &got); !errors.Is(err, cache.ErrNotFound) {
		t.Errorf("the undecodable value did not come back as a miss: %v", err)
	}

	if server.Exists("poisoned") {
		t.Error("the poisoned entry was not evicted")
	}
}

func TestPing(t *testing.T) {
	t.Parallel()

	c, server := newCache(t)

	if err := c.Ping(t.Context()); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	server.Close()

	if err := c.Ping(t.Context()); err == nil {
		t.Error("Ping succeeded against a closed server")
	}
}

// Losing Redis must not pull every service out of rotation: a cold cache is slower but
// correct.
func TestTheHealthCheckIsNotCritical(t *testing.T) {
	t.Parallel()

	c, _ := newCache(t)

	check := c.HealthCheck()

	if check.Name != cache.CheckName {
		t.Errorf("name = %q", check.Name)
	}

	if check.Critical {
		t.Error("the cache check is critical: losing Redis would unready every service")
	}
}

func TestTheNamespaceDefaultsToTheServiceName(t *testing.T) {
	t.Parallel()

	c, _ := newCache(t)

	if got := c.Namespace().Service(); got != "tours" {
		t.Errorf("namespace = %q, want tours", got)
	}
}
