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

func TestValidateAndCoerceJsonExample(t *testing.T) {
	// Define a struct for our table-driven tests
	type testCase struct {
		name          string
		inputString   string
		targetType    string
		expectedValue string
		wantErr       bool
	}

	// Define the test table
	tests := []testCase{
		// --- BOOLEAN TESTS ---
		{
			name:          "Bool_Success_Literal_True",
			inputString:   "true",
			targetType:    "boolean",
			expectedValue: "true",
			wantErr:       false,
		},
		{
			name:          "Bool_Success_Literal_False_Whitespace",
			inputString:   " FALSE ",
			targetType:    "boolean",
			expectedValue: "FALSE",
			wantErr:       false,
		},
		{
			name:          "Bool_Success_Coerced_Quoted",
			inputString:   "\"true\"",
			targetType:    "boolean",
			expectedValue: "true", // Stripped of quotes
			wantErr:       false,
		},
		{
			name:        "Bool_Fail_IsNumber",
			inputString: "123",
			targetType:  "boolean",
			wantErr:     true,
		},
		{
			name:        "Bool_Fail_IsString",
			inputString: "Yes",
			targetType:  "boolean",
			wantErr:     true,
		},

		// --- INTEGER TESTS ---
		{
			name:          "Int_Success_Literal",
			inputString:   "42",
			targetType:    "integer",
			expectedValue: "42",
			wantErr:       false,
		},
		{
			name:          "Int_Success_Coerced_Quoted",
			inputString:   "\"12345\"",
			targetType:    "integer",
			expectedValue: "12345", // Stripped of quotes, but value is int
			wantErr:       false,
		},
		{
			name:          "Int_Success_Coerced_DecimalZero",
			inputString:   "99.00",
			targetType:    "integer",
			expectedValue: "99", // Coerced to "99"
			wantErr:       false,
		},
		{
			name:        "Int_Fail_IsFloat",
			inputString: "12.3",
			targetType:  "integer",
			wantErr:     true,
		},
		{
			name:        "Int_Fail_IsString",
			inputString: "abc",
			targetType:  "integer",
			wantErr:     true,
		},
		{
			name:          "Int_Success_LargeNumber",
			inputString:   "9223372036854775807", // Max int64
			targetType:    "integer",
			expectedValue: "9223372036854775807",
			wantErr:       false,
		},

		// --- NUMBER/FLOAT/DOUBLE TESTS ---
		{
			name:          "Float_Success_Decimal",
			inputString:   "12.34",
			targetType:    "number",
			expectedValue: "12.34",
			wantErr:       false,
		},
		{
			name:          "Double_Success_ScientificNotation",
			inputString:   "1e-5",
			targetType:    "double",
			expectedValue: "1e-5",
			wantErr:       false,
		},
		{
			name:          "Float_Success_Coerced_Quoted",
			inputString:   "\"-7.89\"",
			targetType:    "float",
			expectedValue: "-7.89",
			wantErr:       false,
		},
		{
			name:          "Number_Success_IntegerInput",
			inputString:   "100",
			targetType:    "number",
			expectedValue: "100",
			wantErr:       false,
		},
		{
			name:        "Number_Fail_IsString",
			inputString: "not-a-num",
			targetType:  "number",
			wantErr:     true,
		},
		{
			name:        "Double_Fail_QuotedString",
			inputString: "\"not-a-num\"",
			targetType:  "double",
			wantErr:     true,
		},

		// --- STRING TESTS ---
		{
			name:          "String_Success_AlreadyQuoted",
			inputString:   "\"hello world\"",
			targetType:    "string",
			expectedValue: "\"hello world\"",
			wantErr:       false,
		},
		{
			name:          "String_Coerced_PlainText",
			inputString:   "plain text",
			targetType:    "string",
			expectedValue: "\"plain text\"", // Stringified
			wantErr:       false,
		},
		{
			name:          "String_Coerced_WithInternalQuotes",
			inputString:   "Example with \"internal\" quotes",
			targetType:    "string",
			expectedValue: "\"Example with \\\"internal\\\" quotes\"", // Stringified and escaped
			wantErr:       false,
		},
		{
			name:          "String_Coerced_BooleanLike",
			inputString:   "true",
			targetType:    "string",
			expectedValue: "\"true\"", // Stringified
			wantErr:       false,
		},
		{
			name:          "String_Coerced_NumberLike",
			inputString:   "123",
			targetType:    "string",
			expectedValue: "\"123\"", // Stringified
			wantErr:       false,
		},

		// --- EDGE CASE: Unknown Type ---
		{
			name:          "Edge_UnknownType_ReturnOriginal",
			inputString:   "someValue",
			targetType:    "unsupported",
			expectedValue: "someValue",
			wantErr:       false,
		},
	}

	// Iterate over the test table
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := validateAndCoerceJsonExample(tc.inputString, tc.targetType)

			// Check for expected error state
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateAndCoerceJsonExample(%q, %q) error state mismatch.\nExpected error: %v, Got error: %v",
					tc.inputString, tc.targetType, tc.wantErr, err)
			}

			// Check for expected value only if no error was expected
			if !tc.wantErr && actual != tc.expectedValue {
				t.Errorf("ValidateAndCoerceJsonExample(%q, %q) failed.\nExpected: %q\nActual:   %q",
					tc.inputString, tc.targetType, tc.expectedValue, actual)
			}
		})
	}
}
