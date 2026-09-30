package runtime

import (
	"net/url"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime/internal/examplepb"
	"github.com/grpc-ecosystem/grpc-gateway/v2/utilities"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func TestPopulateQueryParametersWithFieldMask(t *testing.T) {
	tests := []struct {
		name            string
		msg             proto.Message
		targetFieldPath string
		values          url.Values
		want            []string
	}{
		{
			name:            "standard JSON name",
			msg:             &examplepb.NonStandardUpdateRequest{},
			targetFieldPath: "body",
			values:          url.Values{"updateMask": {"lineNum"}},
			want:            []string{"line_num"},
		},
		{
			name:            "nested JSON names",
			msg:             &examplepb.NonStandardUpdateRequest{},
			targetFieldPath: "body",
			values:          url.Values{"updateMask": {"thing.subThing.subValue"}},
			want:            []string{"thing.subThing.sub_value"},
		},
		{
			name:            "non-standard protobuf names",
			msg:             &examplepb.NonStandardUpdateRequest{},
			targetFieldPath: "body",
			values:          url.Values{"updateMask": {"langIdent,STATUS,enGB"}},
			want:            []string{"langIdent", "STATUS", "en_GB"},
		},
		{
			name:            "explicit JSON name overrides",
			msg:             &examplepb.NonStandardWithJSONNamesUpdateRequest{},
			targetFieldPath: "body",
			values:          url.Values{"updateMask": {"ID,LineNum,status,yes,Thingy.SubThing.sub_Value"}},
			want:            []string{"id", "line_num", "STATUS", "no", "thing.subThing.sub_value"},
		},
		{
			name:            "legacy protobuf names and parameter name",
			msg:             &examplepb.NonStandardUpdateRequest{},
			targetFieldPath: "body",
			values:          url.Values{"update_mask": {"line_num,thing.subThing.sub_value"}},
			want:            []string{"line_num", "thing.subThing.sub_value"},
		},
		{
			name:            "nested target message",
			msg:             &examplepb.NonStandardUpdateRequest{},
			targetFieldPath: "body.thing",
			values:          url.Values{"updateMask": {"subThing.subValue"}},
			want:            []string{"subThing.sub_value"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			originalValues := cloneURLValues(test.values)
			if err := PopulateQueryParametersWithFieldMask(test.msg, test.values, utilities.NewDoubleArray(nil), test.targetFieldPath); err != nil {
				t.Fatalf("PopulateQueryParametersWithFieldMask() failed: %v", err)
			}

			field := test.msg.ProtoReflect().Descriptor().Fields().ByName("update_mask")
			got := test.msg.ProtoReflect().Get(field).Message().Interface()
			want := &fieldmaskpb.FieldMask{Paths: test.want}
			if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
				t.Errorf("field mask mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(originalValues, test.values); diff != "" {
				t.Errorf("input query parameters changed (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPopulateQueryParametersWithFieldMaskRejectsInvalidPaths(t *testing.T) {
	for _, test := range []struct {
		name    string
		value   string
		wantErr string
	}{
		{
			name:    "unknown field",
			value:   "unknownField",
			wantErr: `field "unknownField" not found`,
		},
		{
			name:    "wildcard",
			value:   "*",
			wantErr: "field mask wildcard is not supported",
		},
		{
			name:    "empty value",
			value:   "",
			wantErr: "field mask must not be empty",
		},
		{
			name:    "empty nested segment",
			value:   "thing..subValue",
			wantErr: "invalid empty path segment",
		},
		{
			name:    "nested scalar",
			value:   "lineNum.child",
			wantErr: "is not a singular message",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			msg := &examplepb.NonStandardUpdateRequest{}
			err := PopulateQueryParametersWithFieldMask(
				msg,
				url.Values{"updateMask": {test.value}},
				utilities.NewDoubleArray(nil),
				"body",
			)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("PopulateQueryParametersWithFieldMask() error = %v, want an error containing %q", err, test.wantErr)
			}
		})
	}
}

func TestPopulateQueryParametersWithFieldMaskRejectsInvalidTargetPath(t *testing.T) {
	for _, test := range []struct {
		name            string
		targetFieldPath string
		wantErr         string
	}{
		{name: "empty", targetFieldPath: "", wantErr: `invalid field mask target path ""`},
		{name: "wildcard", targetFieldPath: "*", wantErr: `invalid field mask target path "*"`},
		{name: "unknown", targetFieldPath: "unknown", wantErr: `target field "unknown" not found`},
		{name: "scalar", targetFieldPath: "body.line_num", wantErr: `target field "line_num"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := PopulateQueryParametersWithFieldMask(
				&examplepb.NonStandardUpdateRequest{},
				url.Values{"updateMask": {"lineNum"}},
				utilities.NewDoubleArray(nil),
				test.targetFieldPath,
			)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("PopulateQueryParametersWithFieldMask() error = %v, want an error containing %q", err, test.wantErr)
			}
		})
	}
}

func TestPopulateQueryParametersWithFieldMaskRejectsDuplicateParameterNames(t *testing.T) {
	err := PopulateQueryParametersWithFieldMask(
		&examplepb.NonStandardUpdateRequest{},
		url.Values{
			"updateMask":  {"lineNum"},
			"update_mask": {"line_num"},
		},
		utilities.NewDoubleArray(nil),
		"body",
	)
	if err == nil || !strings.Contains(err.Error(), "field mask query parameter is set more than once") {
		t.Fatalf("PopulateQueryParametersWithFieldMask() error = %v, want duplicate parameter error", err)
	}
}

func TestPopulateQueryParametersWithFieldMaskDelegatesWithoutMask(t *testing.T) {
	msg := &examplepb.NonStandardUpdateRequest{}
	if err := PopulateQueryParametersWithFieldMask(
		msg,
		url.Values{"unknown": {"value"}},
		utilities.NewDoubleArray(nil),
		"invalid.target.path",
	); err != nil {
		t.Fatalf("PopulateQueryParametersWithFieldMask() failed without a field mask query parameter: %v", err)
	}
}

func TestPopulateQueryParametersKeepsLegacyFieldMaskBehavior(t *testing.T) {
	msg := &examplepb.Proto3Message{}
	if err := PopulateQueryParameters(
		msg,
		url.Values{"fieldmaskValue": {"fieldA,*"}},
		utilities.NewDoubleArray(nil),
	); err != nil {
		t.Fatalf("PopulateQueryParameters() failed: %v", err)
	}

	want := &fieldmaskpb.FieldMask{Paths: []string{"fieldA", "*"}}
	if diff := cmp.Diff(want, msg.FieldmaskValue, protocmp.Transform()); diff != "" {
		t.Errorf("legacy field mask behavior changed (-want +got):\n%s", diff)
	}
}

func TestNormalizeFieldMaskQueryPathSupportsAlertNamesAndJSONOverride(t *testing.T) {
	_, message := fieldMaskTestMessageDescriptors(t)
	for _, test := range []struct {
		name string
		path string
		want string
	}{
		{name: "standard name", path: "fieldA", want: "field_a"},
		{name: "nested standard names", path: "nestedField.subFieldB", want: "nested_field.sub_field_b"},
		{name: "isNoData", path: "isNoData", want: "isNoData"},
		{name: "logExample", path: "logExample", want: "logExample"},
		{name: "triggeredRoutingOverrides", path: "triggeredRoutingOverrides", want: "triggeredRoutingOverrides"},
		{name: "resolvedRouteOverrides", path: "resolvedRouteOverrides", want: "resolvedRouteOverrides"},
		{name: "explicit JSON name override", path: "customField", want: "canonical_name"},
		{name: "legacy protobuf name", path: "canonical_name", want: "canonical_name"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeFieldMaskQueryPath(test.path, message)
			if err != nil {
				t.Fatalf("normalizeFieldMaskQueryPath() failed: %v", err)
			}
			if got != test.want {
				t.Errorf("normalizeFieldMaskQueryPath() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPopulateQueryParametersWithFieldMaskSupportsAlertNamesAndJSONOverride(t *testing.T) {
	request, _ := fieldMaskTestMessageDescriptors(t)
	msg := dynamicpb.NewMessage(request)
	if err := PopulateQueryParametersWithFieldMask(
		msg,
		url.Values{"updateMask": {"fieldA,nestedField.subFieldB,isNoData,logExample,triggeredRoutingOverrides,resolvedRouteOverrides,customField,canonical_name"}},
		utilities.NewDoubleArray(nil),
		"body",
	); err != nil {
		t.Fatalf("PopulateQueryParametersWithFieldMask() failed: %v", err)
	}

	maskField := request.Fields().ByName("update_mask")
	mask := msg.Get(maskField).Message()
	paths := mask.Get(mask.Descriptor().Fields().ByName("paths")).List()
	got := make([]string, paths.Len())
	for i := range got {
		got[i] = paths.Get(i).String()
	}
	want := []string{
		"field_a",
		"nested_field.sub_field_b",
		"isNoData",
		"logExample",
		"triggeredRoutingOverrides",
		"resolvedRouteOverrides",
		"canonical_name",
		"canonical_name",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("field mask mismatch (-want +got):\n%s", diff)
	}
}

func fieldMaskTestMessageDescriptors(t *testing.T) (protoreflect.MessageDescriptor, protoreflect.MessageDescriptor) {
	t.Helper()
	label := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	stringType := descriptorpb.FieldDescriptorProto_TYPE_STRING
	boolType := descriptorpb.FieldDescriptorProto_TYPE_BOOL
	messageType := descriptorpb.FieldDescriptorProto_TYPE_MESSAGE

	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:       proto.String("runtime/fieldmask_query_test.proto"),
		Package:    proto.String("grpc.gateway.runtime.test"),
		Syntax:     proto.String("proto3"),
		Dependency: []string{"google/protobuf/field_mask.proto"},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Nested"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{Name: proto.String("sub_field_b"), JsonName: proto.String("subFieldB"), Number: proto.Int32(1), Label: &label, Type: &stringType},
				},
			},
			{
				Name: proto.String("Resource"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{Name: proto.String("field_a"), JsonName: proto.String("fieldA"), Number: proto.Int32(1), Label: &label, Type: &stringType},
					{Name: proto.String("nested_field"), JsonName: proto.String("nestedField"), Number: proto.Int32(2), Label: &label, Type: &messageType, TypeName: proto.String(".grpc.gateway.runtime.test.Nested")},
					{Name: proto.String("isNoData"), JsonName: proto.String("isNoData"), Number: proto.Int32(3), Label: &label, Type: &boolType},
					{Name: proto.String("logExample"), JsonName: proto.String("logExample"), Number: proto.Int32(4), Label: &label, Type: &stringType},
					{Name: proto.String("triggeredRoutingOverrides"), JsonName: proto.String("triggeredRoutingOverrides"), Number: proto.Int32(5), Label: &label, Type: &stringType},
					{Name: proto.String("resolvedRouteOverrides"), JsonName: proto.String("resolvedRouteOverrides"), Number: proto.Int32(6), Label: &label, Type: &stringType},
					{Name: proto.String("canonical_name"), JsonName: proto.String("customField"), Number: proto.Int32(7), Label: &label, Type: &stringType},
				},
			},
			{
				Name: proto.String("UpdateRequest"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{Name: proto.String("body"), JsonName: proto.String("body"), Number: proto.Int32(1), Label: &label, Type: &messageType, TypeName: proto.String(".grpc.gateway.runtime.test.Resource")},
					{Name: proto.String("update_mask"), JsonName: proto.String("updateMask"), Number: proto.Int32(2), Label: &label, Type: &messageType, TypeName: proto.String(".google.protobuf.FieldMask")},
				},
			},
		},
	}, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatalf("protodesc.NewFile() failed: %v", err)
	}
	return file.Messages().ByName("UpdateRequest"), file.Messages().ByName("Resource")
}
