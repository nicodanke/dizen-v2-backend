package service

import "errors"

// ErrMissingUserID is returned when an event is recorded without the user it belongs to.
// It is caught here because an event with no subject is one nobody can act on downstream.
var ErrMissingUserID = errors.New("the user id is empty")
