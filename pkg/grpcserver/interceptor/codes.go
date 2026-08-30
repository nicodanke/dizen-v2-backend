package interceptor

import "google.golang.org/grpc/codes"

// Aliases used by the switch in levelFor. They exist so the switch reads as a list of
// codes rather than a chain of qualified constants.
const (
	codesInternal    = codes.Internal
	codesUnknown     = codes.Unknown
	codesDataLoss    = codes.DataLoss
	codesUnavailable = codes.Unavailable
)
