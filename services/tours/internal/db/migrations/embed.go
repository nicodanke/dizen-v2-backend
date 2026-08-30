// Package migrations embeds the SQL migrations into the binary.
//
// They are embedded rather than shipped as files so the production image carries no
// directory that could drift from the binary, and so migrations can be applied at
// container startup (RF-7) with no volume and no extra step.
package migrations

import "embed"

// FS holds every migration file.
//
//go:embed *.sql
var FS embed.FS

// Path is the directory inside FS, as golang-migrate expects it.
const Path = "."
