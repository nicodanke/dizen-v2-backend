// This is a generated file - do not edit.
//
// Generated from dizen/identity/v1/health.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:async' as $async;
import 'dart:core' as $core;

import 'package:grpc/service_api.dart' as $grpc;
import 'package:protobuf/protobuf.dart' as $pb;

import 'health.pb.dart' as $0;

export 'health.pb.dart';

/// Reference service for PRD-00 (RF-19): a single RPC that exercises the service template
/// end to end -- gRPC, REST gateway, interceptors, config and observability -- without
/// dragging in any domain logic. Real authentication arrives with PRD-01.
@$pb.GrpcServiceName('dizen.identity.v1.HealthService')
class HealthServiceClient extends $grpc.Client {
  /// The hostname for this service.
  static const $core.String defaultHost = '';

  /// OAuth scopes needed for the client.
  static const $core.List<$core.String> oauthScopes = [
    '',
  ];

  HealthServiceClient(super.channel, {super.options, super.interceptors});

  /// Returns the identity of the running build. This is a public method: it belongs to the
  /// auth interceptor allowlist (03 section 7).
  $grpc.ResponseFuture<$0.HealthPingResponse> healthPing(
    $0.HealthPingRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$healthPing, request, options: options);
  }

  // method descriptors

  static final _$healthPing =
      $grpc.ClientMethod<$0.HealthPingRequest, $0.HealthPingResponse>(
          '/dizen.identity.v1.HealthService/HealthPing',
          ($0.HealthPingRequest value) => value.writeToBuffer(),
          $0.HealthPingResponse.fromBuffer);
}

@$pb.GrpcServiceName('dizen.identity.v1.HealthService')
abstract class HealthServiceBase extends $grpc.Service {
  $core.String get $name => 'dizen.identity.v1.HealthService';

  HealthServiceBase() {
    $addMethod($grpc.ServiceMethod<$0.HealthPingRequest, $0.HealthPingResponse>(
        'HealthPing',
        healthPing_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.HealthPingRequest.fromBuffer(value),
        ($0.HealthPingResponse value) => value.writeToBuffer()));
  }

  $async.Future<$0.HealthPingResponse> healthPing_Pre($grpc.ServiceCall $call,
      $async.Future<$0.HealthPingRequest> $request) async {
    return healthPing($call, await $request);
  }

  $async.Future<$0.HealthPingResponse> healthPing(
      $grpc.ServiceCall call, $0.HealthPingRequest request);
}
