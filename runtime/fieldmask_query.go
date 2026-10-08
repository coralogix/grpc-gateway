package runtime

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/grpc-ecosystem/grpc-gateway/v2/utilities"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	field_mask "google.golang.org/protobuf/types/known/fieldmaskpb"
)

const fieldMaskMessageName = protoreflect.FullName("google.protobuf.FieldMask")

// PopulateQueryParametersWithFieldMask parses query parameters into msg. It
// converts FieldMask paths from the target message's JSON names to its exact
// protobuf field names. targetFieldPath is the protobuf path from msg to the
// message described by the FieldMask.
//
// Existing clients can continue to send protobuf field names. Other query
// parameters use the configured QueryParameterParser without changes.
func PopulateQueryParametersWithFieldMask(msg proto.Message, values url.Values, filter *utilities.DoubleArray, targetFieldPath string) error {
	fieldMaskField, fieldMaskKey, err := findFieldMaskQueryParameter(msg.ProtoReflect().Descriptor(), values)
	if err != nil {
		return err
	}
	if fieldMaskField == nil {
		return currentQueryParser.Parse(msg, values, filter)
	}

	target, err := messageDescriptorAtPath(msg.ProtoReflect().Descriptor(), targetFieldPath)
	if err != nil {
		return err
	}

	normalizedValues := cloneURLValues(values)
	for i, value := range normalizedValues[fieldMaskKey] {
		normalizedValues[fieldMaskKey][i], err = normalizeFieldMaskQueryValue(value, target)
		if err != nil {
			return fmt.Errorf("parsing field %q: %w", fieldMaskField.FullName().Name(), err)
		}
	}

	return currentQueryParser.Parse(msg, normalizedValues, filter)
}

func cloneURLValues(values url.Values) url.Values {
	cloned := make(url.Values, len(values))
	for key, value := range values {
		cloned[key] = append([]string(nil), value...)
	}
	return cloned
}

func findFieldMaskQueryParameter(message protoreflect.MessageDescriptor, values url.Values) (protoreflect.FieldDescriptor, string, error) {
	var foundField protoreflect.FieldDescriptor
	var foundKey string
	fields := message.Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		if field.Message() == nil || field.Message().FullName() != fieldMaskMessageName {
			continue
		}

		keys := []string{field.JSONName()}
		if protoName := string(field.Name()); protoName != field.JSONName() {
			keys = append(keys, protoName)
		}
		for _, key := range keys {
			if _, ok := values[key]; !ok {
				continue
			}
			if foundField != nil {
				return nil, "", fmt.Errorf("field mask query parameter is set more than once: %q and %q", foundKey, key)
			}
			foundField = field
			foundKey = key
		}
	}
	return foundField, foundKey, nil
}

func messageDescriptorAtPath(message protoreflect.MessageDescriptor, fieldPath string) (protoreflect.MessageDescriptor, error) {
	if fieldPath == "" || fieldPath == "*" {
		return nil, fmt.Errorf("invalid field mask target path %q", fieldPath)
	}

	for _, fieldName := range strings.Split(fieldPath, ".") {
		field := message.Fields().ByName(protoreflect.Name(fieldName))
		if field == nil {
			return nil, fmt.Errorf("field mask target field %q not found in %q", fieldName, message.FullName())
		}
		if field.Message() == nil || field.IsList() || field.IsMap() {
			return nil, fmt.Errorf("field mask target field %q in %q is not a singular message", fieldName, message.FullName())
		}
		message = field.Message()
	}
	return message, nil
}

func normalizeFieldMaskQueryValue(value string, target protoreflect.MessageDescriptor) (string, error) {
	if value == "" {
		return "", fmt.Errorf("field mask must not be empty")
	}

	paths := strings.Split(value, ",")
	for i, path := range paths {
		if path == "*" {
			return "", fmt.Errorf("field mask wildcard is not supported")
		}
		normalized, err := normalizeFieldMaskQueryPath(path, target)
		if err != nil {
			return "", err
		}
		paths[i] = normalized
	}
	return strings.Join(paths, ","), nil
}

func normalizeFieldMaskQueryPath(path string, message protoreflect.MessageDescriptor) (string, error) {
	segments := strings.Split(path, ".")
	for _, segment := range segments {
		if segment == "" {
			return "", fmt.Errorf("invalid empty path segment in %q", path)
		}
	}

	for i, segment := range segments {
		field := message.Fields().ByJSONName(segment)
		if field == nil {
			field = message.Fields().ByName(protoreflect.Name(segment))
		}
		if field == nil {
			return "", fmt.Errorf("field %q not found in %q", segment, message.FullName())
		}

		segments[i] = string(field.Name())
		if i == len(segments)-1 {
			continue
		}
		if field.Message() == nil || field.IsList() || field.IsMap() {
			return "", fmt.Errorf("field %q in %q is not a singular message", segment, message.FullName())
		}
		if isDynamicProtoMessage(field.Message()) {
			return strings.Join(segments, "."), nil
		}
		message = field.Message()
	}
	return strings.Join(segments, "."), nil
}

// RequireFieldMaskQueryParameter returns an InvalidArgument error when mask has
// no non-empty path. Generated PATCH handlers call it after they parse the query,
// for a FieldMask that the request message lists as required. name is the
// protobuf name of the FieldMask field.
func RequireFieldMaskQueryParameter(mask *field_mask.FieldMask, name string) error {
	for _, path := range mask.GetPaths() {
		if path != "" {
			return nil
		}
	}
	return status.Errorf(codes.InvalidArgument, "missing required query parameter %q", name)
}
