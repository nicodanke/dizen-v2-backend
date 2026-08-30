package cache_test

import (
	"testing"

	"github.com/nicodanke/dizen-v2-backend/pkg/cache"
)

// The convention of RF-10 is `<service>:<entity>:<id>:v<version>`. It is tested because
// every invalidation depends on the exact shape: a key built differently is a key that
// DeleteByPrefix will never reach.
func TestVersionedFollowsTheConvention(t *testing.T) {
	t.Parallel()

	ns := cache.NewNamespace("tours")

	if got := ns.Versioned("tour", "0193f0a0", 4); got != "tours:tour:0193f0a0:v4" {
		t.Errorf("Versioned = %q, want tours:tour:0193f0a0:v4", got)
	}
}

func TestKeyJoinsTheSegments(t *testing.T) {
	t.Parallel()

	ns := cache.NewNamespace("identity")

	cases := map[string][]string{
		"identity":                    {},
		"identity:session":            {"session"},
		"identity:session:s1:revoked": {"session", "s1", "revoked"},
	}

	for want, segments := range cases {
		if got := ns.Key(segments...); got != want {
			t.Errorf("Key(%v) = %q, want %q", segments, got, want)
		}
	}
}

// The prefix has to end in the separator, otherwise `tours:tour:t1` would also match
// `tours:tour:t12` and invalidate an unrelated entry.
func TestPrefixEndsInTheSeparator(t *testing.T) {
	t.Parallel()

	ns := cache.NewNamespace("tours")

	got := ns.Prefix("tour", "t1")

	if got != "tours:tour:t1:" {
		t.Errorf("Prefix = %q, want tours:tour:t1:", got)
	}
}

// Two services must never be able to collide, which is the whole point of the namespace.
func TestTwoServicesProduceDisjointKeys(t *testing.T) {
	t.Parallel()

	tours := cache.NewNamespace("tours").Versioned("item", "1", 1)
	booking := cache.NewNamespace("booking").Versioned("item", "1", 1)

	if tours == booking {
		t.Errorf("two services produced the same key: %q", tours)
	}
}
