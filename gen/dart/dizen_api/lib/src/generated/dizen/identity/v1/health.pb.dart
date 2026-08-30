// This is a generated file - do not edit.
//
// Generated from dizen/identity/v1/health.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;
import 'package:protobuf/well_known_types/google/protobuf/timestamp.pb.dart'
    as $1;

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

class HealthPingRequest extends $pb.GeneratedMessage {
  factory HealthPingRequest() => create();

  HealthPingRequest._();

  factory HealthPingRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory HealthPingRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'HealthPingRequest',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'dizen.identity.v1'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  HealthPingRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  HealthPingRequest copyWith(void Function(HealthPingRequest) updates) =>
      super.copyWith((message) => updates(message as HealthPingRequest))
          as HealthPingRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static HealthPingRequest create() => HealthPingRequest._();
  @$core.override
  HealthPingRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static HealthPingRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<HealthPingRequest>(create);
  static HealthPingRequest? _defaultInstance;
}

class HealthPingResponse extends $pb.GeneratedMessage {
  factory HealthPingResponse({
    $core.String? service,
    $core.String? version,
    $core.String? commit,
    $1.Timestamp? serverTime,
  }) {
    final result = create();
    if (service != null) result.service = service;
    if (version != null) result.version = version;
    if (commit != null) result.commit = commit;
    if (serverTime != null) result.serverTime = serverTime;
    return result;
  }

  HealthPingResponse._();

  factory HealthPingResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory HealthPingResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'HealthPingResponse',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'dizen.identity.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'service')
    ..aOS(2, _omitFieldNames ? '' : 'version')
    ..aOS(3, _omitFieldNames ? '' : 'commit')
    ..aOM<$1.Timestamp>(4, _omitFieldNames ? '' : 'serverTime',
        subBuilder: $1.Timestamp.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  HealthPingResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  HealthPingResponse copyWith(void Function(HealthPingResponse) updates) =>
      super.copyWith((message) => updates(message as HealthPingResponse))
          as HealthPingResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static HealthPingResponse create() => HealthPingResponse._();
  @$core.override
  HealthPingResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static HealthPingResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<HealthPingResponse>(create);
  static HealthPingResponse? _defaultInstance;

  /// Name of the service answering the call.
  @$pb.TagNumber(1)
  $core.String get service => $_getSZ(0);
  @$pb.TagNumber(1)
  set service($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasService() => $_has(0);
  @$pb.TagNumber(1)
  void clearService() => $_clearField(1);

  /// Build version (git tag, or "dev").
  @$pb.TagNumber(2)
  $core.String get version => $_getSZ(1);
  @$pb.TagNumber(2)
  set version($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasVersion() => $_has(1);
  @$pb.TagNumber(2)
  void clearVersion() => $_clearField(2);

  /// Commit the binary was built from.
  @$pb.TagNumber(3)
  $core.String get commit => $_getSZ(2);
  @$pb.TagNumber(3)
  set commit($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasCommit() => $_has(2);
  @$pb.TagNumber(3)
  void clearCommit() => $_clearField(3);

  /// Server time, in UTC.
  @$pb.TagNumber(4)
  $1.Timestamp get serverTime => $_getN(3);
  @$pb.TagNumber(4)
  set serverTime($1.Timestamp value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasServerTime() => $_has(3);
  @$pb.TagNumber(4)
  void clearServerTime() => $_clearField(4);
  @$pb.TagNumber(4)
  $1.Timestamp ensureServerTime() => $_ensure(3);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
