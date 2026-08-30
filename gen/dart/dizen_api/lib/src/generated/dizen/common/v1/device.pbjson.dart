// This is a generated file - do not edit.
//
// Generated from dizen/common/v1/device.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports
// ignore_for_file: unused_import

import 'dart:convert' as $convert;
import 'dart:core' as $core;
import 'dart:typed_data' as $typed_data;

@$core.Deprecated('Use platformDescriptor instead')
const Platform$json = {
  '1': 'Platform',
  '2': [
    {'1': 'PLATFORM_UNSPECIFIED', '2': 0},
    {'1': 'PLATFORM_IOS', '2': 1},
    {'1': 'PLATFORM_ANDROID', '2': 2},
    {'1': 'PLATFORM_WEB', '2': 3},
  ],
};

/// Descriptor for `Platform`. Decode as a `google.protobuf.EnumDescriptorProto`.
final $typed_data.Uint8List platformDescriptor = $convert.base64Decode(
    'CghQbGF0Zm9ybRIYChRQTEFURk9STV9VTlNQRUNJRklFRBAAEhAKDFBMQVRGT1JNX0lPUxABEh'
    'QKEFBMQVRGT1JNX0FORFJPSUQQAhIQCgxQTEFURk9STV9XRUIQAw==');

@$core.Deprecated('Use deviceInfoDescriptor instead')
const DeviceInfo$json = {
  '1': 'DeviceInfo',
  '2': [
    {'1': 'device_id', '3': 1, '4': 1, '5': 9, '8': {}, '10': 'deviceId'},
    {
      '1': 'platform',
      '3': 2,
      '4': 1,
      '5': 14,
      '6': '.dizen.common.v1.Platform',
      '10': 'platform'
    },
    {'1': 'model', '3': 3, '4': 1, '5': 9, '8': {}, '10': 'model'},
    {'1': 'app_version', '3': 4, '4': 1, '5': 9, '8': {}, '10': 'appVersion'},
    {'1': 'os_version', '3': 5, '4': 1, '5': 9, '8': {}, '10': 'osVersion'},
  ],
};

/// Descriptor for `DeviceInfo`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List deviceInfoDescriptor = $convert.base64Decode(
    'CgpEZXZpY2VJbmZvEicKCWRldmljZV9pZBgBIAEoCUIKukgHcgUQARiAAVIIZGV2aWNlSWQSNQ'
    'oIcGxhdGZvcm0YAiABKA4yGS5kaXplbi5jb21tb24udjEuUGxhdGZvcm1SCHBsYXRmb3JtEh4K'
    'BW1vZGVsGAMgASgJQgi6SAVyAxiAAVIFbW9kZWwSKAoLYXBwX3ZlcnNpb24YBCABKAlCB7pIBH'
    'ICGCBSCmFwcFZlcnNpb24SJgoKb3NfdmVyc2lvbhgFIAEoCUIHukgEcgIYIFIJb3NWZXJzaW9u');
