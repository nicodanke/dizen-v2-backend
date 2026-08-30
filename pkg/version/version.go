// Package version exposes the identity of the running build. Values are injected at
// compile time with -ldflags; in development the defaults below are used.
package version

import "runtime/debug"

var (
	// Version is the service version (git tag, or "dev").
	Version = "dev"
	// Commit is the short SHA of the commit that was built.
	Commit = "unknown"
	// BuildDate is the build timestamp in RFC 3339 format.
	BuildDate = "unknown"
)

// Info describes the running build. It is emitted in the startup log and by /livez.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
	GoVersion string `json:"go_version"`
}

// Get returns the build information, filling in the Go version from the binary itself.
func Get() Info {
	info := Info{
		Version:   Version,
		Commit:    Commit,
		BuildDate: BuildDate,
		GoVersion: "unknown",
	}

	if bi, ok := debug.ReadBuildInfo(); ok {
		info.GoVersion = bi.GoVersion
	}

	return info
}
