package outbox

import "errors"

// ErrEmptyRoutingKey is returned when an event is recorded without a routing key. It is
// caught here rather than at publication time because an event with no key is unroutable:
// it would sit in the table failing forever.
var ErrEmptyRoutingKey = errors.New("outbox: the routing key is empty")
