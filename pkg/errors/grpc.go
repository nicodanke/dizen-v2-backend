package errors

import (
	"context"
	stderrors "errors"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nicodanke/dizen-v2-backend/pkg/logger"
)

// ToStatus turns any error into the gRPC status the client receives.
//
// This is where RF-13 is enforced. Three cases:
//
//   - A domain error is reported as declared: its code, its reason, its message.
//   - An error that is already a gRPC status -- produced by an interceptor below, such as
//     validate -- is passed through unchanged.
//   - Anything else is an unexpected failure: the client gets INTERNAL with no detail, and
//     the full error is logged with the trace_id.
//
// The context is taken so the logging can happen here rather than being left to a caller
// who might forget, which is the failure mode this design is built to prevent.
func ToStatus(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}

	if domainErr, ok := As(err); ok {
		logDomainError(ctx, domainErr)

		return domainErr.status().Err()
	}

	// A context cancellation is the caller hanging up, not a server failure. Reporting it
	// as INTERNAL would make every abandoned request look like an incident.
	if stderrors.Is(err, context.Canceled) {
		return status.Error(codes.Canceled, "the call was canceled")
	}

	if stderrors.Is(err, context.DeadlineExceeded) {
		return status.Error(codes.DeadlineExceeded, "the call exceeded its deadline")
	}

	// Already a gRPC status: an interceptor below built it deliberately.
	if _, ok := status.FromError(err); ok && isStatusError(err) {
		return err
	}

	return unexpected(ctx, err)
}

// isStatusError reports whether the error really carries a gRPC status, as opposed to
// status.FromError's fallback of treating any error as UNKNOWN.
func isStatusError(err error) bool {
	var withStatus interface{ GRPCStatus() *status.Status }

	return stderrors.As(err, &withStatus)
}

// unexpected reports an error nobody declared: nothing to the client, everything to the log.
func unexpected(ctx context.Context, err error) error {
	logger.Ctx(ctx).Error().
		Err(err).
		Str("reason", ReasonInternal.String()).
		Msg("unexpected error in a handler")

	st := status.New(codes.Internal, internalMessage)

	withDetails, detailErr := st.WithDetails(&errdetails.ErrorInfo{
		Reason: ReasonInternal.String(),
		Domain: Domain,
	})
	if detailErr != nil {
		return st.Err()
	}

	return withDetails.Err()
}

// logDomainError records a declared error at the level its code deserves.
//
// A client error is not an operational problem: logging every 404 at error level buries
// the failures that matter. Server-side codes are logged at error, everything else at
// debug, with the reason always present so the rate of each can be counted.
func logDomainError(ctx context.Context, domainErr *Error) {
	event := logger.Ctx(ctx).WithLevel(levelFor(domainErr.Code)).
		Str("reason", domainErr.Reason.String()).
		Str("code", domainErr.Code.String())

	// The cause is the internal half: it is logged and never returned.
	if cause := domainErr.Unwrap(); cause != nil {
		event = event.Err(cause)
	}

	event.Msg(domainErr.Message)
}

// status builds the gRPC status carrying ErrorInfo, which is the shape 03 section 1
// mandates for every error.
func (e *Error) status() *status.Status {
	st := status.New(e.Code, e.Message)

	info := &errdetails.ErrorInfo{
		Reason:   e.Reason.String(),
		Domain:   Domain,
		Metadata: e.Metadata,
	}

	withDetails, err := st.WithDetails(info)
	if err != nil {
		// Attaching details can only fail on a marshaling problem. The bare status is
		// still a correct answer, so the call is still reported.
		return st
	}

	return withDetails
}

// GRPCStatus lets status.FromError read a domain error directly, which is what makes the
// error usable even by a caller that forgot to run it through ToStatus.
func (e *Error) GRPCStatus() *status.Status {
	return e.status()
}

// ReasonOf extracts the stable reason from any error, returning an empty string when there
// is none. It is what a gRPC client uses to branch on the outcome.
func ReasonOf(err error) Reason {
	if err == nil {
		return ""
	}

	if domainErr, ok := As(err); ok {
		return domainErr.Reason
	}

	st, ok := status.FromError(err)
	if !ok {
		return ""
	}

	for _, detail := range st.Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok {
			return Reason(info.GetReason())
		}
	}

	return ""
}

// levelFor picks the log level from the gRPC code.
func levelFor(code codes.Code) zerologLevel {
	switch code {
	case codes.Internal, codes.Unknown, codes.DataLoss, codes.Unavailable:
		return levelError
	case codes.ResourceExhausted:
		return levelWarn
	default:
		return levelDebug
	}
}
