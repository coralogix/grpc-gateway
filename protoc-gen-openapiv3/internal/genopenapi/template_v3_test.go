package genopenapi

import (
	"log"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/internal/descriptor"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// Mock descriptor.Field type to simulate a protobuf field
type MockField struct {
	Name string
}

func (f *MockField) GetName() string {
	return f.Name
}

func Test_generateOneOfCombinations2(t *testing.T) {
	t.Run("MultipleOneOfGroupsWithDifferentVariantNumbers", func(t *testing.T) {
		oneofGroups := map[string][]*descriptor.Field{
			"oneof_group_A": {
				{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{Name: proto.String("field_A1")}},
				{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{Name: proto.String("field_A2")}},
				{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{Name: proto.String("field_A3")}},
				{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{Name: proto.String("field_A4")}},
			},
			"oneof_group_B": {
				{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{Name: proto.String("field_B1")}},
				{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{Name: proto.String("field_B2")}},
			},
			"oneof_group_C": {
				{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{Name: proto.String("field_C1")}},
				{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{Name: proto.String("field_C2")}},
				{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{Name: proto.String("field_C3")}},
			},
		}

		result := generateOneOfCombinations(oneofGroups, "TestMessage")
		log.Printf("Result: %+v", result)

		if len(result) != 24 {
			t.Fatalf("Expected 4 combinations, got %d", len(result))
		}

	})
}
