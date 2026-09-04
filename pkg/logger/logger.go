// Package logger builds the structured logger every service shares (RF-4): JSON in
// production, colored console in development, level driven by configuration, and
// service, version, trace_id and span_id attached automatically.
//
// There is no package-level logger. The logger is built in main.go and injected, the same
// as every other dependency; the only way to reach it from a handler is through the
// context (see Ctx).
package logger

import (
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/nicodanke/dizen-v2-backend/pkg/version"
)

// Field names. They are constants because dashboards, Loki queries and alerts are written
// against them: renaming one silently breaks the queries.
const (
	FieldService = "service"
	FieldVersion = "version"
	FieldTraceID = "trace_id"
	FieldSpanID  = "span_id"
	FieldUserID  = "user_id"
	FieldMethod  = "method"
)

// Options configures the logger. It mirrors the parts of config.Base the logger needs,
// rather than importing config, so that pkg/logger stays usable on its own.
type Options struct {
	// ServiceName is attached to every line.
	ServiceName string

	// Level is the zerolog level name. An unknown value falls back to info.
	Level string

	// Pretty enables the colored console writer. It is meant for development: it costs
	// an allocation per line and is not machine readable.
	Pretty bool

	// Output is where lines are written. Defaults to os.Stdout.
	Output io.Writer

	// Extra are additional destinations for every line, on top of Output.
	//
	// This is how the Sentry reporter is attached (PRD-24 RF-2): it is an io.Writer that
	// parses the JSON line, so it sees the fields -- trace_id among them -- which a
	// zerolog hook cannot, because a hook is handed only the level and the message.
	//
	// Nil entries are skipped, so a caller can pass a disabled reporter's writer without
	// checking it first.
	Extra []io.Writer
}

// New builds the root logger.
//
// It never fails: a service that cannot log its own startup error is worse than one that
// logs at the wrong level, so an unparseable level degrades to info instead of aborting.
func New(opts Options) zerolog.Logger {
	out := opts.Output
	if out == nil {
		out = os.Stdout
	}

	if opts.Pretty {
		out = zerolog.ConsoleWriter{
			Out:        out,
			TimeFormat: time.RFC3339,
		}
	}

	// Each writer receives the same rendered line, so the console formatting above applies
	// to stdout and the extras still get the JSON they parse.
	if writers := append([]io.Writer{out}, nonNil(opts.Extra)...); len(writers) > 1 {
		out = zerolog.MultiLevelWriter(writers...)
	}

	ctx := zerolog.New(out).
		Level(ParseLevel(opts.Level)).
		With().
		Timestamp().
		Str(FieldVersion, version.Get().Version)

	if opts.ServiceName != "" {
		ctx = ctx.Str(FieldService, opts.ServiceName)
	}

	return ctx.Logger()
}

// nonNil drops the nil writers, so a caller can pass an optional destination straight
// through rather than building the slice conditionally.
func nonNil(writers []io.Writer) []io.Writer {
	kept := make([]io.Writer, 0, len(writers))

	for _, w := range writers {
		if w != nil {
			kept = append(kept, w)
		}
	}

	return kept
}

// ParseLevel maps a level name to a zerolog level, falling back to info.
func ParseLevel(name string) zerolog.Level {
	level, err := zerolog.ParseLevel(strings.ToLower(strings.TrimSpace(name)))
	if err != nil || level == zerolog.NoLevel {
		return zerolog.InfoLevel
	}

	return level
}

// Nop returns a logger that discards everything. Useful in tests that exercise code paths
// which log, without polluting the test output.
func Nop() zerolog.Logger {
	return zerolog.New(io.Discard).Level(zerolog.Disabled)
}
