package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/nicodanke/dizen-v2-backend/pkg/health"
)

// CheckName is how the cache appears in the /readyz report.
const CheckName = "redis"

// ErrEmptyPrefix is returned by DeleteByPrefix when given an empty prefix. Deleting under
// one would wipe the whole keyspace, which is never what a caller means.
var ErrEmptyPrefix = errors.New("cache: DeleteByPrefix requires a non-empty prefix")

// scanBatchSize is how many keys DeleteByPrefix asks for per round trip. SCAN is used
// rather than KEYS because KEYS blocks the whole server while it walks the keyspace, which
// on a shared Redis stalls every service at once.
const scanBatchSize = 256

// Config describes the Redis connection.
type Config struct {
	// URL is the connection string, such as redis://localhost:6379/0. Required.
	URL string `env:"REDIS_URL" validate:"required"`

	// Namespace prefixes every key. Defaults to the service name.
	Namespace string `env:"REDIS_NAMESPACE"`

	// DefaultTTL is applied when a caller passes zero. A cache with no expiry by default
	// is a memory leak with extra steps.
	DefaultTTL time.Duration `env:"REDIS_DEFAULT_TTL" envDefault:"5m"`

	// DialTimeout bounds establishing a connection.
	DialTimeout time.Duration `env:"REDIS_DIAL_TIMEOUT" envDefault:"5s"`

	// ReadTimeout bounds a single command.
	ReadTimeout time.Duration `env:"REDIS_READ_TIMEOUT" envDefault:"3s"`

	// PoolSize is the maximum number of connections.
	PoolSize int `env:"REDIS_POOL_SIZE" envDefault:"10" validate:"min=1"`
}

// RedisCache is the Redis-backed implementation of CacheService.
type RedisCache struct {
	client     redis.UniversalClient
	namespace  Namespace
	defaultTTL time.Duration
}

// compile-time check that the implementation satisfies the contract.
var _ CacheService = (*RedisCache)(nil)

// NewRedis opens the connection and verifies it answers.
//
// Unlike the database, the cache is not retried at startup: a service can serve correctly
// with a cold cache, so blocking the boot on Redis would trade a slow start for no start.
func NewRedis(ctx context.Context, cfg Config, serviceName string) (*RedisCache, error) {
	opts, err := redis.ParseURL(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("REDIS_URL could not be parsed: %w", err)
	}

	opts.DialTimeout = cfg.DialTimeout
	opts.ReadTimeout = cfg.ReadTimeout
	opts.PoolSize = cfg.PoolSize

	client := redis.NewClient(opts)

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()

		return nil, fmt.Errorf("connecting to Redis: %w", err)
	}

	return NewRedisWithClient(client, cfg, serviceName), nil
}

// NewRedisWithClient builds the cache over an existing client. It is what the integration
// tests use against a container, and what a caller with its own client would use.
func NewRedisWithClient(client redis.UniversalClient, cfg Config, serviceName string) *RedisCache {
	namespace := cfg.Namespace
	if namespace == "" {
		namespace = serviceName
	}

	ttl := cfg.DefaultTTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}

	return &RedisCache{
		client:     client,
		namespace:  NewNamespace(namespace),
		defaultTTL: ttl,
	}
}

// Namespace returns the key builder for this service.
func (c *RedisCache) Namespace() Namespace {
	return c.namespace
}

// Get decodes the value at key into dest.
func (c *RedisCache) Get(ctx context.Context, key string, dest any) error {
	raw, err := c.client.Get(ctx, key).Bytes()

	if errors.Is(err, redis.Nil) {
		return ErrNotFound
	}

	if err != nil {
		return fmt.Errorf("reading the cache key %q: %w", key, err)
	}

	if err := json.Unmarshal(raw, dest); err != nil {
		// A value that cannot be decoded is a poisoned entry, usually left by an older
		// version of the struct. It is dropped and reported as a miss so the caller
		// recomputes instead of failing.
		_ = c.client.Del(ctx, key).Err()

		return ErrNotFound
	}

	return nil
}

// Set stores value as JSON with a time to live.
func (c *RedisCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("serializing the value for the cache key %q: %w", key, err)
	}

	if ttl == 0 {
		ttl = c.defaultTTL
	}

	if err := c.client.Set(ctx, key, raw, ttl).Err(); err != nil {
		return fmt.Errorf("writing the cache key %q: %w", key, err)
	}

	return nil
}

// Delete removes the given keys.
func (c *RedisCache) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}

	if err := c.client.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("deleting %d cache keys: %w", len(keys), err)
	}

	return nil
}

// DeleteByPrefix removes every key under a prefix, walking the keyspace with SCAN.
func (c *RedisCache) DeleteByPrefix(ctx context.Context, prefix string) error {
	if prefix == "" {
		return ErrEmptyPrefix
	}

	var cursor uint64

	for {
		keys, next, err := c.client.Scan(ctx, cursor, prefix+"*", scanBatchSize).Result()
		if err != nil {
			return fmt.Errorf("scanning the cache prefix %q: %w", prefix, err)
		}

		if len(keys) > 0 {
			if err := c.Delete(ctx, keys...); err != nil {
				return err
			}
		}

		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}

// Ping checks the connection.
func (c *RedisCache) Ping(ctx context.Context) error {
	if err := c.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("pinging Redis: %w", err)
	}

	return nil
}

// Close releases the connection.
func (c *RedisCache) Close() error {
	if err := c.client.Close(); err != nil {
		return fmt.Errorf("closing the Redis connection: %w", err)
	}

	return nil
}

// HealthCheck is the readiness probe for the cache.
//
// It is NOT critical: a service with a cold cache is slower but correct, so losing Redis
// must not pull every service out of rotation at once. The probe still reports it, so the
// degradation is visible.
func (c *RedisCache) HealthCheck() health.Check {
	return health.Check{
		Name:     CheckName,
		Critical: false,
		Probe:    c.Ping,
	}
}
