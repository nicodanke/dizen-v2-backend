// Package sentry reports errors to Sentry (PRD-24 RF-2).
//
// It is wired as a second writer of the logger rather than as a zerolog hook, which is what
// the official integration provides and is the better shape anyway: a hook is handed the
// level and the message and cannot read the fields, while a writer receives the rendered
// JSON line and can. That is how trace_id, span_id and service reach Sentry as tags, and it
// is what ties an error there to its trace in Jaeger (RF-7).
//
// Nothing here decides what is worth reporting. Every log at error level or above becomes an
// event, which means the decision stays at the call site, where the context is.
package sentry

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	sentrygo "github.com/getsentry/sentry-go"
	sentryzerolog "github.com/getsentry/sentry-go/zerolog"
	"github.com/rs/zerolog"
)

// flushTimeout bounds how long a shutdown waits for pending events. Losing the last error
// of a process is bad; hanging its shutdown is worse, and a supervisor that gives up waiting
// kills it before the rest of the graceful shutdown runs.
const flushTimeout = 3 * time.Second

// Options configures the reporter.
type Options struct {
	// DSN of the Sentry project. One project per service, so this differs per service
	// even within an environment. Empty disables reporting entirely.
	DSN string

	// Environment separates the same error happening in staging from production.
	Environment string

	// Release is what ties an error to the version that introduced it (RF-8). It carries
	// the commit, because a version alone does not identify a build of staging.
	Release string

	// ServiceName is attached as a tag so one project's events can still be grouped by
	// which binary produced them.
	ServiceName string
}

// Sink is the reporter. The zero value is a working no-op, so a caller never has to check
// whether reporting is enabled before using it.
type Sink struct {
	writer io.Writer
	close  func() error
}

// Setup initializes the reporter.
//
// An empty DSN is not an error: it disables reporting and returns a Sink that writes
// nowhere. That is what lets a developer machine and a fresh environment run without a
// Sentry project, and it is the same contract the tracing package uses for its endpoint.
func Setup(opts Options) (*Sink, error) {
	if opts.DSN == "" {
		return &Sink{}, nil
	}

	writer, err := sentryzerolog.New(sentryzerolog.Config{
		ClientOptions: sentrygo.ClientOptions{
			Dsn:         opts.DSN,
			Environment: opts.Environment,
			Release:     opts.Release,

			// RF-10. Sentry can populate user fields and attach bodies, headers and
			// cookies on its own; none of that is ours to send. Everything this reports
			// is what a log line already said.
			//
			// DataCollection rather than the deprecated SendDefaultPII, which this
			// version ignores whenever DataCollection is set -- so leaving the old one
			// in would have read as a privacy setting that does nothing.
			DataCollection: &sentrygo.DataCollection{
				UserInfo:   sentrygo.Set(false),
				HTTPBodies: []sentrygo.BodyType{},
			},

			BeforeSend: scrubEvent,
		},
		Options: sentryzerolog.Options{
			// Error and above. A warning is something the service handled.
			Levels:       []zerolog.Level{zerolog.ErrorLevel, zerolog.FatalLevel, zerolog.PanicLevel},
			FlushTimeout: flushTimeout,

			// Breadcrumbs would mean buffering every log line in memory to attach the
			// last few to an event. The lines are already in the container log, indexed
			// by the same trace_id this event carries, so the trail is one query away.
			WithBreadcrumbs: false,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("initializing sentry: %w", err)
	}

	if opts.ServiceName != "" {
		sentrygo.ConfigureScope(func(scope *sentrygo.Scope) {
			scope.SetTag("service", opts.ServiceName)
		})
	}

	return &Sink{writer: writer, close: writer.Close}, nil
}

// Writer returns the writer to add to the logger, or nil when reporting is disabled.
func (s *Sink) Writer() io.Writer {
	if s == nil {
		return nil
	}

	return s.writer
}

// Close flushes whatever is still pending. Safe on a disabled sink and on a nil one.
func (s *Sink) Close() error {
	if s == nil || s.close == nil {
		return nil
	}

	return s.close()
}

// sensitiveKey matches the name of a field whose value must never leave the process.
//
// Matched on the name rather than the value because a token is not recognizable by shape --
// a bearer token and a request id are both opaque strings -- and because the name is what
// the developer chose, so it is the honest signal about what the value is.
var sensitiveKey = regexp.MustCompile(`(?i)(token|password|passwd|secret|authorization|api[_-]?key|credential|dsn|cookie|session|email|latitude|longitude|\blat\b|\blng\b)`)

// Patterns that must not travel even inside a message, where there is no field name to go
// on. Deliberately few: a filter that rewrites too much makes errors unreadable, and the
// real defense is not logging these in the first place.
var sensitiveValue = []*regexp.Regexp{
	// A JWT, which is what an access or refresh token looks like here.
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{4,}`),
	// An Authorization header that made it into a message.
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._-]{8,}`),
	// An email address.
	regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`),
	// A connection string with credentials in it.
	regexp.MustCompile(`(?i)(postgres|postgresql|redis|amqp)://[^:\s]+:[^@\s]+@`),
}

const redacted = "[redacted]"

// scrubEvent is the BeforeSend filter of RF-10.
//
// It redacts rather than dropping the event. An error that cannot be reported because it
// mentioned an email is an error nobody finds out about, which is the worse failure.
func scrubEvent(event *sentrygo.Event, _ *sentrygo.EventHint) *sentrygo.Event {
	if event == nil {
		return nil
	}

	event.Message = scrub(event.Message)

	for key, value := range event.Tags {
		if sensitiveKey.MatchString(key) {
			event.Tags[key] = redacted

			continue
		}

		event.Tags[key] = scrub(value)
	}

	for i := range event.Exception {
		event.Exception[i].Value = scrub(event.Exception[i].Value)
	}

	// Tags is where everything ends up: the zerolog integration maps `message`, `error`
	// and `user` to their own places and every other field to a tag, so a scrub of the
	// tags is a scrub of the structured context.

	// RF-10 again, from the other side: even with SendDefaultPII off, an identifier can
	// reach the user through a log field. The id is allowed and is what makes an error
	// actionable; the rest is not.
	event.User = sentrygo.User{ID: event.User.ID}

	return event
}

// scrub replaces every sensitive pattern in a string.
func scrub(text string) string {
	if text == "" {
		return text
	}

	for _, pattern := range sensitiveValue {
		text = pattern.ReplaceAllString(text, redacted)
	}

	return text
}

// Release builds the release identifier: the version and the commit that produced it.
//
// Sentry groups by this string, so it has to change with every build or a regression is
// attributed to the release before it. The version alone does not: every build of staging
// reports the same one.
func Release(serviceName, version, commit string) string {
	parts := []string{serviceName, version}

	if commit != "" && commit != "unknown" {
		parts = append(parts, commit)
	}

	return strings.Join(parts, "@")
}
