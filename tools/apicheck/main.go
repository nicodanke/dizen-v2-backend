// Command apicheck validates the versioned Yaak collection (RF-17c, 03 section 8.2 rule 5).
//
// It is written in Go rather than as a shell script with a YAML parser bolted on because Go
// is the one toolchain this repository already requires: the check must run on every machine
// and in CI without anybody installing anything.
//
// What it checks:
//
//  1. The collection is complete: the workspace and the four environments are all present.
//     Without this the check passes on a collection somebody deleted half of, which is a
//     failure mode this very check once had.
//  2. Every file parses as YAML, so a collection broken by a bad merge is caught here rather
//     than when somebody opens Yaak.
//  3. No credential is committed, in any environment.
//  4. The remote environments carry no values at all, since their hostnames are not public.
//
// The rule it enforces is narrower than "no values anywhere", and deliberately so. What has
// to stay out of the repository is a secret; the localhost ports of the compose file are not
// one. They are constants of the development environment, identical on every machine and
// already written down in the README and the compose file, so committing them means the
// collection works right after `make up` with nothing to type.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// The two ways this check fails, as static errors so a caller can branch on them and so the
// err113 linter has something to match. Both are wrapped with the detail at the call site.
var (
	errNoCollection = errors.New("no yaak.*.yaml file")
	errProblems     = errors.New("problem(s) found")
)

// resource is the subset of a Yaak file this check reads.
type resource struct {
	Model     string     `yaml:"model"`
	Name      string     `yaml:"name"`
	ID        string     `yaml:"id"`
	Variables []variable `yaml:"variables"`
}

// variable is one entry of an environment.
type variable struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}

// credentialVariables must never carry a value, in any environment. They are filled in at
// run time by the sign-in request (03 section 8.2, rule 4).
var credentialVariables = []string{
	"access_token",
	"refresh_token",
	"id_token",
	"password",
	"api_key",
	"client_secret",
	"totp_secret",
}

// localEnvironmentID is the one environment allowed to carry values: its endpoints are the
// ports the compose file publishes.
const localEnvironmentID = "env_local"

// requiredEnvironments must all be present. A missing one is not a harmless absence: a
// collection without `local` does not work after `make up`, and one without `prod` is one
// somebody will recreate by hand, filled in.
var requiredEnvironments = []string{"env_base", "env_local", "env_staging", "env_prod"}

// requiredModels are the resource kinds the collection cannot be without.
var requiredModels = []string{"workspace"}

// localAllowedValue matches what the local environment may hold: a localhost endpoint or a
// plain semver. Anything else is refused, so a token pasted into `local` is still caught.
var localAllowedValue = regexp.MustCompile(
	`^(https?://(localhost|127\.0\.0\.1)(:\d+)?(/.*)?|(localhost|127\.0\.0\.1):\d+|\d+\.\d+\.\d+)$`,
)

// credentialShaped matches values that look like a secret regardless of the variable name: a
// JWT, or a long opaque string. It is the backstop for a variable nobody thought to list.
var credentialShaped = regexp.MustCompile(`^(eyJ[A-Za-z0-9_-]{10,}|[A-Za-z0-9_-]{40,})$`)

func main() {
	dir := "api-client"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}

	if err := run(dir); err != nil {
		fmt.Fprintf(os.Stderr, "\napi-client: %v\n\n", err)
		os.Exit(1)
	}
}

func run(dir string) error {
	files, err := filepath.Glob(filepath.Join(dir, "yaak.*.yaml"))
	if err != nil {
		return fmt.Errorf("listing %s: %w", dir, err)
	}

	if len(files) == 0 {
		return fmt.Errorf("%w in %s", errNoCollection, dir)
	}

	sort.Strings(files)

	var (
		failures     []string
		requests     int
		environments int
	)

	seenEnvironments := map[string]bool{}
	seenModels := map[string]bool{}

	for _, file := range files {
		raw, err := os.ReadFile(file) //nolint:gosec // paths come from the glob above
		if err != nil {
			return fmt.Errorf("reading %s: %w", file, err)
		}

		var doc resource
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			failures = append(failures, fmt.Sprintf("%s: does not parse as YAML: %v", filepath.Base(file), err))

			continue
		}

		seenModels[doc.Model] = true

		switch doc.Model {
		case "environment":
			environments++
			seenEnvironments[doc.ID] = true
			failures = append(failures, checkEnvironment(filepath.Base(file), doc)...)

		case "http_request", "grpc_request":
			requests++
		}
	}

	failures = append(failures, checkCompleteness(seenEnvironments, seenModels, requests)...)

	if len(failures) > 0 {
		for _, failure := range failures {
			fmt.Fprintf(os.Stderr, "  - %s\n", failure)
		}

		return fmt.Errorf("%d %w", len(failures), errProblems)
	}

	fmt.Printf("==> api-client: %d files, %d requests, %d environments, no credentials committed\n",
		len(files), requests, environments)

	return nil
}

// checkCompleteness reports what the collection is missing.
func checkCompleteness(environments, models map[string]bool, requests int) []string {
	var failures []string

	for _, id := range requiredEnvironments {
		if !environments[id] {
			failures = append(failures, fmt.Sprintf(
				"the %q environment is missing. The collection has to carry all four "+
					"(base, local, staging, prod)", id))
		}
	}

	for _, model := range requiredModels {
		if !models[model] {
			failures = append(failures, fmt.Sprintf("there is no %s file", model))
		}
	}

	if requests == 0 {
		failures = append(failures, "the collection has no requests")
	}

	return failures
}

// checkEnvironment applies the rules to one environment.
func checkEnvironment(filename string, doc resource) []string {
	var failures []string

	isLocal := doc.ID == localEnvironmentID

	for _, v := range doc.Variables {
		value := strings.TrimSpace(v.Value)
		if value == "" {
			continue
		}

		// A credential is never committed, not even locally.
		if slices.Contains(credentialVariables, strings.ToLower(v.Name)) {
			failures = append(failures, fmt.Sprintf(
				"%s: %q carries a value. Credentials are filled in at run time by the sign-in "+
					"request, never committed (03 section 8.2, rule 4)", filename, v.Name))

			continue
		}

		if credentialShaped.MatchString(value) {
			failures = append(failures, fmt.Sprintf(
				"%s: %q holds something that looks like a credential", filename, v.Name))

			continue
		}

		// Beyond local, nothing is committed: those hostnames are not public.
		if !isLocal {
			failures = append(failures, fmt.Sprintf(
				"%s: %q carries a value. Only the local environment is versioned filled in, "+
					"because its endpoints are the ports the compose file publishes "+
					"(03 section 8.2, rule 2)", filename, v.Name))

			continue
		}

		// Inside local, only endpoints and versions.
		if !localAllowedValue.MatchString(value) {
			failures = append(failures, fmt.Sprintf(
				"%s: %q = %q is not a localhost endpoint or a version. The local environment "+
					"is versioned filled in only for the constants of the compose file",
				filename, v.Name, value))
		}
	}

	return failures
}
