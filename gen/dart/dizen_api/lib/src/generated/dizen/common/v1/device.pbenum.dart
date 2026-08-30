// This is a generated file - do not edit.
//
// Generated from dizen/common/v1/device.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

/// Platform of the device originating the call.
class Platform extends $pb.ProtobufEnum {
  static const Platform PLATFORM_UNSPECIFIED =
      Platform._(0, _omitEnumNames ? '' : 'PLATFORM_UNSPECIFIED');
  static const Platform PLATFORM_IOS =
      Platform._(1, _omitEnumNames ? '' : 'PLATFORM_IOS');
  static const Platform PLATFORM_ANDROID =
      Platform._(2, _omitEnumNames ? '' : 'PLATFORM_ANDROID');
  static const Platform PLATFORM_WEB =
      Platform._(3, _omitEnumNames ? '' : 'PLATFORM_WEB');

  static const $core.List<Platform> values = <Platform>[
    PLATFORM_UNSPECIFIED,
    PLATFORM_IOS,
    PLATFORM_ANDROID,
    PLATFORM_WEB,
  ];

  static final $core.List<Platform?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static Platform? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const Platform._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
