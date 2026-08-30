// Package contract verifies that the three artifacts produced by `make proto` -- the Go
// stubs, the OpenAPI contract and the Dart package -- all describe the same contract.
//
// This is the test for acceptance criterion 2 of PRD-00: the three are regenerated
// together, so if one falls behind the .proto sources it shows up here. Checking that
// nothing is left uncommitted is the job of scripts/proto-check.sh, which runs in CI.
package contract_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/nicodanke/dizen-v2-backend/pkg/genproto/dizen/common/v1"
	identityv1 "github.com/nicodanke/dizen-v2-backend/pkg/genproto/dizen/identity/v1"
)

// repoRoot returns the repository root, so gen/ can be read from inside the pkg module.
func repoRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("could not resolve the repository root: %v", err)
	}

	return root
}

func TestGoStubsExposeTheIdentityService(t *testing.T) {
	t.Parallel()

	const wantFullMethod = "/dizen.identity.v1.HealthService/HealthPing"

	if identityv1.HealthService_HealthPing_FullMethodName != wantFullMethod {
		t.Errorf("full method = %q, want %q",
			identityv1.HealthService_HealthPing_FullMethodName, wantFullMethod)
	}

	desc := identityv1.HealthService_ServiceDesc
	if desc.ServiceName != "dizen.identity.v1.HealthService" {
		t.Errorf("ServiceName = %q", desc.ServiceName)
	}

	if len(desc.Methods) != 1 {
		t.Fatalf("expected 1 method on HealthService, got %d", len(desc.Methods))
	}

	if desc.Methods[0].MethodName != "HealthPing" {
		t.Errorf("MethodName = %q, want HealthPing", desc.Methods[0].MethodName)
	}

	// The generated server forces embedding Unimplemented (require_unimplemented_servers),
	// which is what keeps adding an RPC from breaking the compilation of the services.
	var _ identityv1.HealthServiceServer = (*identityv1.UnimplementedHealthServiceServer)(nil)
}

func TestThePublicRPCCarriesItsHTTPAnnotation(t *testing.T) {
	t.Parallel()

	// Mapping rule from 03 section 1: Get* -> GET. The gateway is built from this
	// annotation, so if it disappears the dashboard REST surface silently breaks.
	method := identityv1.File_dizen_identity_v1_health_proto.
		Services().ByName("HealthService").
		Methods().ByName("HealthPing")

	if method == nil {
		t.Fatal("HealthPing was not found in the descriptor")
	}

	opts, ok := method.Options().(*descriptorpb.MethodOptions)
	if !ok {
		t.Fatalf("unexpected method options type: %T", method.Options())
	}

	rule, ok := proto.GetExtension(opts, annotations.E_Http).(*annotations.HttpRule)
	if !ok || rule == nil {
		t.Fatal("HealthPing has no google.api.http annotation")
	}

	if got := rule.GetGet(); got != "/v1/identity/health" {
		t.Errorf("REST path = %q, want GET /v1/identity/health", got)
	}
}

func TestEnumsHaveAnUnspecifiedZeroValue(t *testing.T) {
	t.Parallel()

	// buf lint enforces this (enum_zero_value_suffix), but the test pins it on the
	// generated side too: an enum without an explicit zero value breaks backward
	// compatibility as soon as a new value is added.
	enums := []protoreflect.EnumDescriptor{
		commonv1.AccessType(0).Descriptor(),
		commonv1.Platform(0).Descriptor(),
	}

	for _, enum := range enums {
		zero := enum.Values().ByNumber(0)
		if zero == nil {
			t.Errorf("%s has no zero value", enum.FullName())
			continue
		}

		if !strings.HasSuffix(string(zero.Name()), "_UNSPECIFIED") {
			t.Errorf("%s: zero value %q does not end in _UNSPECIFIED", enum.FullName(), zero.Name())
		}
	}
}

func TestTheResponseMessageRoundTrips(t *testing.T) {
	t.Parallel()

	original := &identityv1.HealthPingResponse{
		Service:    "identity",
		Version:    "v0.1.0",
		Commit:     "abc1234",
		ServerTime: timestamppb.Now(),
	}

	raw, err := proto.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded identityv1.HealthPingResponse
	if err := proto.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.GetService() != original.GetService() || decoded.GetCommit() != original.GetCommit() {
		t.Errorf("round trip changed the message: %+v", &decoded)
	}
}

func TestTheGeneratedOpenAPIDeclaresTheSamePath(t *testing.T) {
	t.Parallel()

	path := filepath.Join(repoRoot(t), "gen", "openapi", "identity.yaml")

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the identity OpenAPI contract is missing; run 'make proto': %v", err)
	}

	content := string(raw)

	for _, want := range []string{
		"openapi: 3.0.3",
		"/v1/identity/health:",
		"HealthService_HealthPing",
		"dizen.identity.v1.HealthPingResponse",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("gen/openapi/identity.yaml does not contain %q", want)
		}
	}
}

func TestTheDartPackageExportsTheIdentityStubs(t *testing.T) {
	t.Parallel()

	pkgDir := filepath.Join(repoRoot(t), "gen", "dart", "dizen_api")

	barrel, err := os.ReadFile(filepath.Join(pkgDir, "lib", "dizen_api.dart"))
	if err != nil {
		t.Fatalf("the Dart barrel file is missing; run 'make proto': %v", err)
	}

	for _, want := range []string{
		"src/generated/dizen/identity/v1/health.pbgrpc.dart",
		"src/generated/dizen/common/v1/geo.pb.dart",
	} {
		if !strings.Contains(string(barrel), want) {
			t.Errorf("lib/dizen_api.dart does not export %q", want)
		}
	}

	// The pubspec is what dizen-v2-mobile consumes, pinned to an api-v* tag.
	pubspec, err := os.ReadFile(filepath.Join(pkgDir, "pubspec.yaml"))
	if err != nil {
		t.Fatalf("the Dart pubspec.yaml is missing: %v", err)
	}

	if !strings.Contains(string(pubspec), "name: dizen_api") {
		t.Error("pubspec.yaml does not declare the dizen_api package")
	}
}
