// This is a generated file - do not edit.
//
// Generated from dizen/common/v1/pagination.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

/// Cursor-based pagination (03 section 1). The token is opaque to the client: it is sent
/// back exactly as it arrived in next_page_token, without being interpreted.
class PageRequest extends $pb.GeneratedMessage {
  factory PageRequest({
    $core.int? pageSize,
    $core.String? pageToken,
  }) {
    final result = create();
    if (pageSize != null) result.pageSize = pageSize;
    if (pageToken != null) result.pageToken = pageToken;
    return result;
  }

  PageRequest._();

  factory PageRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory PageRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'PageRequest',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'dizen.common.v1'),
      createEmptyInstance: create)
    ..aI(1, _omitFieldNames ? '' : 'pageSize')
    ..aOS(2, _omitFieldNames ? '' : 'pageToken')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PageRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PageRequest copyWith(void Function(PageRequest) updates) =>
      super.copyWith((message) => updates(message as PageRequest))
          as PageRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static PageRequest create() => PageRequest._();
  @$core.override
  PageRequest createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static PageRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<PageRequest>(create);
  static PageRequest? _defaultInstance;

  /// Number of items requested. 0 uses the service default.
  @$pb.TagNumber(1)
  $core.int get pageSize => $_getIZ(0);
  @$pb.TagNumber(1)
  set pageSize($core.int value) => $_setSignedInt32(0, value);
  @$pb.TagNumber(1)
  $core.bool hasPageSize() => $_has(0);
  @$pb.TagNumber(1)
  void clearPageSize() => $_clearField(1);

  /// Cursor returned by the previous call. Empty requests the first page.
  @$pb.TagNumber(2)
  $core.String get pageToken => $_getSZ(1);
  @$pb.TagNumber(2)
  set pageToken($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasPageToken() => $_has(1);
  @$pb.TagNumber(2)
  void clearPageToken() => $_clearField(2);
}

/// Pagination metadata returned by every list response.
class PageResponse extends $pb.GeneratedMessage {
  factory PageResponse({
    $core.String? nextPageToken,
    $core.int? totalSize,
  }) {
    final result = create();
    if (nextPageToken != null) result.nextPageToken = nextPageToken;
    if (totalSize != null) result.totalSize = totalSize;
    return result;
  }

  PageResponse._();

  factory PageResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory PageResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'PageResponse',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'dizen.common.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'nextPageToken')
    ..aI(2, _omitFieldNames ? '' : 'totalSize')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PageResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PageResponse copyWith(void Function(PageResponse) updates) =>
      super.copyWith((message) => updates(message as PageResponse))
          as PageResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static PageResponse create() => PageResponse._();
  @$core.override
  PageResponse createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static PageResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<PageResponse>(create);
  static PageResponse? _defaultInstance;

  /// Cursor for the next page. Empty means there are no more results.
  @$pb.TagNumber(1)
  $core.String get nextPageToken => $_getSZ(0);
  @$pb.TagNumber(1)
  set nextPageToken($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasNextPageToken() => $_has(0);
  @$pb.TagNumber(1)
  void clearNextPageToken() => $_clearField(1);

  /// Total number of items matching the filter. Only populated when it is cheap to
  /// compute (03 section 1); otherwise it stays 0.
  @$pb.TagNumber(2)
  $core.int get totalSize => $_getIZ(1);
  @$pb.TagNumber(2)
  set totalSize($core.int value) => $_setSignedInt32(1, value);
  @$pb.TagNumber(2)
  $core.bool hasTotalSize() => $_has(1);
  @$pb.TagNumber(2)
  void clearTotalSize() => $_clearField(2);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
