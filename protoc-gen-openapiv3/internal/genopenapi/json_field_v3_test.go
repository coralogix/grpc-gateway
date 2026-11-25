package genopenapi

import (
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/internal/descriptor"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func jsonFieldWithOption(t *testing.T, schema string) *descriptor.Field {
	t.Helper()
	inner := protowire.AppendTag(nil, 1, protowire.BytesType)
	inner = protowire.AppendString(inner, schema)
	raw := protowire.AppendTag(nil, jsonFieldExtensionNumber, protowire.BytesType)
	raw = protowire.AppendBytes(raw, inner)
	opts := &descriptorpb.FieldOptions{}
	if err := proto.Unmarshal(raw, opts); err != nil {
		t.Fatal(err)
	}
	return &descriptor.Field{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{Name: proto.String("policy"), Options: opts}}
}

func TestJSONFieldSchemaPath(t *testing.T) {
	got, ok := jsonFieldSchemaPath(jsonFieldWithOption(t, "schemas/policy.schema.json"))
	if !ok || got != "schemas/policy.schema.json" {
		t.Fatalf("got %q, %v", got, ok)
	}
	plain := &descriptor.Field{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{Name: proto.String("policy")}}
	if _, ok := jsonFieldSchemaPath(plain); ok {
		t.Fatal("field without the option must not match")
	}
}

func TestJSONSiblingName(t *testing.T) {
	for in, want := range map[string]string{"policy": "policyJson", "access_policy": "access_policy_json", "accessPolicy": "accessPolicyJson"} {
		if got := jsonSiblingName(in); got != want {
			t.Errorf("jsonSiblingName(%q) = %q, want %q", in, got, want)
		}
	}
}
