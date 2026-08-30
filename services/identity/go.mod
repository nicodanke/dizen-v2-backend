module github.com/nicodanke/dizen-v2-backend/services/identity

go 1.27.0

// There is no `require` or `replace` for .../pkg: in workspace mode it is resolved by the
// `use` directive in go.work (01 section 3, "no manual replace scattered per service").
// Declaring it as a require of a module that is never published breaks graph resolution.
// Consequence: every build -- Dockerfiles included -- must copy the root go.work.

require (
	github.com/jackc/pgx/v5 v5.10.0
	github.com/rs/zerolog v1.35.1
	github.com/stretchr/testify v1.12.1
	google.golang.org/grpc v1.83.2
	google.golang.org/protobuf v1.36.12
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/stretchr/objx v0.5.3 // indirect
	go.opentelemetry.io/otel v1.46.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.46.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260825221802-da73d73af1c5 // indirect
)
