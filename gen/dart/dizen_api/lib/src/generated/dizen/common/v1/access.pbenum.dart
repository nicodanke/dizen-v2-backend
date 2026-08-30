// This is a generated file - do not edit.
//
// Generated from dizen/common/v1/access.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

/// How a piece of content is accessed. Values carry the enum prefix because in protobuf
/// enum values share the namespace of the package that contains them.
class AccessType extends $pb.ProtobufEnum {
  static const AccessType ACCESS_TYPE_UNSPECIFIED =
      AccessType._(0, _omitEnumNames ? '' : 'ACCESS_TYPE_UNSPECIFIED');
  static const AccessType ACCESS_TYPE_FREE =
      AccessType._(1, _omitEnumNames ? '' : 'ACCESS_TYPE_FREE');
  static const AccessType ACCESS_TYPE_PREMIUM =
      AccessType._(2, _omitEnumNames ? '' : 'ACCESS_TYPE_PREMIUM');
  static const AccessType ACCESS_TYPE_SUBSCRIPTION =
      AccessType._(3, _omitEnumNames ? '' : 'ACCESS_TYPE_SUBSCRIPTION');

  static const $core.List<AccessType> values = <AccessType>[
    ACCESS_TYPE_UNSPECIFIED,
    ACCESS_TYPE_FREE,
    ACCESS_TYPE_PREMIUM,
    ACCESS_TYPE_SUBSCRIPTION,
  ];

  static final $core.List<AccessType?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static AccessType? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const AccessType._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
