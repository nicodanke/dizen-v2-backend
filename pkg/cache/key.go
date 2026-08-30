package cache

import (
	"strconv"
	"strings"
)

// Separator between the segments of a key.
const Separator = ":"

// Namespace builds the keys of one service, so two services sharing a Redis instance can
// never collide and one service's keys can be invalidated as a group.
//
// The convention of RF-10 is `<service>:<entity>:<id>:v<version>`. The version segment is
// what makes invalidation cheap: publishing a new version of a tour makes every key of the
// previous one unreachable without deleting anything.
type Namespace struct {
	service string
}

// NewNamespace builds the namespace of a service.
func NewNamespace(service string) Namespace {
	return Namespace{service: service}
}

// Service returns the prefix every key of this namespace starts with.
func (n Namespace) Service() string {
	return n.service
}

// Key builds `<service>:<segments...>`.
func (n Namespace) Key(segments ...string) string {
	parts := make([]string, 0, len(segments)+1)
	parts = append(parts, n.service)
	parts = append(parts, segments...)

	return strings.Join(parts, Separator)
}

// Versioned builds `<service>:<entity>:<id>:v<version>`, the full convention of RF-10.
func (n Namespace) Versioned(entity, id string, version int) string {
	return n.Key(entity, id, "v"+strconv.Itoa(version))
}

// Prefix builds the prefix that covers every key under the given segments, for use with
// DeleteByPrefix.
func (n Namespace) Prefix(segments ...string) string {
	return n.Key(segments...) + Separator
}
