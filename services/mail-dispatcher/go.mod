module github.com/nicodanke/dizen-v2-backend/services/mail-dispatcher

go 1.27.0

// There is no `require` or `replace` for .../pkg: in workspace mode it is resolved by the
// `use` directive in go.work (01 section 3, "no manual replace scattered per service").
// Declaring it as a require of a module that is never published breaks graph resolution.
// Consequence: every build -- Dockerfiles included -- must copy the root go.work.
