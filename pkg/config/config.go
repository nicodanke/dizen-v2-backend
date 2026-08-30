// Package config loads typed configuration from a .env file and from environment
// variables, validates it, and fails at startup with an explicit message when a required
// variable is missing (RF-3).
//
// The contract is deliberately narrow: a service declares a struct, calls Load, and either
// gets a fully valid configuration or an error naming every field that is wrong. There is
// no partially valid configuration and no silent default for a required value.
package config

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/caarlos0/env/v11"
	"github.com/go-playground/validator/v10"
	"github.com/joho/godotenv"
)

// Environment is the deployment environment a service runs in. It gates behavior that
// must differ outside production: gRPC reflection, console logging, verbose errors.
type Environment string

const (
	// EnvLocal is a developer machine running docker compose.
	EnvLocal Environment = "local"
	// EnvStaging is the pre-production environment.
	EnvStaging Environment = "staging"
	// EnvProduction is the live environment.
	EnvProduction Environment = "production"
	// EnvTest is used by the test suites.
	EnvTest Environment = "test"
)

// IsProduction reports whether the service runs in production. Anything that leaks
// internals -- reflection, stack traces, pretty logging -- must be gated on this.
func (e Environment) IsProduction() bool {
	return e == EnvProduction
}

// IsLocal reports whether the service runs on a developer machine.
func (e Environment) IsLocal() bool {
	return e == EnvLocal
}

// String implements fmt.Stringer.
func (e Environment) String() string {
	return string(e)
}

// ErrInvalidConfig is returned when the configuration fails validation. Callers should
// treat it as fatal: it is checked with errors.Is in main.go before exiting.
var ErrInvalidConfig = errors.New("invalid configuration")

// Load reads the configuration into T.
//
// Order of precedence, lowest first: struct defaults, then the .env files given, then the
// process environment. Real environment variables always win, which is what lets docker
// compose and Dokploy override a committed .env without editing it.
//
// A missing .env file is not an error: in production the configuration comes from the
// environment and no such file exists.
func Load[T any](envFiles ...string) (*T, error) {
	loadEnvFiles(envFiles...)

	cfg := new(T)

	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}

	if err := Validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// loadEnvFiles loads the given .env files, ignoring the ones that do not exist.
func loadEnvFiles(envFiles ...string) {
	for _, file := range envFiles {
		if _, err := os.Stat(file); err != nil {
			continue
		}

		// godotenv.Load does not overwrite variables already set in the environment,
		// which is exactly the precedence we want.
		_ = godotenv.Load(file)
	}
}

// Validate runs the struct validation rules and turns the result into an error that names
// every offending field, so a misconfigured deployment is diagnosed in one restart instead
// of one variable at a time.
func Validate(cfg any) error {
	v := validator.New(validator.WithRequiredStructEnabled())

	// Report the env var name rather than the Go field name: that is what the operator
	// has to fix.
	v.RegisterTagNameFunc(func(field reflect.StructField) string {
		name, _, _ := strings.Cut(field.Tag.Get("env"), ",")
		if name == "" {
			return field.Name
		}

		return name
	})

	err := v.Struct(cfg)
	if err == nil {
		return nil
	}

	if _, ok := errors.AsType[*validator.InvalidValidationError](err); ok {
		return fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}

	var validationErrors validator.ValidationErrors
	if !errors.As(err, &validationErrors) {
		return fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}

	return fmt.Errorf("%w:\n%s", ErrInvalidConfig, describe(validationErrors))
}

// describe renders the validation failures as one indented line per variable.
func describe(validationErrors validator.ValidationErrors) string {
	var b strings.Builder

	for i, fieldErr := range validationErrors {
		if i > 0 {
			b.WriteString("\n")
		}

		fmt.Fprintf(&b, "  - %s: %s", fieldErr.Field(), explain(fieldErr))
	}

	return b.String()
}

// explain turns a validator tag into a sentence an operator can act on.
func explain(fieldErr validator.FieldError) string {
	switch fieldErr.Tag() {
	case "required":
		return "is required but was not set"
	case "oneof":
		return fmt.Sprintf("must be one of [%s], got %q", fieldErr.Param(), fieldErr.Value())
	case "min":
		return fmt.Sprintf("must be at least %s, got %v", fieldErr.Param(), fieldErr.Value())
	case "max":
		return fmt.Sprintf("must be at most %s, got %v", fieldErr.Param(), fieldErr.Value())
	case "url":
		return fmt.Sprintf("must be a valid URL, got %q", fieldErr.Value())
	case "hostname_port":
		return fmt.Sprintf("must be host:port, got %q", fieldErr.Value())
	case "gte":
		return fmt.Sprintf("must be >= %s, got %v", fieldErr.Param(), fieldErr.Value())
	case "lte":
		return fmt.Sprintf("must be <= %s, got %v", fieldErr.Param(), fieldErr.Value())
	default:
		return fmt.Sprintf("failed the %q rule, got %v", fieldErr.Tag(), fieldErr.Value())
	}
}
