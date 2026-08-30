package errors

// Domain is the value carried in ErrorInfo.domain. It identifies who owns the reason
// vocabulary, which is what lets a client tell a Dizen error apart from one raised by an
// infrastructure component in front of it.
const Domain = "dizen.io"

// Reason is a stable machine-readable error identifier (03 section 1).
//
// The contract with the clients is explicit: **the client decides on the reason, never on
// the message text**. That makes the message free to change, be translated or gain detail
// without breaking an app already in the stores, and it makes a reason a permanent part of
// the API -- renaming one is a breaking change exactly like renaming a field.
type Reason string

// Reasons shared by every service.
const (
	// ReasonInternal is the only reason an unexpected failure ever reports. It carries no
	// detail by design.
	ReasonInternal Reason = "INTERNAL_ERROR"

	// ReasonInvalidArgument is a request that failed validation.
	ReasonInvalidArgument Reason = "INVALID_ARGUMENT"

	// ReasonNotFound is a resource that does not exist, or that the caller may not know
	// exists.
	ReasonNotFound Reason = "NOT_FOUND"

	// ReasonAlreadyExists is a uniqueness conflict.
	ReasonAlreadyExists Reason = "ALREADY_EXISTS"

	// ReasonPermissionDenied is an authenticated caller without the required permission.
	ReasonPermissionDenied Reason = "PERMISSION_DENIED"

	// ReasonUnauthenticated is a missing, malformed or expired credential.
	ReasonUnauthenticated Reason = "UNAUTHENTICATED"

	// ReasonRateLimited is a caller over its quota.
	ReasonRateLimited Reason = "RATE_LIMITED"

	// ReasonUnavailable is a dependency that is down; the caller may retry.
	ReasonUnavailable Reason = "UNAVAILABLE"

	// ReasonClientTooOld is a client below MIN_CLIENT_API_VERSION (RF-2c). The app
	// translates it to "update the app".
	ReasonClientTooOld Reason = "CLIENT_TOO_OLD"
)

// Reasons owned by identity (PRD-01).
const (
	// ReasonSessionRevoked means the session_version in the token no longer matches the
	// user's. The app has to sign in again.
	ReasonSessionRevoked Reason = "SESSION_REVOKED"

	// ReasonTokenExpired is an access token past its expiry; the app refreshes it.
	ReasonTokenExpired Reason = "TOKEN_EXPIRED"

	// ReasonUserBlocked is an account blocked by an administrator.
	ReasonUserBlocked Reason = "USER_BLOCKED"

	// ReasonMustChangePassword is an account created by an administrator with a temporary
	// password.
	ReasonMustChangePassword Reason = "MUST_CHANGE_PASSWORD"
)

// Reasons owned by tours and booking (PRD-02, PRD-04, PRD-12).
const (
	// ReasonTourNotEntitled means the user has no access to the tour.
	ReasonTourNotEntitled Reason = "TOUR_NOT_ENTITLED"

	// ReasonSlotFull means the chosen slot has no capacity left.
	ReasonSlotFull Reason = "SLOT_FULL"
)

// String makes a Reason printable.
func (r Reason) String() string {
	return string(r)
}
