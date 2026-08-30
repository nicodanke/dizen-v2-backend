package errors

import "github.com/rs/zerolog"

// zerologLevel aliases the log level so grpc.go reads as a policy rather than as a set of
// qualified constants.
type zerologLevel = zerolog.Level

const (
	levelError = zerolog.ErrorLevel
	levelWarn  = zerolog.WarnLevel
	levelDebug = zerolog.DebugLevel
)
