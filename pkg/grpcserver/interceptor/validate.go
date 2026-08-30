package interceptor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"buf.build/go/protovalidate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Validate enforces the constraints declared in the protos (03 section 7).
//
// It uses protovalidate rather than the protoc-gen-validate the document names, because
// PGV is no longer maintained; protovalidate evaluates CEL rules straight from the
// descriptor and generates no code. Decision D-3 of PRD-00.
//
// It runs last, after auth: an unauthenticated caller must not be able to probe the shape
// of a request by reading validation errors.
func Validate(validator protovalidate.Validator) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		_ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		msg, ok := req.(proto.Message)
		if !ok {
			// Not a protobuf message: nothing to validate. This is not an error, it is
			// what a non-generated handler in a test looks like.
			return handler(ctx, req)
		}

		if err := validator.Validate(msg); err != nil {
			return nil, invalidArgument(err)
		}

		return handler(ctx, req)
	}
}

// NewValidator builds the validator shared by the whole server. It is built once at
// startup because compiling the CEL rules is expensive and the result is immutable.
func NewValidator() (protovalidate.Validator, error) {
	validator, err := protovalidate.New()
	if err != nil {
		return nil, fmt.Errorf("building the protovalidate validator: %w", err)
	}

	return validator, nil
}

// invalidArgument turns a validation failure into INVALID_ARGUMENT listing the offending
// fields. This detail is safe to return: it describes the request the client just sent, so
// it leaks nothing the caller does not already know.
func invalidArgument(err error) error {
	var validationErr *protovalidate.ValidationError
	if !errors.As(err, &validationErr) {
		return status.Error(codes.InvalidArgument, "invalid request")
	}

	violations := validationErr.Violations

	fields := make([]string, 0, len(violations))
	for _, v := range violations {
		path := v.Proto.GetField().String()
		if path == "" {
			path = v.Proto.GetRuleId()
		}

		fields = append(fields, fmt.Sprintf("%s: %s", path, v.Proto.GetMessage()))
	}

	return status.Errorf(codes.InvalidArgument, "invalid request: %s", strings.Join(fields, "; "))
}
