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

func Test_sanitizeUrlPath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Standard Case",
			input:    "my/path/{some.url.path}",
			expected: "my/path/{path}",
		},
		{
			name:     "Multiple Dots Per Segment",
			input:    "a.b.c/d.e.f",
			expected: "c/f",
		},
		{
			name:     "No Dots Present",
			input:    "hello/world/test",
			expected: "hello/world/test",
		},
		{
			name:     "Leading and Trailing Slashes",
			input:    "/folder.name/file.ext/",
			expected: "/name/ext/",
		},
		{
			name:     "Empty String Input",
			input:    "",
			expected: "",
		},
		{
			name:     "Root Path Only",
			input:    "/",
			expected: "/",
		},
		{
			name:     "Segment is only Dots",
			input:    "..../path",
			expected: "/path",
		},
		{
			name:     "All Segments Have Dots",
			input:    "v1.2/api.users/get.list.json",
			expected: "2/users/json",
		},
		{
			name:     "Segments Containing Curly Braces (Placeholders)",
			input:    "templates/{user.id}/v1.0.0/data.json",
			expected: "templates/{id}/0/json",
		},
		{
			name:     "Double Slashes",
			input:    "path//with.double.slash",
			expected: "path//slash",
		},
	}

	// Iterate over the test cases
	for _, tt := range tests {
		// Run each test case as a subtest
		t.Run(tt.name, func(t *testing.T) {
			actual := sanitizeURLPath(tt.input)

			// Check if the actual output matches the expected output
			if actual != tt.expected {
				t.Errorf("sanitizeURLPath(%q) failed.\nExpected: %q\n  Actual: %q", tt.input, tt.expected, actual)
			}
		})
	}
}
