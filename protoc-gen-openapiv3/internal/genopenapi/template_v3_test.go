package genopenapi

import (
	"log"
	"slices"
	"strings"
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

	t.Run("CollisionWithExistingTypeName", func(t *testing.T) {
		// This tests the scenario where a oneOf field name + message name creates a collision
		// with an existing type name.
		// E.g., message ColorsBy with field ColorsByAggregation aggregation would generate
		// "ColorsByAggregation" as the combination name, colliding with the actual nested message type.
		oneofGroups := map[string][]*descriptor.Field{
			"value": {
				{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{Name: proto.String("stack")}},
				{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{Name: proto.String("aggregation")}},
			},
		}

		// Simulate existing type names that could collide
		resolvedNames := map[string]string{
			".example.ColorsBy.ColorsByAggregation": "ColorsByAggregation",
			".example.ColorsBy.ColorsByStack":       "ColorsByStack",
		}

		result := generateOneOfCombinationsWithResolvedNames(oneofGroups, "ColorsBy", resolvedNames)

		if len(result) != 2 {
			t.Fatalf("Expected 2 combinations, got %d", len(result))
		}

		// Check that the collision is avoided by adding "Variant" suffix
		if _, ok := result["ColorsByAggregationVariant"]; !ok {
			t.Errorf("Expected 'ColorsByAggregationVariant' to exist due to collision avoidance, got keys: %v", result)
		}
		if _, ok := result["ColorsByStackVariant"]; !ok {
			t.Errorf("Expected 'ColorsByStackVariant' to exist due to collision avoidance, got keys: %v", result)
		}
		// Ensure the original colliding names are NOT used
		if _, ok := result["ColorsByAggregation"]; ok {
			t.Errorf("Expected 'ColorsByAggregation' to NOT exist (should be renamed to avoid collision), got keys: %v", result)
		}
		if _, ok := result["ColorsByStack"]; ok {
			t.Errorf("Expected 'ColorsByStack' to NOT exist (should be renamed to avoid collision), got keys: %v", result)
		}
	})

	t.Run("NoCollision_NoVariantSuffix", func(t *testing.T) {
		// When there's no collision, the Variant suffix should NOT be added
		oneofGroups := map[string][]*descriptor.Field{
			"value": {
				{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{Name: proto.String("foo")}},
				{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{Name: proto.String("bar")}},
			},
		}

		// No colliding names - these don't match the generated combination names
		resolvedNames := map[string]string{
			".example.SomeOtherType": "SomeOtherType",
		}

		result := generateOneOfCombinationsWithResolvedNames(oneofGroups, "MyMessage", resolvedNames)

		if len(result) != 2 {
			t.Fatalf("Expected 2 combinations, got %d", len(result))
		}

		// Check that no "Variant" suffix is added when there's no collision
		if _, ok := result["MyMessageFoo"]; !ok {
			t.Errorf("Expected 'MyMessageFoo' to exist (no collision, no Variant suffix), got keys: %v", result)
		}
		if _, ok := result["MyMessageBar"]; !ok {
			t.Errorf("Expected 'MyMessageBar' to exist (no collision, no Variant suffix), got keys: %v", result)
		}
		// Ensure Variant suffix is NOT added
		if _, ok := result["MyMessageFooVariant"]; ok {
			t.Errorf("Expected 'MyMessageFooVariant' to NOT exist (no collision should mean no Variant suffix), got keys: %v", result)
		}
		if _, ok := result["MyMessageBarVariant"]; ok {
			t.Errorf("Expected 'MyMessageBarVariant' to NOT exist (no collision should mean no Variant suffix), got keys: %v", result)
		}
	})

	t.Run("PartialCollision_OnlyCollidingGetsVariant", func(t *testing.T) {
		// When only some combinations collide, only those should get the Variant suffix
		oneofGroups := map[string][]*descriptor.Field{
			"value": {
				{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{Name: proto.String("collides")}},
				{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{Name: proto.String("no_collision")}},
			},
		}

		// Only one name collides
		resolvedNames := map[string]string{
			".example.Msg.MsgCollides": "MsgCollides", // This collides with "Msg_collides" -> "MsgCollides"
		}

		result := generateOneOfCombinationsWithResolvedNames(oneofGroups, "Msg", resolvedNames)

		if len(result) != 2 {
			t.Fatalf("Expected 2 combinations, got %d", len(result))
		}

		// The colliding name should get Variant suffix
		if _, ok := result["MsgCollidesVariant"]; !ok {
			t.Errorf("Expected 'MsgCollidesVariant' to exist due to collision, got keys: %v", result)
		}
		// The non-colliding name should NOT get Variant suffix
		if _, ok := result["MsgNoCollision"]; !ok {
			t.Errorf("Expected 'MsgNoCollision' to exist (no collision, no Variant), got keys: %v", result)
		}
		// Ensure the original colliding name is not used
		if _, ok := result["MsgCollides"]; ok {
			t.Errorf("Expected 'MsgCollides' to NOT exist, got keys: %v", result)
		}
	})

	t.Run("EmptyResolvedNames_NoVariantSuffix", func(t *testing.T) {
		// With empty resolvedNames, there can be no collisions
		oneofGroups := map[string][]*descriptor.Field{
			"value": {
				{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{Name: proto.String("alpha")}},
				{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{Name: proto.String("beta")}},
			},
		}

		resolvedNames := map[string]string{}

		result := generateOneOfCombinationsWithResolvedNames(oneofGroups, "Test", resolvedNames)

		if len(result) != 2 {
			t.Fatalf("Expected 2 combinations, got %d", len(result))
		}

		if _, ok := result["TestAlpha"]; !ok {
			t.Errorf("Expected 'TestAlpha' to exist, got keys: %v", result)
		}
		if _, ok := result["TestBeta"]; !ok {
			t.Errorf("Expected 'TestBeta' to exist, got keys: %v", result)
		}
	})

	t.Run("NilResolvedNames_NoVariantSuffix", func(t *testing.T) {
		// With nil resolvedNames (backward compatibility), there should be no collisions
		oneofGroups := map[string][]*descriptor.Field{
			"value": {
				{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{Name: proto.String("gamma")}},
			},
		}

		result := generateOneOfCombinationsWithResolvedNames(oneofGroups, "Test", nil)

		if len(result) != 1 {
			t.Fatalf("Expected 1 combination, got %d", len(result))
		}

		if _, ok := result["TestGamma"]; !ok {
			t.Errorf("Expected 'TestGamma' to exist, got keys: %v", result)
		}
	})

	t.Run("CollisionWithDottedTypeName", func(t *testing.T) {
		// This tests collision detection when resolved names contain dots.
		// E.g., "Annotation.WidgetScope.SpecificWidgets" should collide with
		// "AnnotationWidgetScopeSpecificWidgets" because some code generators
		// strip dots when comparing names.
		oneofGroups := map[string][]*descriptor.Field{
			"value": {
				{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{Name: proto.String("specific_widgets")}},
				{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{Name: proto.String("all_widgets")}},
			},
		}

		// The resolved name has dots, but when converted to PascalCase it would
		// collide with the generated combination name
		resolvedNames := map[string]string{
			".example.Annotation.WidgetScope.SpecificWidgets": "Annotation.WidgetScope.SpecificWidgets",
		}

		result := generateOneOfCombinationsWithResolvedNames(oneofGroups, "Annotation.WidgetScope", resolvedNames)

		if len(result) != 2 {
			t.Fatalf("Expected 2 combinations, got %d", len(result))
		}

		// The colliding name should get Variant suffix
		// "Annotation.WidgetScope_specific_widgets" -> "AnnotationWidgetScopeSpecificWidgets"
		// which collides with toPascalCase("Annotation.WidgetScope.SpecificWidgets") = "AnnotationWidgetScopeSpecificWidgets"
		if _, ok := result["AnnotationWidgetScopeSpecificWidgetsVariant"]; !ok {
			t.Errorf("Expected 'AnnotationWidgetScopeSpecificWidgetsVariant' to exist due to collision with dotted type name, got keys: %v", result)
		}
		// The non-colliding name should NOT get Variant suffix
		if _, ok := result["AnnotationWidgetScopeAllWidgets"]; !ok {
			t.Errorf("Expected 'AnnotationWidgetScopeAllWidgets' to exist (no collision), got keys: %v", result)
		}
		// Ensure the colliding name without Variant is not used
		if _, ok := result["AnnotationWidgetScopeSpecificWidgets"]; ok {
			t.Errorf("Expected 'AnnotationWidgetScopeSpecificWidgets' to NOT exist (should be renamed), got keys: %v", result)
		}
	})
}

func TestApplyInferredDiscriminatorFields(t *testing.T) {
	t.Run("single unique field", func(t *testing.T) {
		schemas := map[string]*OpenAPIV3SchemaRef{
			"LogRulesVariant": {
				OpenAPIV3Schema: &OpenAPIV3Schema{
					Properties: map[string]*OpenAPIV3SchemaRef{
						"name":     {},
						"priority": {},
						"logRules": {},
					},
					Required: []string{"name", "priority"},
				},
			},
			"SpanRulesVariant": {
				OpenAPIV3Schema: &OpenAPIV3Schema{
					Properties: map[string]*OpenAPIV3SchemaRef{
						"name":      {},
						"priority":  {},
						"spanRules": {},
					},
					Required: []string{"name", "priority"},
				},
			},
		}

		applyInferredDiscriminatorFields(schemas)

		if got := schemas["LogRulesVariant"].Required; !slices.Equal(got, []string{"name", "priority", "logRules"}) {
			t.Fatalf("LogRulesVariant required fields = %v, want %v", got, []string{"name", "priority", "logRules"})
		}
		if got := schemas["SpanRulesVariant"].Required; !slices.Equal(got, []string{"name", "priority", "spanRules"}) {
			t.Fatalf("SpanRulesVariant required fields = %v, want %v", got, []string{"name", "priority", "spanRules"})
		}
	})

	t.Run("combo-only discriminator", func(t *testing.T) {
		schemas := map[string]*OpenAPIV3SchemaRef{
			"AlphaXray":   {OpenAPIV3Schema: schemaWithProperties("common", "alpha", "xray")},
			"AlphaYankee": {OpenAPIV3Schema: schemaWithProperties("common", "alpha", "yankee")},
			"BetaXray":    {OpenAPIV3Schema: schemaWithProperties("common", "beta", "xray")},
			"BetaYankee":  {OpenAPIV3Schema: schemaWithProperties("common", "beta", "yankee")},
		}

		applyInferredDiscriminatorFields(schemas)

		assertRequiredFields(t, schemas["AlphaXray"].Required, []string{"alpha", "xray"})
		assertRequiredFields(t, schemas["AlphaYankee"].Required, []string{"alpha", "yankee"})
		assertRequiredFields(t, schemas["BetaXray"].Required, []string{"beta", "xray"})
		assertRequiredFields(t, schemas["BetaYankee"].Required, []string{"beta", "yankee"})
	})

	t.Run("impossible discriminator leaves schemas unchanged", func(t *testing.T) {
		schemas := map[string]*OpenAPIV3SchemaRef{
			"Left":  {OpenAPIV3Schema: schemaWithProperties("common", "value")},
			"Right": {OpenAPIV3Schema: schemaWithProperties("common", "value")},
		}

		applyInferredDiscriminatorFields(schemas)

		if len(schemas["Left"].Required) != 0 {
			t.Fatalf("Left required fields = %v, want unchanged empty slice", schemas["Left"].Required)
		}
		if len(schemas["Right"].Required) != 0 {
			t.Fatalf("Right required fields = %v, want unchanged empty slice", schemas["Right"].Required)
		}
	})
}

func schemaWithProperties(propertyNames ...string) *OpenAPIV3Schema {
	properties := make(map[string]*OpenAPIV3SchemaRef, len(propertyNames))
	for _, propertyName := range propertyNames {
		properties[propertyName] = &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{}}
	}
	return &OpenAPIV3Schema{Properties: properties}
}

func assertRequiredFields(t *testing.T, got []string, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("required fields = %v, want %v", got, want)
	}
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

func TestApplyPathParamRenames(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		renames  map[string]string
		expected string
	}{
		{
			name:     "No renames",
			path:     "/v1/shelves/{shelf_id}",
			renames:  map[string]string{},
			expected: "/v1/shelves/{shelf_id}",
		},
		{
			name:     "Single path param renamed",
			path:     "/v1/shelves/{shelf_id}",
			renames:  map[string]string{"{shelf_id}": "{shelfName}"},
			expected: "/v1/shelves/{shelfName}",
		},
		{
			name:     "Path param renamed for spectral equivalence",
			path:     "/integrations/contextual-data/v1/{integration_id}",
			renames:  map[string]string{"{integration_id}": "{id}"},
			expected: "/integrations/contextual-data/v1/{id}",
		},
		{
			name:     "Multiple path params renamed",
			path:     "/v1/publishers/{publisher_id}/books/{book_id}",
			renames:  map[string]string{"{publisher_id}": "{publisherId}", "{book_id}": "{bookId}"},
			expected: "/v1/publishers/{publisherId}/books/{bookId}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := applyPathParamRenames(tt.path, tt.renames)
			if actual != tt.expected {
				t.Errorf("applyPathParamRenames(%q, %v) = %q; want %q", tt.path, tt.renames, actual, tt.expected)
			}
		})
	}
}

func TestCheckDuplicatePath(t *testing.T) {
	t.Run("no duplicates - different paths", func(t *testing.T) {
		registeredPaths := make(map[pathMethodKey]pathMethodSource)

		// First registration
		err := checkDuplicatePath(registeredPaths, "/v1/users/{id}", "GET", "UserService", "GetUser", 0)
		if err != nil {
			t.Fatalf("expected no error for first path, got: %v", err)
		}

		// Second registration with different path
		err = checkDuplicatePath(registeredPaths, "/v1/users", "GET", "UserService", "ListUsers", 0)
		if err != nil {
			t.Fatalf("expected no error for different path, got: %v", err)
		}

		// Third registration with different path
		err = checkDuplicatePath(registeredPaths, "/v1/accounts/{id}", "GET", "AccountService", "GetAccount", 0)
		if err != nil {
			t.Fatalf("expected no error for different path, got: %v", err)
		}
	})

	t.Run("no duplicates - same path different methods", func(t *testing.T) {
		registeredPaths := make(map[pathMethodKey]pathMethodSource)

		// GET registration
		err := checkDuplicatePath(registeredPaths, "/v1/users/{id}", "GET", "UserService", "GetUser", 0)
		if err != nil {
			t.Fatalf("expected no error for GET, got: %v", err)
		}

		// PUT registration (same path, different method)
		err = checkDuplicatePath(registeredPaths, "/v1/users/{id}", "PUT", "UserService", "UpdateUser", 0)
		if err != nil {
			t.Fatalf("expected no error for PUT on same path, got: %v", err)
		}

		// DELETE registration (same path, different method)
		err = checkDuplicatePath(registeredPaths, "/v1/users/{id}", "DELETE", "UserService", "DeleteUser", 0)
		if err != nil {
			t.Fatalf("expected no error for DELETE on same path, got: %v", err)
		}

		// POST registration (same path, different method)
		err = checkDuplicatePath(registeredPaths, "/v1/users/{id}", "POST", "UserService", "CreateUser", 0)
		if err != nil {
			t.Fatalf("expected no error for POST on same path, got: %v", err)
		}

		// PATCH registration (same path, different method)
		err = checkDuplicatePath(registeredPaths, "/v1/users/{id}", "PATCH", "UserService", "PatchUser", 0)
		if err != nil {
			t.Fatalf("expected no error for PATCH on same path, got: %v", err)
		}
	})

	t.Run("duplicate path and method within same service", func(t *testing.T) {
		registeredPaths := make(map[pathMethodKey]pathMethodSource)

		// First registration
		err := checkDuplicatePath(registeredPaths, "/v1/users/{id}", "GET", "TestService", "GetUser", 0)
		if err != nil {
			t.Fatalf("expected no error for first registration, got: %v", err)
		}

		// Duplicate registration
		err = checkDuplicatePath(registeredPaths, "/v1/users/{id}", "GET", "TestService", "GetUserById", 0)
		if err == nil {
			t.Fatal("expected error for duplicate path and method, got nil")
		}

		// Verify error message contains useful information
		errMsg := err.Error()
		if !strings.Contains(errMsg, "duplicate HTTP path and method") {
			t.Errorf("error message should mention 'duplicate HTTP path and method', got: %s", errMsg)
		}
		if !strings.Contains(errMsg, "GET /v1/users/{id}") {
			t.Errorf("error message should contain path and method 'GET /v1/users/{id}', got: %s", errMsg)
		}
		if !strings.Contains(errMsg, "GetUser") {
			t.Errorf("error message should mention first method 'GetUser', got: %s", errMsg)
		}
		if !strings.Contains(errMsg, "GetUserById") {
			t.Errorf("error message should mention second method 'GetUserById', got: %s", errMsg)
		}
		if !strings.Contains(errMsg, "TestService") {
			t.Errorf("error message should mention service 'TestService', got: %s", errMsg)
		}
	})

	t.Run("duplicate path and method across different services", func(t *testing.T) {
		registeredPaths := make(map[pathMethodKey]pathMethodSource)

		// First service registration
		err := checkDuplicatePath(registeredPaths, "/v1/users/{id}", "GET", "UserService", "GetUser", 0)
		if err != nil {
			t.Fatalf("expected no error for first registration, got: %v", err)
		}

		// Second service registration (duplicate)
		err = checkDuplicatePath(registeredPaths, "/v1/users/{id}", "GET", "AdminService", "GetUserAdmin", 0)
		if err == nil {
			t.Fatal("expected error for duplicate path and method across services, got nil")
		}

		errMsg := err.Error()
		if !strings.Contains(errMsg, "UserService") {
			t.Errorf("error message should mention first service 'UserService', got: %s", errMsg)
		}
		if !strings.Contains(errMsg, "AdminService") {
			t.Errorf("error message should mention second service 'AdminService', got: %s", errMsg)
		}
	})

	t.Run("duplicate via additional bindings", func(t *testing.T) {
		registeredPaths := make(map[pathMethodKey]pathMethodSource)

		// Main binding
		err := checkDuplicatePath(registeredPaths, "/v1/users/{id}", "GET", "TestService", "GetUser", 0)
		if err != nil {
			t.Fatalf("expected no error for main binding, got: %v", err)
		}

		// Additional binding on same method
		err = checkDuplicatePath(registeredPaths, "/v1/accounts/{id}", "GET", "TestService", "GetUser", 1)
		if err != nil {
			t.Fatalf("expected no error for additional binding with different path, got: %v", err)
		}

		// Another method trying to use the same path as the additional binding
		err = checkDuplicatePath(registeredPaths, "/v1/accounts/{id}", "GET", "TestService", "GetAccount", 0)
		if err == nil {
			t.Fatal("expected error for duplicate path via additional binding, got nil")
		}

		errMsg := err.Error()
		if !strings.Contains(errMsg, "/v1/accounts/{id}") {
			t.Errorf("error message should contain the conflicting path '/v1/accounts/{id}', got: %s", errMsg)
		}
		if !strings.Contains(errMsg, "binding 1") {
			t.Errorf("error message should mention binding index 1 for the additional binding, got: %s", errMsg)
		}
		if !strings.Contains(errMsg, "binding 0") {
			t.Errorf("error message should mention binding index 0 for the conflicting method, got: %s", errMsg)
		}
	})

	t.Run("duplicate POST endpoints", func(t *testing.T) {
		registeredPaths := make(map[pathMethodKey]pathMethodSource)

		err := checkDuplicatePath(registeredPaths, "/v1/users", "POST", "TestService", "CreateUser", 0)
		if err != nil {
			t.Fatalf("expected no error for first POST, got: %v", err)
		}

		err = checkDuplicatePath(registeredPaths, "/v1/users", "POST", "TestService", "AddUser", 0)
		if err == nil {
			t.Fatal("expected error for duplicate POST path, got nil")
		}

		errMsg := err.Error()
		if !strings.Contains(errMsg, "POST /v1/users") {
			t.Errorf("error message should contain 'POST /v1/users', got: %s", errMsg)
		}
	})

	t.Run("all HTTP methods can be duplicated", func(t *testing.T) {
		methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD", "TRACE"}

		for _, method := range methods {
			t.Run(method, func(t *testing.T) {
				registeredPaths := make(map[pathMethodKey]pathMethodSource)

				err := checkDuplicatePath(registeredPaths, "/v1/test", method, "Service1", "Method1", 0)
				if err != nil {
					t.Fatalf("expected no error for first %s registration, got: %v", method, err)
				}

				err = checkDuplicatePath(registeredPaths, "/v1/test", method, "Service2", "Method2", 0)
				if err == nil {
					t.Fatalf("expected error for duplicate %s path, got nil", method)
				}

				if !strings.Contains(err.Error(), method+" /v1/test") {
					t.Errorf("error message should contain '%s /v1/test', got: %s", method, err.Error())
				}
			})
		}
	})
}

func TestSanitizeURLPath_DuplicateDetection(t *testing.T) {
	// Test that paths normalize correctly and would be detected as duplicates
	t.Run("dotted path params normalize to same path", func(t *testing.T) {
		path1 := sanitizeURLPath("/v1/users/{user.id}")
		path2 := sanitizeURLPath("/v1/users/{id}")

		if path1 != path2 {
			t.Errorf("expected paths to normalize to same value, got %q and %q", path1, path2)
		}
	})

	t.Run("deeply nested path params normalize", func(t *testing.T) {
		path1 := sanitizeURLPath("/v1/users/{user.profile.id}")
		path2 := sanitizeURLPath("/v1/users/{id}")

		if path1 != path2 {
			t.Errorf("expected paths to normalize to same value, got %q and %q", path1, path2)
		}
	})
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
