package genopenapi

import (
	"os"
	"path/filepath"
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

func TestJSONFieldSiblingMarksTarget(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "demo", "p.schema.json"), []byte(`{"type":"object"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	field := jsonFieldWithOption(t, "p.schema.json")
	field.Type = descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()
	field.Label = descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()
	field.Message = &descriptor.Message{
		DescriptorProto: &descriptorpb.DescriptorProto{Name: proto.String("Req")},
		File:            &descriptor.File{FileDescriptorProto: &descriptorpb.FileDescriptorProto{Name: proto.String("demo/x.proto")}},
	}
	reg := descriptor.NewRegistry()
	reg.SetJSONSchemaRoot(dir)
	name, schema, ok := jsonFieldSibling(field, reg)
	if !ok || name != "policyJson" {
		t.Fatalf("got %q, %v", name, ok)
	}
	if got := schema.OpenAPIV3Extensions[JSONTargetExtension]; got != "policy" {
		t.Fatalf("target extension = %v", got)
	}
}
