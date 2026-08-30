// Package errors maps domain errors to gRPC codes with a stable ErrorInfo.reason (RF-13).
//
// The rule it exists to enforce: **an unexpected error never leaks internal detail to the
// client, but is always logged in full with the trace_id**. A connection string, a SQL
// fragment or a stack trace in a response is an information leak; the same text in the log
// is what makes the incident diagnosable.
//
// Everything a handler returns goes through ToStatus. An error this package built is
// reported as declared; anything else becomes INTERNAL with no detail.
package errors

import (
	stderrors "errors"
	"fmt"
	"maps"

	"google.golang.org/grpc/codes"
)

// Error is a domain error carrying everything the transport needs to report it.
type Error struct {
	// Code is the gRPC code the client receives.
	Code codes.Code

	// Reason is the stable identifier the client branches on.
	Reason Reason

	// Message is safe to return: it describes what the caller did, never how the service
	// is built.
	Message string

	// Metadata is extra machine-readable context, such as which field was rejected. It is
	// returned to the client, so it must carry nothing internal.
	Metadata map[string]string

	// cause is the underlying failure. It is NEVER returned to the client; it exists to
	// be logged and to support errors.Is and errors.As.
	cause error
}

// New builds a domain error.
func New(code codes.Code, reason Reason, message string) *Error {
	return &Error{Code: code, Reason: reason, Message: message}
}

// Error implements the error interface. It renders the cause too, because this string only
// ever reaches the log -- what the client sees is built by ToStatus from Message.
func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Reason, e.Message, e.cause)
	}

	return fmt.Sprintf("%s: %s", e.Reason, e.Message)
}

// Unwrap exposes the cause to errors.Is and errors.As.
func (e *Error) Unwrap() error {
	return e.cause
}

// WithCause attaches the underlying failure. The cause is logged and never returned.
func (e *Error) WithCause(cause error) *Error {
	clone := *e
	clone.cause = cause

	return &clone
}

// WithMetadata attaches machine-readable context returned to the client.
func (e *Error) WithMetadata(key, value string) *Error {
	clone := *e
	clone.Metadata = make(map[string]string, len(e.Metadata)+1)

	maps.Copy(clone.Metadata, e.Metadata)

	clone.Metadata[key] = value

	return &clone
}

// WithMessage replaces the client-facing message.
func (e *Error) WithMessage(message string) *Error {
	clone := *e
	clone.Message = message

	return &clone
}

// Is makes two domain errors comparable by reason, so a caller can write
// errors.Is(err, ErrNotFound) regardless of the message or the cause.
func (e *Error) Is(target error) bool {
	var other *Error
	if !stderrors.As(target, &other) {
		return false
	}

	return e.Reason == other.Reason
}

// As extracts a domain error from a chain, reporting whether there was one.
func As(err error) (*Error, bool) {
	if domainErr, ok := stderrors.AsType[*Error](err); ok {
		return domainErr, true
	}

	return nil, false
}

// Constructors for the shapes every service needs. They exist so a handler declares its
// intent -- "this is a not found" -- instead of picking a gRPC code, which is how codes
// drift apart between services.

// NotFound builds a NOT_FOUND.
func NotFound(reason Reason, message string) *Error {
	return New(codes.NotFound, reason, message)
}

// InvalidArgument builds an INVALID_ARGUMENT.
func InvalidArgument(reason Reason, message string) *Error {
	return New(codes.InvalidArgument, reason, message)
}

// AlreadyExists builds an ALREADY_EXISTS.
func AlreadyExists(reason Reason, message string) *Error {
	return New(codes.AlreadyExists, reason, message)
}

// PermissionDenied builds a PERMISSION_DENIED.
func PermissionDenied(reason Reason, message string) *Error {
	return New(codes.PermissionDenied, reason, message)
}

// Unauthenticated builds an UNAUTHENTICATED.
func Unauthenticated(reason Reason, message string) *Error {
	return New(codes.Unauthenticated, reason, message)
}

// FailedPrecondition builds a FAILED_PRECONDITION: the request is well formed but the
// system state does not allow it.
func FailedPrecondition(reason Reason, message string) *Error {
	return New(codes.FailedPrecondition, reason, message)
}

// ResourceExhausted builds a RESOURCE_EXHAUSTED, used by the rate limiter.
func ResourceExhausted(reason Reason, message string) *Error {
	return New(codes.ResourceExhausted, reason, message)
}

// Unavailable builds an UNAVAILABLE: a dependency is down and the caller may retry.
func Unavailable(reason Reason, message string) *Error {
	return New(codes.Unavailable, reason, message)
}

// Internal builds an INTERNAL from an unexpected failure.
//
// The message is fixed and carries nothing: the cause travels only to the log. A handler
// should rarely call this, because returning any non-domain error produces the same result.
func Internal(cause error) *Error {
	return (&Error{
		Code:    codes.Internal,
		Reason:  ReasonInternal,
		Message: internalMessage,
	}).WithCause(cause)
}

// internalMessage is what a client is told about an unexpected failure. It is deliberately
// devoid of information.
const internalMessage = "internal error"
