// This is a generated file - do not edit.
//
// Generated from dizen/identity/v1/health.proto.

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

@$core.Deprecated('Use healthPingRequestDescriptor instead')
const HealthPingRequest$json = {
  '1': 'HealthPingRequest',
};

/// Descriptor for `HealthPingRequest`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List healthPingRequestDescriptor =
    $convert.base64Decode('ChFIZWFsdGhQaW5nUmVxdWVzdA==');

@$core.Deprecated('Use healthPingResponseDescriptor instead')
const HealthPingResponse$json = {
  '1': 'HealthPingResponse',
  '2': [
    {'1': 'service', '3': 1, '4': 1, '5': 9, '10': 'service'},
    {'1': 'version', '3': 2, '4': 1, '5': 9, '10': 'version'},
    {'1': 'commit', '3': 3, '4': 1, '5': 9, '10': 'commit'},
    {
      '1': 'server_time',
      '3': 4,
      '4': 1,
      '5': 11,
      '6': '.google.protobuf.Timestamp',
      '10': 'serverTime'
    },
  ],
};

/// Descriptor for `HealthPingResponse`. Decode as a `google.protobuf.DescriptorProto`.
final $typed_data.Uint8List healthPingResponseDescriptor = $convert.base64Decode(
    'ChJIZWFsdGhQaW5nUmVzcG9uc2USGAoHc2VydmljZRgBIAEoCVIHc2VydmljZRIYCgd2ZXJzaW'
    '9uGAIgASgJUgd2ZXJzaW9uEhYKBmNvbW1pdBgDIAEoCVIGY29tbWl0EjsKC3NlcnZlcl90aW1l'
    'GAQgASgLMhouZ29vZ2xlLnByb3RvYnVmLlRpbWVzdGFtcFIKc2VydmVyVGltZQ==');
