//go:build integration

// Integration tests for pkg/cache against a real Redis (RF-18).
//
// The unit tests run on miniredis, which is a reimplementation. These check the behavior
// this code actually relies on -- that SCAN walks the keyspace in cursor order, that TTLs
// expire the way the server implements them -- which is exactly what a reimplementation
// cannot promise.

package cache_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/nicodanke/dizen-v2-backend/pkg/cache"
	"github.com/nicodanke/dizen-v2-backend/pkg/testutils"
)

// One Redis for the whole package; tests isolate themselves by key prefix.
var redisURL string

func TestMain(m *testing.M) {
	if !testutils.DockerAvailable() {
		fmt.Fprintln(os.Stderr, "skipping the cache integration tests: Docker is not available")
		os.Exit(0)
	}

	os.Exit(m.Run())
}

// liveCache returns a cache backed by a real Redis.
func liveCache(t *testing.T) (*cache.RedisCache, *redis.Client) {
	t.Helper()
	testutils.SkipIfNoDocker(t)

	live := testutils.SetupRedis(t)
	redisURL = live.URL

	return cache.NewRedisWithClient(live.Client, cache.Config{DefaultTTL: time.Minute}, "tours"), live.Client
}

type tourDoc struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Version int    `json:"version"`
}

func TestRoundTripAgainstRealRedis(t *testing.T) {
	c, _ := liveCache(t)

	want := tourDoc{ID: "t1", Title: "Recoleta", Version: 3}
	key := cache.NewNamespace("tours").Versioned("tour", "t1", 3)

	if err := c.Set(t.Context(), key, want, time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}

	var got tourDoc
	if err := c.Get(t.Context(), key, &got); err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestNewRedisConnectsFromAURL(t *testing.T) {
	// Exercises the constructor the services actually call, which the miniredis tests skip
	// because they inject a client.
	c, _ := liveCache(t)
	_ = c

	live, err := cache.NewRedis(t.Context(), cache.Config{URL: redisURL}, "identity")
	if err != nil {
		t.Fatalf("NewRedis: %v", err)
	}

	defer func() { _ = live.Close() }()

	if err := live.Ping(t.Context()); err != nil {
		t.Errorf("Ping: %v", err)
	}

	if live.Namespace().Service() != "identity" {
		t.Errorf("namespace = %q", live.Namespace().Service())
	}
}

// A real expiry, not a simulated one.
func TestTTLExpiresForReal(t *testing.T) {
	c, _ := liveCache(t)

	if err := c.Set(t.Context(), "expiring", tourDoc{ID: "t1"}, 1*time.Second); err != nil {
		t.Fatalf("Set: %v", err)
	}

	var got tourDoc
	if err := c.Get(t.Context(), "expiring", &got); err != nil {
		t.Fatalf("the key was already gone: %v", err)
	}

	time.Sleep(1500 * time.Millisecond)

	if err := c.Get(t.Context(), "expiring", &got); !errors.Is(err, cache.ErrNotFound) {
		t.Errorf("the expired key did not come back as a miss: %v", err)
	}
}

// DeleteByPrefix uses SCAN, which is cursor-based and paginated. With more keys than one
// batch holds, a wrong cursor loop silently leaves keys behind -- which is the bug this
// test exists to catch, and which miniredis would not surface.
func TestDeleteByPrefixWalksTheWholeKeyspace(t *testing.T) {
	c, client := liveCache(t)

	const total = 1500

	ctx := t.Context()

	for i := range total {
		key := fmt.Sprintf("tours:tour:t%d:v1", i)

		if err := c.Set(ctx, key, tourDoc{ID: key}, time.Hour); err != nil {
			t.Fatalf("seeding %s: %v", key, err)
		}
	}

	// A key outside the prefix, to prove the delete is scoped.
	if err := c.Set(ctx, "tours:destination:d1:v1", tourDoc{ID: "d1"}, time.Hour); err != nil {
		t.Fatalf("seeding the destination: %v", err)
	}

	if err := c.DeleteByPrefix(ctx, "tours:tour:"); err != nil {
		t.Fatalf("DeleteByPrefix: %v", err)
	}

	remaining, err := client.Keys(ctx, "tours:tour:*").Result()
	if err != nil {
		t.Fatalf("counting what is left: %v", err)
	}

	if len(remaining) != 0 {
		t.Errorf("%d of %d keys survived the prefix delete: the SCAN loop is not paginating",
			len(remaining), total)
	}

	if client.Exists(ctx, "tours:destination:d1:v1").Val() != 1 {
		t.Error("a key outside the prefix was deleted")
	}
}

// Two services sharing one Redis must not be able to collide, which is what the namespace is
// for. Against a real server this also checks the prefix delete of one leaves the other
// alone.
func TestTwoServicesShareRedisWithoutColliding(t *testing.T) {
	_, client := liveCache(t)

	tours := cache.NewRedisWithClient(client, cache.Config{}, "tours")
	booking := cache.NewRedisWithClient(client, cache.Config{}, "booking")

	ctx := t.Context()

	toursKey := tours.Namespace().Versioned("item", "1", 1)
	bookingKey := booking.Namespace().Versioned("item", "1", 1)

	if err := tours.Set(ctx, toursKey, tourDoc{Title: "from tours"}, time.Hour); err != nil {
		t.Fatalf("Set tours: %v", err)
	}

	if err := booking.Set(ctx, bookingKey, tourDoc{Title: "from booking"}, time.Hour); err != nil {
		t.Fatalf("Set booking: %v", err)
	}

	if err := tours.DeleteByPrefix(ctx, tours.Namespace().Prefix("item")); err != nil {
		t.Fatalf("DeleteByPrefix: %v", err)
	}

	var got tourDoc
	if err := booking.Get(ctx, bookingKey, &got); err != nil {
		t.Errorf("invalidating tours removed a booking key: %v", err)
	}

	if got.Title != "from booking" {
		t.Errorf("title = %q", got.Title)
	}
}

func TestHealthCheckAgainstRealRedis(t *testing.T) {
	c, _ := liveCache(t)

	check := c.HealthCheck()

	if err := check.Probe(t.Context()); err != nil {
		t.Errorf("the probe failed against a live Redis: %v", err)
	}

	// Losing Redis must not unready the service: a cold cache is slower but correct.
	if check.Critical {
		t.Error("the cache check is critical")
	}
}

func TestNewRedisRejectsAnUnreachableServer(t *testing.T) {
	testutils.SkipIfNoDocker(t)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	_, err := cache.NewRedis(ctx, cache.Config{
		URL:         "redis://127.0.0.1:1/0",
		DialTimeout: time.Second,
	}, "identity")
	if err == nil {
		t.Error("NewRedis succeeded against a closed port")
	}
}
