// This is a generated file - do not edit.
//
// Generated from dizen/common/v1/media.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

/// Reference to an image stored outside the database, in the S3-compatible bucket
/// (ADR A-5). The URL is signed and short-lived; the bucket is never public.
class MediaRef extends $pb.GeneratedMessage {
  factory MediaRef({
    $core.String? id,
    $core.String? url,
    $core.int? width,
    $core.int? height,
    $core.String? blurhash,
    $core.String? altText,
  }) {
    final result = create();
    if (id != null) result.id = id;
    if (url != null) result.url = url;
    if (width != null) result.width = width;
    if (height != null) result.height = height;
    if (blurhash != null) result.blurhash = blurhash;
    if (altText != null) result.altText = altText;
    return result;
  }

  MediaRef._();

  factory MediaRef.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory MediaRef.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'MediaRef',
      package:
          const $pb.PackageName(_omitMessageNames ? '' : 'dizen.common.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'id')
    ..aOS(2, _omitFieldNames ? '' : 'url')
    ..aI(3, _omitFieldNames ? '' : 'width')
    ..aI(4, _omitFieldNames ? '' : 'height')
    ..aOS(5, _omitFieldNames ? '' : 'blurhash')
    ..aOS(6, _omitFieldNames ? '' : 'altText')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MediaRef clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  MediaRef copyWith(void Function(MediaRef) updates) =>
      super.copyWith((message) => updates(message as MediaRef)) as MediaRef;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static MediaRef create() => MediaRef._();
  @$core.override
  MediaRef createEmptyInstance() => create();
  @$core.pragma('dart2js:noInline')
  static MediaRef getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<MediaRef>(create);
  static MediaRef? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get id => $_getSZ(0);
  @$pb.TagNumber(1)
  set id($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasId() => $_has(0);
  @$pb.TagNumber(1)
  void clearId() => $_clearField(1);

  /// Signed URL. It must not be cached beyond its expiry.
  @$pb.TagNumber(2)
  $core.String get url => $_getSZ(1);
  @$pb.TagNumber(2)
  set url($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasUrl() => $_has(1);
  @$pb.TagNumber(2)
  void clearUrl() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.int get width => $_getIZ(2);
  @$pb.TagNumber(3)
  set width($core.int value) => $_setSignedInt32(2, value);
  @$pb.TagNumber(3)
  $core.bool hasWidth() => $_has(2);
  @$pb.TagNumber(3)
  void clearWidth() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.int get height => $_getIZ(3);
  @$pb.TagNumber(4)
  set height($core.int value) => $_setSignedInt32(3, value);
  @$pb.TagNumber(4)
  $core.bool hasHeight() => $_has(3);
  @$pb.TagNumber(4)
  void clearHeight() => $_clearField(4);

  /// Low-resolution placeholder to paint while the image loads.
  @$pb.TagNumber(5)
  $core.String get blurhash => $_getSZ(4);
  @$pb.TagNumber(5)
  set blurhash($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasBlurhash() => $_has(4);
  @$pb.TagNumber(5)
  void clearBlurhash() => $_clearField(5);

  /// Alternative text for accessibility.
  @$pb.TagNumber(6)
  $core.String get altText => $_getSZ(5);
  @$pb.TagNumber(6)
  set altText($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasAltText() => $_has(5);
  @$pb.TagNumber(6)
  void clearAltText() => $_clearField(6);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
