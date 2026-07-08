package genopenapi

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/internal/descriptor"
	options "github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv3/options"
	"google.golang.org/genproto/googleapis/api/visibility"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"google.golang.org/protobuf/types/pluginpb"
)

// Mock descriptor.Field type to simulate a protobuf field
type MockField struct {
	Name string
}

func (f *MockField) GetName() string {
	return f.Name
}

func testOneOfField(name string) *descriptor.Field {
	return &descriptor.Field{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{
		Name: proto.String(name),
	}}
}

func testOneOfMessageField(name string, typeName string) *descriptor.Field {
	return &descriptor.Field{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{
		Name:     proto.String(name),
		Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
		TypeName: proto.String(typeName),
	}}
}

func assertRequiredFields(t *testing.T, got []string, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("required fields = %v, want %v", got, want)
	}
}

func TestBuildOpenAPIV3SchemaFromMessage_IndependentOneOfGroups(t *testing.T) {
	msg := newIndependentOneOfDashboardFixture(t, []string{"name", "layout"})

	schema, componentSchemas := buildOpenAPIV3SchemaFromMessage(msg, nil, map[string]string{msg.FQMN(): "Dashboard"}, descriptor.NewRegistry())

	if len(componentSchemas) != 0 {
		t.Fatalf("independent oneof groups should not emit Cartesian component variants, got %d", len(componentSchemas))
	}
	if len(schema.OneOf) != 0 {
		t.Fatalf("dashboard should be one object schema, got %d top-level oneOf variants", len(schema.OneOf))
	}
	assertRequiredFields(t, schema.Required, []string{"name", "layout"})
	assertProperties(t, schema.Properties, []string{"name", "layout", "off", "one_minute", "absolute_time_frame", "relative_time_frame"})
	assertOneOfGroupConstraint(t, schema.AllOf[0], []string{"off", "one_minute"}, true)
	assertOneOfGroupConstraint(t, schema.AllOf[1], []string{"absolute_time_frame", "relative_time_frame"}, true)
}

func TestBuildOpenAPIV3SchemaFromMessage_RequiredIndependentOneOfGroups(t *testing.T) {
	msg := newIndependentOneOfDashboardFixture(t, []string{"name", "layout", "auto_refresh", "time_frame"})

	schema, componentSchemas := buildOpenAPIV3SchemaFromMessage(msg, nil, map[string]string{msg.FQMN(): "Dashboard"}, descriptor.NewRegistry())

	if len(componentSchemas) != 0 {
		t.Fatalf("independent oneof groups should not emit Cartesian component variants, got %d", len(componentSchemas))
	}
	assertRequiredFields(t, schema.Required, []string{"name", "layout"})
	assertOneOfGroupConstraint(t, schema.AllOf[0], []string{"off", "one_minute"}, false)
	assertOneOfGroupConstraint(t, schema.AllOf[1], []string{"absolute_time_frame", "relative_time_frame"}, false)
}

func TestBuildOpenAPIV3SchemaFromMessage_RequiredIndependentOneOfGroupsJSONNames(t *testing.T) {
	msg := newIndependentOneOfDashboardFixture(t, []string{"name", "layout", "autoRefresh", "timeFrame"})

	schema, componentSchemas := buildOpenAPIV3SchemaFromMessage(msg, nil, map[string]string{msg.FQMN(): "Dashboard"}, descriptor.NewRegistry())

	if len(componentSchemas) != 0 {
		t.Fatalf("independent oneof groups should not emit Cartesian component variants, got %d", len(componentSchemas))
	}
	assertRequiredFields(t, schema.Required, []string{"name", "layout"})
	assertOneOfGroupConstraint(t, schema.AllOf[0], []string{"off", "one_minute"}, false)
	assertOneOfGroupConstraint(t, schema.AllOf[1], []string{"absolute_time_frame", "relative_time_frame"}, false)
}

func TestBuildOpenAPIV3SchemaFromMessage_SingleIndependentOneOfGroupUsesTopLevelOneOf(t *testing.T) {
	msg := newSingleIndependentOneOfFixture(t, []string{"name"})

	schema, componentSchemas := buildOpenAPIV3SchemaFromMessage(msg, nil, map[string]string{msg.FQMN(): "Rule"}, descriptor.NewRegistry())

	if len(componentSchemas) != 0 {
		t.Fatalf("independent oneof groups should not emit Cartesian component variants, got %d", len(componentSchemas))
	}
	assertRequiredFields(t, schema.Required, []string{"name"})
	assertProperties(t, schema.Properties, []string{"name", "log_rules", "span_rules"})
	if len(schema.AllOf) != 0 {
		t.Fatalf("single independent oneof group should not be wrapped in allOf, got %d entries", len(schema.AllOf))
	}
	assertOneOfGroupConstraint(t, &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{OneOf: schema.OneOf}}, []string{"log_rules", "span_rules"}, true)
}

func TestSanitizeIndependentOneOfDiscriminatorDropsSyntheticMappingsButKeepsPropertyName(t *testing.T) {
	discriminator := &OpenAPIV3Discriminator{
		PropertyName: "kind",
		Mapping: map[string]string{
			"alpha": "#/components/schemas/ExampleAlpha",
			"beta":  "#/components/schemas/ExampleBeta",
		},
	}

	got := sanitizeIndependentOneOfDiscriminator(discriminator, "Example", map[string][]*descriptor.Field{
		"choice": {
			testOneOfField("alpha"),
			testOneOfField("beta"),
		},
	}, nil)

	if got == nil {
		t.Fatal("discriminator = nil, want propertyName preserved after dropping synthetic mappings")
	}
	if got.PropertyName != "kind" {
		t.Fatalf("propertyName = %q, want kind", got.PropertyName)
	}
	if len(got.Mapping) != 0 {
		t.Fatalf("mapping = %v, want empty mapping after dropping synthetic mappings", got.Mapping)
	}
}

func TestSanitizeIndependentOneOfDiscriminatorPreservesRealMappings(t *testing.T) {
	discriminator := &OpenAPIV3Discriminator{
		PropertyName: "kind",
		Mapping: map[string]string{
			"alpha": "#/components/schemas/ExampleAlpha",
			"other": "#/components/schemas/OtherSchema",
		},
	}

	got := sanitizeIndependentOneOfDiscriminator(discriminator, "Example", map[string][]*descriptor.Field{
		"choice": {
			testOneOfField("alpha"),
		},
	}, nil)

	if got == nil {
		t.Fatal("discriminator = nil, want remaining real mapping")
	}
	if len(got.Mapping) != 2 ||
		got.Mapping["alpha"] != "#/components/schemas/ExampleAlpha" ||
		got.Mapping["other"] != "#/components/schemas/OtherSchema" {
		t.Fatalf("mapping = %v, want alpha and other mappings to remain", got.Mapping)
	}
}

func TestSanitizeIndependentOneOfDiscriminatorDropsBareSyntheticMapping(t *testing.T) {
	discriminator := &OpenAPIV3Discriminator{
		PropertyName: "kind",
		Mapping: map[string]string{
			"alpha": "ExampleAlpha",
			"other": "OtherSchema",
		},
	}

	got := sanitizeIndependentOneOfDiscriminator(discriminator, "Example", map[string][]*descriptor.Field{
		"choice": {
			testOneOfField("alpha"),
			testOneOfField("beta"),
		},
	}, nil)

	if got == nil {
		t.Fatal("discriminator = nil, want remaining real mapping")
	}
	if len(got.Mapping) != 1 || got.Mapping["other"] != "OtherSchema" {
		t.Fatalf("mapping = %v, want only other -> OtherSchema", got.Mapping)
	}
}

func TestSanitizeIndependentOneOfDiscriminatorPreservesPrefixedRealMapping(t *testing.T) {
	discriminator := &OpenAPIV3Discriminator{
		PropertyName: "kind",
		Mapping: map[string]string{
			"alpha": "#/components/schemas/ExampleAlpha",
		},
	}

	got := sanitizeIndependentOneOfDiscriminator(discriminator, "Example", map[string][]*descriptor.Field{
		"choice": {
			testOneOfField("beta"),
		},
	}, nil)

	if got == nil {
		t.Fatal("discriminator = nil, want prefixed non-synthetic mapping to be preserved")
	}
	if len(got.Mapping) != 1 || got.Mapping["alpha"] != "#/components/schemas/ExampleAlpha" {
		t.Fatalf("mapping = %v, want alpha -> #/components/schemas/ExampleAlpha", got.Mapping)
	}
}

func TestSanitizeIndependentOneOfDiscriminatorDropsPayloadSchemaMappings(t *testing.T) {
	discriminator := &OpenAPIV3Discriminator{
		PropertyName: "access_type",
		Mapping: map[string]string{
			"permanent": "#/components/schemas/PermanentAccess",
			"temporary": "TemporaryAccess",
			"other":     "#/components/schemas/OtherSchema",
		},
	}

	got := sanitizeIndependentOneOfDiscriminator(discriminator, "AccessType", map[string][]*descriptor.Field{
		"access": {
			testOneOfMessageField("permanent_access", ".test.PermanentAccess"),
			testOneOfMessageField("temporary_access", ".test.TemporaryAccess"),
		},
	}, map[string]string{
		".test.PermanentAccess": "PermanentAccess",
		".test.TemporaryAccess": "TemporaryAccess",
	})

	if got == nil {
		t.Fatal("discriminator = nil, want propertyName preserved after dropping payload mappings")
	}
	if got.PropertyName != "access_type" {
		t.Fatalf("propertyName = %q, want access_type", got.PropertyName)
	}
	if len(got.Mapping) != 1 || got.Mapping["other"] != "#/components/schemas/OtherSchema" {
		t.Fatalf("mapping = %v, want only other -> #/components/schemas/OtherSchema", got.Mapping)
	}
}

func TestSanitizeIndependentOneOfDiscriminatorDropsCartesianSyntheticMapping(t *testing.T) {
	discriminator := &OpenAPIV3Discriminator{
		PropertyName: "kind",
		Mapping: map[string]string{
			"dashboard": "#/components/schemas/DashboardOffRelativeTimeFrame",
			"other":     "#/components/schemas/OtherSchema",
		},
	}

	got := sanitizeIndependentOneOfDiscriminator(discriminator, "Dashboard", map[string][]*descriptor.Field{
		"auto_refresh": {
			testOneOfField("off"),
			testOneOfField("five_minutes"),
		},
		"time_frame": {
			testOneOfField("absolute_time_frame"),
			testOneOfField("relative_time_frame"),
		},
	}, nil)

	if got == nil {
		t.Fatal("discriminator = nil, want remaining real mapping")
	}
	if len(got.Mapping) != 1 || got.Mapping["other"] != "#/components/schemas/OtherSchema" {
		t.Fatalf("mapping = %v, want only other -> #/components/schemas/OtherSchema", got.Mapping)
	}
}

func TestSanitizeIndependentOneOfDiscriminatorDropsDottedSyntheticMapping(t *testing.T) {
	discriminator := &OpenAPIV3Discriminator{
		PropertyName: "kind",
		Mapping: map[string]string{
			"alpha": "#/components/schemas/BCAlpha",
			"other": "#/components/schemas/OtherSchema",
		},
	}

	got := sanitizeIndependentOneOfDiscriminator(discriminator, "B.C", map[string][]*descriptor.Field{
		"choice": {
			testOneOfField("alpha"),
			testOneOfField("beta"),
		},
	}, nil)

	if got == nil {
		t.Fatal("discriminator = nil, want remaining real mapping")
	}
	if len(got.Mapping) != 1 || got.Mapping["other"] != "#/components/schemas/OtherSchema" {
		t.Fatalf("mapping = %v, want only other -> #/components/schemas/OtherSchema", got.Mapping)
	}
}

func TestSanitizeIndependentOneOfDiscriminatorDropsCollidedSyntheticMapping(t *testing.T) {
	discriminator := &OpenAPIV3Discriminator{
		PropertyName: "kind",
		Mapping: map[string]string{
			"real":      "#/components/schemas/ColorsByAggregation",
			"synthetic": "#/components/schemas/ColorsByAggregationVariant",
			"other":     "#/components/schemas/OtherSchema",
		},
	}

	got := sanitizeIndependentOneOfDiscriminator(discriminator, "Colors", map[string][]*descriptor.Field{
		"choice": {
			testOneOfField("by_aggregation"),
			testOneOfField("by_query"),
		},
	}, map[string]string{
		".test.ColorsByAggregation": "ColorsByAggregation",
	})

	if got == nil {
		t.Fatal("discriminator = nil, want real mappings to remain")
	}
	if len(got.Mapping) != 2 ||
		got.Mapping["real"] != "#/components/schemas/ColorsByAggregation" ||
		got.Mapping["other"] != "#/components/schemas/OtherSchema" {
		t.Fatalf("mapping = %v, want real and other mappings to remain", got.Mapping)
	}
}

func TestSanitizeIndependentOneOfDiscriminatorIgnoresOneArmGroupsInLegacyName(t *testing.T) {
	discriminator := &OpenAPIV3Discriminator{
		PropertyName: "kind",
		Mapping: map[string]string{
			"variant": "#/components/schemas/ThingVariantA",
			"other":   "#/components/schemas/OtherSchema",
		},
	}

	got := sanitizeIndependentOneOfDiscriminator(discriminator, "Thing", map[string][]*descriptor.Field{
		"created_at": {
			testOneOfField("created_at"),
		},
		"variant": {
			testOneOfField("variant_a"),
			testOneOfField("variant_b"),
		},
	}, nil)

	if got == nil {
		t.Fatal("discriminator = nil, want remaining real mapping")
	}
	if len(got.Mapping) != 1 || got.Mapping["other"] != "#/components/schemas/OtherSchema" {
		t.Fatalf("mapping = %v, want only other -> #/components/schemas/OtherSchema", got.Mapping)
	}
}

func TestBuildOpenAPIV3SchemaFromMessage_SanitizesDiscriminatorUsingOriginalOneOfGroups(t *testing.T) {
	var zero int32
	fieldType := descriptorpb.FieldDescriptorProto_TYPE_STRING
	label := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	hiddenFieldOptions := &descriptorpb.FieldOptions{}
	proto.SetExtension(hiddenFieldOptions, visibility.E_FieldVisibility, &visibility.VisibilityRule{Restriction: "INTERNAL"})

	msgOptions := &descriptorpb.MessageOptions{}
	proto.SetExtension(msgOptions, options.E_Openapiv3Schema, &options.Schema{
		Discriminator: &options.Discriminator{
			PropertyName: "kind",
			Mapping: map[string]string{
				"visible": "#/components/schemas/ThingVisibleArm",
			},
		},
	})

	msg := descriptorMessageFromProto(&descriptorpb.DescriptorProto{
		Name: proto.String("Thing"),
		OneofDecl: []*descriptorpb.OneofDescriptorProto{
			{Name: proto.String("variant")},
		},
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:       proto.String("visible_arm"),
				Number:     proto.Int32(1),
				Label:      &label,
				Type:       &fieldType,
				OneofIndex: &zero,
			},
			{
				Name:       proto.String("hidden_arm"),
				Number:     proto.Int32(2),
				Label:      &label,
				Type:       &fieldType,
				OneofIndex: &zero,
				Options:    hiddenFieldOptions,
			},
		},
		Options: msgOptions,
	})

	schema := buildOpenAPIV3SchemaFromMessageWithReferences(msg, descriptor.NewRegistry(), map[string]string{msg.FQMN(): "Thing"})

	if schema.Discriminator == nil {
		t.Fatal("discriminator = nil, want propertyName preserved after dropping old synthetic mapping")
	}
	if schema.Discriminator.PropertyName != "kind" {
		t.Fatalf("propertyName = %q, want kind", schema.Discriminator.PropertyName)
	}
	if len(schema.Discriminator.Mapping) != 0 {
		t.Fatalf("mapping = %v, want empty mapping after dropping old synthetic mapping", schema.Discriminator.Mapping)
	}
	assertProperties(t, schema.Properties, []string{"visible_arm"})
}

func TestBuildRequestBody_IndependentOneOfGroups(t *testing.T) {
	msg := newIndependentOneOfDashboardFixture(t, []string{"name", "layout"})
	method := &descriptor.Method{
		MethodDescriptorProto: &descriptorpb.MethodDescriptorProto{
			Name:       proto.String("CreateDashboard"),
			InputType:  proto.String(".test.Dashboard"),
			OutputType: proto.String(".test.Dashboard"),
		},
		RequestType:  msg,
		ResponseType: msg,
	}
	binding := &descriptor.Binding{
		HTTPMethod: "POST",
		Body:       &descriptor.Body{},
		Method:     method,
	}

	body, componentSchemas := buildRequestBody(binding, map[string]*OpenAPIV3SchemaRef{}, descriptor.NewRegistry(), map[string]string{msg.FQMN(): "Dashboard"})
	if body == nil || body.OpenAPIV3RequestBody == nil {
		t.Fatal("expected non-nil request body")
	}
	if len(componentSchemas) != 0 {
		t.Fatalf("independent request body should not emit Cartesian component variants, got %d", len(componentSchemas))
	}
	if !body.Required {
		t.Fatal("normal required fields should make requestBody.required=true")
	}

	bodySchema := body.OpenAPIV3RequestBody.Content["application/json"].Schema.OpenAPIV3Schema
	assertRequiredFields(t, bodySchema.Required, []string{"name", "layout"})
	assertOneOfGroupConstraint(t, bodySchema.AllOf[0], []string{"off", "one_minute"}, true)
	assertOneOfGroupConstraint(t, bodySchema.AllOf[1], []string{"absolute_time_frame", "relative_time_frame"}, true)
}

func TestBuildRequestBody_RequiredSetWhenBodyHasIndependentOneOfGroups(t *testing.T) {
	msg := newIndependentOneOfDashboardFixture(t, nil)
	method := &descriptor.Method{
		MethodDescriptorProto: &descriptorpb.MethodDescriptorProto{
			Name:       proto.String("UpdateDashboard"),
			InputType:  proto.String(".test.Dashboard"),
			OutputType: proto.String(".test.Dashboard"),
		},
		RequestType:  msg,
		ResponseType: msg,
	}
	binding := &descriptor.Binding{
		HTTPMethod: "POST",
		Body:       &descriptor.Body{},
		Method:     method,
	}

	body, _ := buildRequestBody(binding, map[string]*OpenAPIV3SchemaRef{}, descriptor.NewRegistry(), map[string]string{msg.FQMN(): "Dashboard"})
	if body == nil || body.OpenAPIV3RequestBody == nil {
		t.Fatal("expected non-nil request body")
	}
	if !body.Required {
		t.Fatal("body-bound independent oneof groups should preserve requestBody.required=true")
	}
}

func TestBuildRequestBody_RequiredSetWhenBodyHasSingleIndependentOneOfGroup(t *testing.T) {
	msg := newSingleIndependentOneOfFixture(t, nil)
	method := &descriptor.Method{
		MethodDescriptorProto: &descriptorpb.MethodDescriptorProto{
			Name:       proto.String("UpdateRule"),
			InputType:  proto.String(".test.Rule"),
			OutputType: proto.String(".test.Rule"),
		},
		RequestType:  msg,
		ResponseType: msg,
	}
	binding := &descriptor.Binding{
		HTTPMethod: "POST",
		Body:       &descriptor.Body{},
		Method:     method,
	}

	body, _ := buildRequestBody(binding, map[string]*OpenAPIV3SchemaRef{}, descriptor.NewRegistry(), map[string]string{msg.FQMN(): "Rule"})
	if body == nil || body.OpenAPIV3RequestBody == nil {
		t.Fatal("expected non-nil request body")
	}
	if !body.Required {
		t.Fatal("body-bound single independent oneof group should preserve requestBody.required=true")
	}

	bodySchema := body.OpenAPIV3RequestBody.Content["application/json"].Schema.OpenAPIV3Schema
	if len(bodySchema.OneOf) == 0 {
		t.Fatal("single independent oneof group should be emitted as top-level oneOf")
	}
	if len(bodySchema.AllOf) != 0 {
		t.Fatalf("single independent oneof group should not be wrapped in allOf, got %d entries", len(bodySchema.AllOf))
	}
}

func TestBuildRequestBody_PathSelectedIndependentOneOfGroup(t *testing.T) {
	msg := newSingleIndependentOneOfFixture(t, []string{"name"})
	method := &descriptor.Method{
		MethodDescriptorProto: &descriptorpb.MethodDescriptorProto{
			Name:       proto.String("UpdateRuleByLogRules"),
			InputType:  proto.String(".test.Rule"),
			OutputType: proto.String(".test.Rule"),
		},
		RequestType:  msg,
		ResponseType: msg,
	}

	logRulesField := msg.Fields[1]
	binding := &descriptor.Binding{
		HTTPMethod: "PATCH",
		Body:       &descriptor.Body{},
		Method:     method,
		PathParams: []descriptor.Parameter{{
			FieldPath: descriptor.FieldPath{{Name: "log_rules", Target: logRulesField}},
			Target:    logRulesField,
			Method:    method,
		}},
	}

	body, componentSchemas := buildRequestBody(binding, map[string]*OpenAPIV3SchemaRef{}, descriptor.NewRegistry(), map[string]string{msg.FQMN(): "Rule"})
	if body == nil || body.OpenAPIV3RequestBody == nil {
		t.Fatal("expected non-nil request body")
	}
	if len(componentSchemas) != 0 {
		t.Fatalf("path-selected oneof body should not emit component variants, got %d", len(componentSchemas))
	}

	bodySchema := body.OpenAPIV3RequestBody.Content["application/json"].Schema.OpenAPIV3Schema
	if _, ok := bodySchema.Properties["log_rules"]; ok {
		t.Fatal("path-selected oneof field should not remain as a body property")
	}
	assertProperties(t, bodySchema.Properties, []string{"name", "span_rules"})
	assertRequiredFields(t, bodySchema.Required, []string{"name"})
	assertForbiddenFields(t, bodySchema, []string{"span_rules"})
}

func newIndependentOneOfDashboardFixture(t *testing.T, required []string) *descriptor.Message {
	t.Helper()

	fieldType := descriptorpb.FieldDescriptorProto_TYPE_STRING
	label := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	oneOfIndex := func(i int32) *int32 { return &i }
	field := func(name string, number int32, oneofIndex *int32) *descriptor.Field {
		return &descriptor.Field{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{
			Name:       proto.String(name),
			Number:     proto.Int32(number),
			Label:      &label,
			Type:       &fieldType,
			OneofIndex: oneofIndex,
		}}
	}

	msgOptions := &descriptorpb.MessageOptions{}
	proto.SetExtension(msgOptions, options.E_Openapiv3Schema, &options.Schema{
		JsonSchema: &options.JSONSchema{Required: required},
	})

	msgDesc := &descriptorpb.DescriptorProto{
		Name: proto.String("Dashboard"),
		OneofDecl: []*descriptorpb.OneofDescriptorProto{
			{Name: proto.String("auto_refresh")},
			{Name: proto.String("time_frame")},
		},
		Field: []*descriptorpb.FieldDescriptorProto{
			field("name", 1, nil).FieldDescriptorProto,
			field("layout", 2, nil).FieldDescriptorProto,
			field("off", 3, oneOfIndex(0)).FieldDescriptorProto,
			field("one_minute", 4, oneOfIndex(0)).FieldDescriptorProto,
			field("relative_time_frame", 5, oneOfIndex(1)).FieldDescriptorProto,
			field("absolute_time_frame", 6, oneOfIndex(1)).FieldDescriptorProto,
		},
		Options: msgOptions,
	}

	return descriptorMessageFromProto(msgDesc)
}

func newSingleIndependentOneOfFixture(t *testing.T, required []string) *descriptor.Message {
	t.Helper()

	var zero int32
	fieldType := descriptorpb.FieldDescriptorProto_TYPE_STRING
	label := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	field := func(name string, number int32, oneofIndex *int32) *descriptor.Field {
		return &descriptor.Field{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{
			Name:       proto.String(name),
			Number:     proto.Int32(number),
			Label:      &label,
			Type:       &fieldType,
			OneofIndex: oneofIndex,
		}}
	}

	msgOptions := &descriptorpb.MessageOptions{}
	proto.SetExtension(msgOptions, options.E_Openapiv3Schema, &options.Schema{
		JsonSchema: &options.JSONSchema{Required: required},
	})

	msgDesc := &descriptorpb.DescriptorProto{
		Name: proto.String("Rule"),
		OneofDecl: []*descriptorpb.OneofDescriptorProto{
			{Name: proto.String("rules")},
		},
		Field: []*descriptorpb.FieldDescriptorProto{
			field("name", 1, nil).FieldDescriptorProto,
			field("log_rules", 2, &zero).FieldDescriptorProto,
			field("span_rules", 3, &zero).FieldDescriptorProto,
		},
		Options: msgOptions,
	}

	return descriptorMessageFromProto(msgDesc)
}

func descriptorMessageFromProto(msgDesc *descriptorpb.DescriptorProto) *descriptor.Message {
	file := &descriptor.File{FileDescriptorProto: &descriptorpb.FileDescriptorProto{
		Package: proto.String("test"),
	}}
	msg := &descriptor.Message{DescriptorProto: msgDesc, File: file}
	for _, fieldDescriptor := range msgDesc.Field {
		msg.Fields = append(msg.Fields, &descriptor.Field{FieldDescriptorProto: fieldDescriptor, Message: msg})
	}
	return msg
}

func assertProperties(t *testing.T, properties map[string]*OpenAPIV3SchemaRef, want []string) {
	t.Helper()
	got := make([]string, 0, len(properties))
	for propertyName := range properties {
		got = append(got, propertyName)
	}
	sort.Strings(got)
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Fatalf("properties = %v, want %v", got, want)
	}
}

func assertOneOfGroupConstraint(t *testing.T, schemaRef *OpenAPIV3SchemaRef, wantRequiredBranches []string, wantUnset bool) {
	t.Helper()
	if schemaRef == nil || schemaRef.OpenAPIV3Schema == nil {
		t.Fatal("expected oneof group schema")
	}
	gotRequiredBranches := []string{}
	gotUnset := false
	for _, branchRef := range schemaRef.OpenAPIV3Schema.OneOf {
		if branchRef == nil || branchRef.OpenAPIV3Schema == nil {
			continue
		}
		if branchRef.OpenAPIV3Schema.Type != "object" {
			t.Fatalf("oneof branch type = %q, want object", branchRef.OpenAPIV3Schema.Type)
		}
		if len(branchRef.OpenAPIV3Schema.Required) > 0 {
			gotRequiredBranches = append(gotRequiredBranches, branchRef.OpenAPIV3Schema.Required...)
		}
		if branchRef.OpenAPIV3Schema.Not != nil {
			gotUnset = true
			notSchema := branchRef.OpenAPIV3Schema.Not.OpenAPIV3Schema
			if notSchema == nil {
				t.Fatal("oneof unset branch not schema is nil")
			}
			for _, absentRef := range notSchema.AnyOf {
				if absentRef == nil || absentRef.OpenAPIV3Schema == nil {
					continue
				}
				if absentRef.OpenAPIV3Schema.Type != "object" {
					t.Fatalf("oneof unset absent-field branch type = %q, want object", absentRef.OpenAPIV3Schema.Type)
				}
			}
		}
	}
	sort.Strings(gotRequiredBranches)
	sort.Strings(wantRequiredBranches)
	if !slices.Equal(gotRequiredBranches, wantRequiredBranches) {
		t.Fatalf("oneof required branches = %v, want %v", gotRequiredBranches, wantRequiredBranches)
	}
	if gotUnset != wantUnset {
		t.Fatalf("oneof unset branch = %v, want %v", gotUnset, wantUnset)
	}
}

func assertForbiddenFields(t *testing.T, schema *OpenAPIV3Schema, want []string) {
	t.Helper()
	got := make([]string, 0, len(schema.AllOf))
	for _, schemaRef := range schema.AllOf {
		if schemaRef == nil || schemaRef.OpenAPIV3Schema == nil || schemaRef.OpenAPIV3Schema.Not == nil || schemaRef.OpenAPIV3Schema.Not.OpenAPIV3Schema == nil {
			continue
		}
		got = append(got, schemaRef.OpenAPIV3Schema.Not.OpenAPIV3Schema.Required...)
	}
	sort.Strings(got)
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Fatalf("forbidden fields = %v, want %v", got, want)
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

func makeRepeatedFieldWithExtension(name string, fieldType descriptorpb.FieldDescriptorProto_Type, ext *options.JSONSchema) *descriptor.Field {
	opts := &descriptorpb.FieldOptions{}
	proto.SetExtension(opts, options.E_Openapiv3Field, ext)
	label := descriptorpb.FieldDescriptorProto_LABEL_REPEATED
	return &descriptor.Field{
		FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{
			Name:    proto.String(name),
			Type:    &fieldType,
			Label:   &label,
			Options: opts,
		},
	}
}

func TestRepeatedField_DescriptionOnArraySchema(t *testing.T) {
	field := makeRepeatedFieldWithExtension("items", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{
		Description: "the list of items",
	})
	reg := descriptor.NewRegistry()
	schema := buildPropertySchemaWithReferencesFromField(field, reg, map[string]string{})
	if schema == nil {
		t.Fatal("expected non-nil schema")
	}
	if schema.Type != "array" {
		t.Fatalf("expected array schema, got %q", schema.Type)
	}
	if schema.Description != "the list of items" {
		t.Errorf("expected description %q on array schema, got %q", "the list of items", schema.Description)
	}
}

func TestRepeatedField_MinMaxItemsOnArraySchema(t *testing.T) {
	field := makeRepeatedFieldWithExtension("tags", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{
		MinItems: 1,
		MaxItems: 10,
	})
	reg := descriptor.NewRegistry()
	schema := buildPropertySchemaWithReferencesFromField(field, reg, map[string]string{})
	if schema == nil {
		t.Fatal("expected non-nil schema")
	}
	wantMinItems(t, schema.MinItems, 1)
	if schema.MaxItems != 10 {
		t.Errorf("expected maxItems=10, got %d", schema.MaxItems)
	}
}

func TestFilterRequired(t *testing.T) {
	prop := func(names ...string) map[string]*OpenAPIV3SchemaRef {
		m := make(map[string]*OpenAPIV3SchemaRef, len(names))
		for _, n := range names {
			m[n] = &OpenAPIV3SchemaRef{}
		}
		return m
	}
	tests := []struct {
		name           string
		required       []string
		bodyProperties map[string]*OpenAPIV3SchemaRef
		want           []string
	}{
		{
			name:           "removes path param from required",
			required:       []string{"id", "name", "status"},
			bodyProperties: prop("name", "status"),
			want:           []string{"name", "status"},
		},
		{
			name:           "removes multiple path params",
			required:       []string{"org_id", "resource_id", "name"},
			bodyProperties: prop("name"),
			want:           []string{"name"},
		},
		{
			name:           "no path params leaves required unchanged",
			required:       []string{"name", "status"},
			bodyProperties: prop("name", "status"),
			want:           []string{"name", "status"},
		},
		{
			name:           "all fields are path params yields empty required",
			required:       []string{"id"},
			bodyProperties: prop(),
			want:           []string{},
		},
		{
			// Nested path param {resource.id}: leaf name "id" matches an unrelated
			// top-level body field "id". That top-level "id" must stay in required
			// because it is present in bodyProperties; only "resource" is absent.
			name:           "nested path param does not remove unrelated top-level field with same leaf name",
			required:       []string{"id", "resource"},
			bodyProperties: prop("id"),
			want:           []string{"id"},
		},
		{
			name:           "json camelCase required name matches proto property name",
			required:       []string{"fooBar"},
			bodyProperties: prop("foo_bar"),
			want:           []string{"fooBar"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := filterRequired(tc.required, tc.bodyProperties)
			if !slices.Equal(got, tc.want) {
				t.Errorf("filterRequired() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBuildSchemaFromFields_EmptyMessage_AdditionalPropertiesFalse(t *testing.T) {
	schema := buildSchemaFromFieldsWithReferences(
		nil,
		descriptor.NewRegistry(),
		nil,
		"",
		"",
		nil,
		nil,
		map[string]string{},
	)
	if schema == nil {
		t.Fatal("expected non-nil schema")
	}
	if schema.AdditionalProperties != false {
		t.Errorf("expected additionalProperties=false for empty message, got %v", schema.AdditionalProperties)
	}
}

// serviceWithTag builds an in-memory descriptor.Service carrying an openapiv3 Tag
// extension with the given tag name.
func serviceWithTag(tagName string) *descriptor.Service {
	opts := &descriptorpb.ServiceOptions{}
	proto.SetExtension(opts, options.E_Openapiv3Tag, &options.Tag{Name: tagName})
	return &descriptor.Service{
		ServiceDescriptorProto: &descriptorpb.ServiceDescriptorProto{
			Name:    proto.String(tagName + "Service"),
			Options: opts,
		},
	}
}

func TestBuildTagsStableOrder(t *testing.T) {
	// Tag names supplied in non-sorted order. buildTags collects them into a map,
	// so without an explicit sort the result order is randomized by Go.
	services := []*descriptor.Service{
		serviceWithTag("Zebra"),
		serviceWithTag("Alpha"),
		serviceWithTag("Mango"),
		serviceWithTag("Beta"),
	}
	p := param{File: &descriptor.File{Services: services}}

	want := []string{"Alpha", "Beta", "Mango", "Zebra"}

	// Run many times; every run must return the tags sorted by name.
	for i := 0; i < 100; i++ {
		tags, err := buildTags(p)
		if err != nil {
			t.Fatalf("iteration %d: buildTags returned error: %v", i, err)
		}
		got := make([]string, 0, len(tags))
		for _, tag := range tags {
			got = append(got, tag.Name)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("iteration %d: tags not sorted by name:\n  want %v\n   got %v", i, want, got)
		}
	}
}

// deterministicSpecRequest is a CodeGeneratorRequest exercising the parts of the
// generated spec that are assembled from Go maps (and were therefore at risk of
// nondeterministic ordering): multiple services -> multiple tags, multiple RPCs
// with HTTP bindings -> multiple paths, and several messages (incl. an enum,
// nested and repeated fields) -> multiple component schemas. Two files are merged
// so the cross-file merge path is covered too.
const deterministicSpecRequest = `
file_to_generate: "petstore/v1/pets.proto"
file_to_generate: "store/v1/store.proto"
proto_file: {
  name: "petstore/v1/pets.proto"
  package: "petstore.v1"
  message_type: {
    name: "Pet"
    field: { name: "id" number: 1 label: LABEL_OPTIONAL type: TYPE_STRING json_name: "id" }
    field: { name: "name" number: 2 label: LABEL_OPTIONAL type: TYPE_STRING json_name: "name" }
    field: { name: "status" number: 3 label: LABEL_OPTIONAL type: TYPE_ENUM type_name: ".petstore.v1.PetStatus" json_name: "status" }
    field: { name: "category" number: 4 label: LABEL_OPTIONAL type: TYPE_MESSAGE type_name: ".petstore.v1.Category" json_name: "category" }
  }
  message_type: {
    name: "Category"
    field: { name: "id" number: 1 label: LABEL_OPTIONAL type: TYPE_STRING json_name: "id" }
    field: { name: "name" number: 2 label: LABEL_OPTIONAL type: TYPE_STRING json_name: "name" }
  }
  message_type: {
    name: "ListPetsRequest"
    field: { name: "page_size" number: 1 label: LABEL_OPTIONAL type: TYPE_INT32 json_name: "pageSize" }
    field: { name: "page_token" number: 2 label: LABEL_OPTIONAL type: TYPE_STRING json_name: "pageToken" }
  }
  message_type: {
    name: "ListPetsResponse"
    field: { name: "pets" number: 1 label: LABEL_REPEATED type: TYPE_MESSAGE type_name: ".petstore.v1.Pet" json_name: "pets" }
  }
  message_type: {
    name: "GetPetRequest"
    field: { name: "id" number: 1 label: LABEL_OPTIONAL type: TYPE_STRING json_name: "id" }
  }
  message_type: {
    name: "CreatePetRequest"
    field: { name: "pet" number: 1 label: LABEL_OPTIONAL type: TYPE_MESSAGE type_name: ".petstore.v1.Pet" json_name: "pet" }
  }
  message_type: {
    name: "DeletePetRequest"
    field: { name: "id" number: 1 label: LABEL_OPTIONAL type: TYPE_STRING json_name: "id" }
  }
  enum_type: {
    name: "PetStatus"
    value: { name: "PET_STATUS_UNSPECIFIED" number: 0 }
    value: { name: "AVAILABLE" number: 1 }
    value: { name: "SOLD" number: 2 }
  }
  service: {
    name: "PetService"
    method: { name: "ListPets" input_type: ".petstore.v1.ListPetsRequest" output_type: ".petstore.v1.ListPetsResponse" options: { [google.api.http]: { get: "/v1/pets" } } }
    method: { name: "GetPet" input_type: ".petstore.v1.GetPetRequest" output_type: ".petstore.v1.Pet" options: { [google.api.http]: { get: "/v1/pets/{id}" } } }
    method: { name: "CreatePet" input_type: ".petstore.v1.CreatePetRequest" output_type: ".petstore.v1.Pet" options: { [google.api.http]: { post: "/v1/pets" body: "pet" } } }
    method: { name: "DeletePet" input_type: ".petstore.v1.DeletePetRequest" output_type: ".petstore.v1.Pet" options: { [google.api.http]: { delete: "/v1/pets/{id}" } } }
    options: { [grpc.gateway.protoc_gen_openapiv3.options.openapiv3_tag]: { name: "Pets" } }
  }
  options: { go_package: "example.com/petstore/v1;petstorev1" }
  syntax: "proto3"
}
proto_file: {
  name: "store/v1/store.proto"
  package: "store.v1"
  message_type: {
    name: "Order"
    field: { name: "id" number: 1 label: LABEL_OPTIONAL type: TYPE_STRING json_name: "id" }
    field: { name: "pet_id" number: 2 label: LABEL_OPTIONAL type: TYPE_STRING json_name: "petId" }
    field: { name: "quantity" number: 3 label: LABEL_OPTIONAL type: TYPE_INT32 json_name: "quantity" }
  }
  message_type: {
    name: "GetOrderRequest"
    field: { name: "id" number: 1 label: LABEL_OPTIONAL type: TYPE_STRING json_name: "id" }
  }
  message_type: {
    name: "ListOrdersResponse"
    field: { name: "orders" number: 1 label: LABEL_REPEATED type: TYPE_MESSAGE type_name: ".store.v1.Order" json_name: "orders" }
  }
  service: {
    name: "StoreService"
    method: { name: "GetOrder" input_type: ".store.v1.GetOrderRequest" output_type: ".store.v1.Order" options: { [google.api.http]: { get: "/v1/store/orders/{id}" } } }
    method: { name: "ListOrders" input_type: ".store.v1.GetOrderRequest" output_type: ".store.v1.ListOrdersResponse" options: { [google.api.http]: { get: "/v1/store/orders" } } }
    options: { [grpc.gateway.protoc_gen_openapiv3.options.openapiv3_tag]: { name: "Store" } }
  }
  options: { go_package: "example.com/store/v1;storev1" }
  syntax: "proto3"
}
`

// generateMergedSpec builds a fresh registry from req and runs the full generator
// pipeline (merge + path sort + JSON encode), returning the merged spec bytes.
// A fresh registry is used per call so the whole pipeline is re-exercised.
func generateMergedSpec(t *testing.T, reqText string) string {
	t.Helper()
	var req pluginpb.CodeGeneratorRequest
	if err := prototext.Unmarshal([]byte(reqText), &req); err != nil {
		t.Fatalf("prototext.Unmarshal: %v", err)
	}
	reg := descriptor.NewRegistry()
	reg.SetAllowMerge(true)
	reg.SetMergeFileName("apidocs")
	// AddErrorDefs registers google.rpc.Status (used for error responses) and must
	// run before Load, mirroring the plugin's main().
	if err := AddErrorDefs(reg); err != nil {
		t.Fatalf("AddErrorDefs: %v", err)
	}
	if err := reg.Load(&req); err != nil {
		t.Fatalf("reg.Load: %v", err)
	}
	var targets []*descriptor.File
	for _, name := range req.GetFileToGenerate() {
		f, err := reg.LookupFile(name)
		if err != nil {
			t.Fatalf("reg.LookupFile(%q): %v", name, err)
		}
		targets = append(targets, f)
	}
	g := New(reg, FormatJSON)
	resp, err := g.Generate(targets)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("expected 1 merged spec file, got %d", len(resp))
	}
	return resp[0].GetContent()
}

// TestGeneratedSpecIsDeterministic asserts the same protos produce the exact same
// OpenAPI spec on every generation — byte-for-byte, across the whole document
// (paths, schemas, tags, everything), not just the tags array.
func TestGeneratedSpecIsDeterministic(t *testing.T) {
	want := generateMergedSpec(t, deterministicSpecRequest)

	// Guard against a degenerate fixture: the assertion is only meaningful if the
	// spec actually contains the map-ordered sections we care about.
	for _, probe := range []string{`"paths"`, `"tags"`, "/v1/pets", "/v1/store/orders", `"Pets"`, `"Store"`, "PetStatus"} {
		if !strings.Contains(want, probe) {
			t.Fatalf("fixture too thin: generated spec does not contain %q", probe)
		}
	}

	for i := 0; i < 50; i++ {
		got := generateMergedSpec(t, deterministicSpecRequest)
		if got != want {
			t.Fatalf("iteration %d: generated spec is not deterministic.\n%s", i, firstLineDiff(want, got))
		}
	}
}

// firstLineDiff returns a short, human-readable description of the first line at
// which a and b differ, to make determinism failures easy to diagnose.
func firstLineDiff(a, b string) string {
	la := strings.Split(a, "\n")
	lb := strings.Split(b, "\n")
	n := len(la)
	if len(lb) < n {
		n = len(lb)
	}
	for i := 0; i < n; i++ {
		if la[i] != lb[i] {
			return fmt.Sprintf("first diff at line %d:\n  first: %s\n  later: %s", i+1, la[i], lb[i])
		}
	}
	if len(la) != len(lb) {
		return fmt.Sprintf("specs differ in length: first has %d lines, later has %d lines", len(la), len(lb))
	}
	return "specs differ but no line-level difference found"
}

// makeRepeatedField builds a repeated scalar field without any extension.
func makeRepeatedField(name string, fieldType descriptorpb.FieldDescriptorProto_Type) *descriptor.Field {
	label := descriptorpb.FieldDescriptorProto_LABEL_REPEATED
	return &descriptor.Field{
		FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{
			Name:  proto.String(name),
			Type:  &fieldType,
			Label: &label,
		},
	}
}

// makeSingularFieldWithExtension builds a non-repeated scalar field with a JSONSchema extension.
func makeSingularFieldWithExtension(name string, fieldType descriptorpb.FieldDescriptorProto_Type, ext *options.JSONSchema) *descriptor.Field {
	opts := &descriptorpb.FieldOptions{}
	proto.SetExtension(opts, options.E_Openapiv3Field, ext)
	label := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	return &descriptor.Field{
		FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{
			Name:    proto.String(name),
			Type:    &fieldType,
			Label:   &label,
			Options: opts,
		},
	}
}

// newRequestBodyFixture builds an in-memory descriptor.Binding for buildRequestBody tests.
// All fields are TYPE_STRING scalars. `required` is set on the message via
// openapiv3_schema.json_schema.required. `pathParamNames` become path parameters on
// the binding (they are valid field names from `fieldNames`).
func newRequestBodyFixture(t *testing.T, fieldNames []string, required []string, pathParamNames []string) *descriptor.Binding {
	t.Helper()

	fieldDescriptors := make([]*descriptorpb.FieldDescriptorProto, 0, len(fieldNames))
	for i, name := range fieldNames {
		t := descriptorpb.FieldDescriptorProto_TYPE_STRING
		l := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
		n := int32(i + 1)
		fieldDescriptors = append(fieldDescriptors, &descriptorpb.FieldDescriptorProto{
			Name:   proto.String(name),
			Type:   &t,
			Label:  &l,
			Number: &n,
		})
	}

	msgOptions := &descriptorpb.MessageOptions{}
	if len(required) > 0 {
		proto.SetExtension(msgOptions, options.E_Openapiv3Schema, &options.Schema{
			JsonSchema: &options.JSONSchema{Required: required},
		})
	}

	msgDesc := &descriptorpb.DescriptorProto{
		Name:    proto.String("ReqMsg"),
		Field:   fieldDescriptors,
		Options: msgOptions,
	}

	msg := &descriptor.Message{DescriptorProto: msgDesc}
	fields := make([]*descriptor.Field, 0, len(fieldDescriptors))
	for _, fd := range fieldDescriptors {
		fields = append(fields, &descriptor.Field{
			FieldDescriptorProto: fd,
			Message:              msg,
		})
	}
	msg.Fields = fields

	method := &descriptor.Method{
		MethodDescriptorProto: &descriptorpb.MethodDescriptorProto{
			Name:       proto.String("DoThing"),
			InputType:  proto.String(".example.ReqMsg"),
			OutputType: proto.String(".example.ReqMsg"),
		},
		RequestType:  msg,
		ResponseType: msg,
	}

	binding := &descriptor.Binding{
		HTTPMethod: "POST",
		Body:       &descriptor.Body{},
		Method:     method,
	}

	fieldByName := func(name string) *descriptor.Field {
		for _, f := range fields {
			if *f.Name == name {
				return f
			}
		}
		t.Fatalf("path param %q not in fields", name)
		return nil
	}
	for _, name := range pathParamNames {
		target := fieldByName(name)
		binding.PathParams = append(binding.PathParams, descriptor.Parameter{
			FieldPath: descriptor.FieldPath{{Name: name, Target: target}},
			Target:    target,
			Method:    method,
		})
	}

	return binding
}

// TestBuildRequestBody_RequiredSetWhenBodyHasRequiredProperties verifies that
// when the request body schema has required properties, requestBody.required
// is set to true. This is the fix for ibm-no-required-properties-in-optional-body.
func TestBuildRequestBody_RequiredSetWhenBodyHasRequiredProperties(t *testing.T) {
	binding := newRequestBodyFixture(t, []string{"name", "kind"}, []string{"name"}, nil)
	body, _ := buildRequestBody(binding, map[string]*OpenAPIV3SchemaRef{}, descriptor.NewRegistry(), map[string]string{})
	if body == nil || body.OpenAPIV3RequestBody == nil {
		t.Fatal("expected non-nil request body")
	}
	if !body.Required {
		t.Errorf("expected requestBody.required=true when body has required properties, got false")
	}
}

// TestBuildRequestBody_RequiredFalseWhenNoRequiredProperties verifies the
// negative case: no required fields → requestBody.required stays false (and
// hence is omitted from JSON output).
func TestBuildRequestBody_RequiredFalseWhenNoRequiredProperties(t *testing.T) {
	binding := newRequestBodyFixture(t, []string{"name", "kind"}, nil, nil)
	body, _ := buildRequestBody(binding, map[string]*OpenAPIV3SchemaRef{}, descriptor.NewRegistry(), map[string]string{})
	if body == nil || body.OpenAPIV3RequestBody == nil {
		t.Fatal("expected non-nil request body")
	}
	if body.Required {
		t.Errorf("expected requestBody.required=false when body has no required properties, got true")
	}
}

// TestBuildRequestBody_RequiredFalseWhenOnlyPathParamRequired verifies the
// regression case from PR#18: if the only required field is also a path
// parameter, it is filtered out of the schema's required list, so
// requestBody.required must NOT be set.
func TestBuildRequestBody_RequiredFalseWhenOnlyPathParamRequired(t *testing.T) {
	binding := newRequestBodyFixture(t, []string{"id", "name"}, []string{"id"}, []string{"id"})
	body, _ := buildRequestBody(binding, map[string]*OpenAPIV3SchemaRef{}, descriptor.NewRegistry(), map[string]string{})
	if body == nil || body.OpenAPIV3RequestBody == nil {
		t.Fatal("expected non-nil request body")
	}
	if body.Required {
		t.Errorf("expected requestBody.required=false when only required field is a path param, got true")
	}
}

func TestFieldDescription(t *testing.T) {
	t.Run("returns empty when field is nil", func(t *testing.T) {
		if got := fieldDescription(nil); got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})
	t.Run("returns empty when no options", func(t *testing.T) {
		field := &descriptor.Field{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{
			Name: proto.String("foo"),
		}}
		if got := fieldDescription(field); got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})
	t.Run("returns empty when extension absent", func(t *testing.T) {
		field := &descriptor.Field{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{
			Name:    proto.String("foo"),
			Options: &descriptorpb.FieldOptions{},
		}}
		if got := fieldDescription(field); got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})
	t.Run("returns extension description", func(t *testing.T) {
		opts := &descriptorpb.FieldOptions{}
		proto.SetExtension(opts, options.E_Openapiv3Field, &options.JSONSchema{
			Description: "the user id",
		})
		field := &descriptor.Field{FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{
			Name:    proto.String("id"),
			Options: opts,
		}}
		if got := fieldDescription(field); got != "the user id" {
			t.Errorf("expected %q, got %q", "the user id", got)
		}
	})
}

// newParamFixture builds a binding suitable for buildPathParameters tests.
// `descriptions` maps field names to openapiv3_field.description values; absent
// names get no extension.
func newParamFixture(t *testing.T, fieldNames []string, pathParamNames []string, descriptions map[string]string) *descriptor.Binding {
	t.Helper()

	fieldDescriptors := make([]*descriptorpb.FieldDescriptorProto, 0, len(fieldNames))
	for i, name := range fieldNames {
		typ := descriptorpb.FieldDescriptorProto_TYPE_STRING
		lbl := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
		num := int32(i + 1)
		var fopts *descriptorpb.FieldOptions
		if desc, ok := descriptions[name]; ok && desc != "" {
			fopts = &descriptorpb.FieldOptions{}
			proto.SetExtension(fopts, options.E_Openapiv3Field, &options.JSONSchema{
				Description: desc,
			})
		}
		fieldDescriptors = append(fieldDescriptors, &descriptorpb.FieldDescriptorProto{
			Name:    proto.String(name),
			Type:    &typ,
			Label:   &lbl,
			Number:  &num,
			Options: fopts,
		})
	}

	msgDesc := &descriptorpb.DescriptorProto{
		Name:  proto.String("ReqMsg"),
		Field: fieldDescriptors,
	}
	file := &descriptor.File{
		FileDescriptorProto: &descriptorpb.FileDescriptorProto{
			Name:    proto.String("example.proto"),
			Package: proto.String("example"),
			Options: &descriptorpb.FileOptions{
				GoPackage: proto.String("example.com/path/to/example;example"),
			},
		},
	}
	msg := &descriptor.Message{DescriptorProto: msgDesc, File: file}
	fields := make([]*descriptor.Field, 0, len(fieldDescriptors))
	for _, fd := range fieldDescriptors {
		fields = append(fields, &descriptor.Field{
			FieldDescriptorProto: fd,
			Message:              msg,
		})
	}
	msg.Fields = fields

	method := &descriptor.Method{
		MethodDescriptorProto: &descriptorpb.MethodDescriptorProto{
			Name:       proto.String("DoThing"),
			InputType:  proto.String(".example.ReqMsg"),
			OutputType: proto.String(".example.ReqMsg"),
		},
		RequestType:  msg,
		ResponseType: msg,
	}

	binding := &descriptor.Binding{
		HTTPMethod: "GET",
		Method:     method,
	}

	fieldByName := func(name string) *descriptor.Field {
		for _, f := range fields {
			if *f.Name == name {
				return f
			}
		}
		t.Fatalf("path param %q not in fields", name)
		return nil
	}
	for _, name := range pathParamNames {
		target := fieldByName(name)
		binding.PathParams = append(binding.PathParams, descriptor.Parameter{
			FieldPath: descriptor.FieldPath{{Name: name, Target: target}},
			Target:    target,
			Method:    method,
		})
	}

	return binding
}

// TestBuildPathParameters_DescriptionFromFieldExtension verifies that
// openapiv3_field.description on a proto field flows to the path
// parameter's Description, not only the parameter's Schema.Description.
func TestBuildPathParameters_DescriptionFromFieldExtension(t *testing.T) {
	binding := newParamFixture(t,
		[]string{"id", "name"},
		[]string{"id"},
		map[string]string{"id": "the user id"},
	)
	params := buildPathParameters(binding, descriptor.NewRegistry(), map[string]string{})
	if len(params) != 1 {
		t.Fatalf("expected 1 path parameter, got %d", len(params))
	}
	if got := params[0].Description; got != "the user id" {
		t.Errorf("expected parameter.description=%q, got %q", "the user id", got)
	}
}

// TestBuildPathParameters_NoDescriptionWhenFieldUnannotated verifies that
// fields without the openapiv3_field extension produce parameters with an
// empty Description (omitted in JSON).
func TestBuildPathParameters_NoDescriptionWhenFieldUnannotated(t *testing.T) {
	binding := newParamFixture(t,
		[]string{"id"},
		[]string{"id"},
		nil,
	)
	params := buildPathParameters(binding, descriptor.NewRegistry(), map[string]string{})
	if len(params) != 1 {
		t.Fatalf("expected 1 path parameter, got %d", len(params))
	}
	if got := params[0].Description; got != "" {
		t.Errorf("expected empty parameter.description, got %q", got)
	}
}

// newQueryParamFixture is like newParamFixture but also loads the message
// into a fresh registry, because buildQueryParameters does registry.LookupMsg
// to find the request message.
func newQueryParamFixture(t *testing.T, fieldNames []string, descriptions map[string]string) (*descriptor.Binding, *descriptor.Registry) {
	t.Helper()
	binding := newParamFixture(t, fieldNames, nil, descriptions)

	file := binding.Method.RequestType.File
	file.FileDescriptorProto.MessageType = []*descriptorpb.DescriptorProto{
		binding.Method.RequestType.DescriptorProto,
	}
	reg := descriptor.NewRegistry()
	if err := reg.Load(&pluginpb.CodeGeneratorRequest{
		ProtoFile: []*descriptorpb.FileDescriptorProto{file.FileDescriptorProto},
	}); err != nil {
		t.Fatalf("reg.Load: %v", err)
	}
	return binding, reg
}

func newQueryParamFixtureWithFields(t *testing.T, fieldDescriptors ...*descriptorpb.FieldDescriptorProto) (*descriptor.Binding, *descriptor.Registry) {
	t.Helper()

	msgDesc := &descriptorpb.DescriptorProto{
		Name:  proto.String("ReqMsg"),
		Field: fieldDescriptors,
	}
	file := &descriptor.File{
		FileDescriptorProto: &descriptorpb.FileDescriptorProto{
			Name:    proto.String("example.proto"),
			Package: proto.String("example"),
			Options: &descriptorpb.FileOptions{
				GoPackage: proto.String("example.com/path/to/example;example"),
			},
			MessageType: []*descriptorpb.DescriptorProto{msgDesc},
		},
	}
	msg := &descriptor.Message{DescriptorProto: msgDesc, File: file}
	method := &descriptor.Method{
		MethodDescriptorProto: &descriptorpb.MethodDescriptorProto{
			Name:       proto.String("DoThing"),
			InputType:  proto.String(".example.ReqMsg"),
			OutputType: proto.String(".example.ReqMsg"),
		},
		RequestType:  msg,
		ResponseType: msg,
	}
	binding := &descriptor.Binding{
		HTTPMethod: "GET",
		Method:     method,
	}

	reg := descriptor.NewRegistry()
	if err := reg.Load(&pluginpb.CodeGeneratorRequest{
		ProtoFile: []*descriptorpb.FileDescriptorProto{file.FileDescriptorProto},
	}); err != nil {
		t.Fatalf("reg.Load: %v", err)
	}
	return binding, reg
}

// TestBuildQueryParameters_DescriptionFromFieldExtension covers the scalar
// (non-enum) branch in buildQueryParameters.
func TestBuildQueryParameters_DescriptionFromFieldExtension(t *testing.T) {
	binding, reg := newQueryParamFixture(t,
		[]string{"filter", "limit"},
		map[string]string{"filter": "search filter expression"},
	)
	params := buildQueryParameters(binding, map[string]*OpenAPIV3SchemaRef{}, map[string]string{}, reg)
	var got string
	for _, p := range params {
		if p.Name == "filter" {
			got = p.Description
			break
		}
	}
	if got != "search filter expression" {
		t.Errorf("expected parameter.description=%q, got %q", "search filter expression", got)
	}
}

// --- Fix 1: repeated-field array metadata ---

// TestRepeatedField_NonReferences_DescriptionMinMaxOnArraySchema exercises
// buildPropertySchemaFromField (the non-references variant) for the same three fields.
func TestRepeatedField_NonReferences_DescriptionMinMaxOnArraySchema(t *testing.T) {
	field := makeRepeatedFieldWithExtension("items", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{
		Description: "list of strings",
		MinItems:    2,
		MaxItems:    20,
	})
	reg := descriptor.NewRegistry()
	schema := buildPropertySchemaFromField(field, map[string]*OpenAPIV3SchemaRef{}, map[string]string{}, reg)
	if schema == nil {
		t.Fatal("expected non-nil schema")
	}
	if schema.Type != "array" {
		t.Fatalf("expected array schema, got %q", schema.Type)
	}
	if schema.Description != "list of strings" {
		t.Errorf("expected description %q, got %q", "list of strings", schema.Description)
	}
	wantMinItems(t, schema.MinItems, 2)
	if schema.MaxItems != 20 {
		t.Errorf("expected maxItems=20, got %d", schema.MaxItems)
	}
}

// TestRepeatedField_ItemsSchemaStillPopulated verifies that adding description/minItems/maxItems
// to the array wrapper does not destroy the Items sub-schema.
func TestRepeatedField_ItemsSchemaStillPopulated(t *testing.T) {
	field := makeRepeatedFieldWithExtension("tags", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{
		Description: "tags",
		MinItems:    1,
	})
	reg := descriptor.NewRegistry()

	t.Run("with-references variant", func(t *testing.T) {
		schema := buildPropertySchemaWithReferencesFromField(field, reg, map[string]string{})
		if schema == nil {
			t.Fatal("expected non-nil schema")
		}
		if schema.Items == nil {
			t.Fatal("Items must not be nil after setting description/minItems on array")
		}
	})

	t.Run("non-references variant", func(t *testing.T) {
		schema := buildPropertySchemaFromField(field, map[string]*OpenAPIV3SchemaRef{}, map[string]string{}, reg)
		if schema == nil {
			t.Fatal("expected non-nil schema")
		}
		if schema.Items == nil {
			t.Fatal("Items must not be nil after setting description/minItems on array")
		}
	})
}

// TestRepeatedField_NoExtension_ArrayMetadataIsZero checks that a repeated field with no
// extension does not accidentally get non-zero description/minItems/maxItems.
func TestRepeatedField_NoExtension_ArrayMetadataIsZero(t *testing.T) {
	field := makeRepeatedField("values", descriptorpb.FieldDescriptorProto_TYPE_INT64)
	reg := descriptor.NewRegistry()

	t.Run("with-references variant", func(t *testing.T) {
		schema := buildPropertySchemaWithReferencesFromField(field, reg, map[string]string{})
		if schema == nil {
			t.Fatal("expected non-nil schema")
		}
		if schema.Description != "" {
			t.Errorf("expected empty description, got %q", schema.Description)
		}
		// Arrays always emit minItems (default 0), so an array with no
		// min_items annotation carries minItems: 0.
		wantMinItems(t, schema.MinItems, 0)
		if schema.MaxItems != 0 {
			t.Errorf("expected maxItems=0, got %d", schema.MaxItems)
		}
	})

	t.Run("non-references variant", func(t *testing.T) {
		schema := buildPropertySchemaFromField(field, map[string]*OpenAPIV3SchemaRef{}, map[string]string{}, reg)
		if schema == nil {
			t.Fatal("expected non-nil schema")
		}
		if schema.Description != "" {
			t.Errorf("expected empty description, got %q", schema.Description)
		}
		// Arrays always emit minItems (default 0), so an array with no
		// min_items annotation carries minItems: 0.
		wantMinItems(t, schema.MinItems, 0)
		if schema.MaxItems != 0 {
			t.Errorf("expected maxItems=0, got %d", schema.MaxItems)
		}
	})
}

// TestSingularField_ArrayMetadataNotApplied confirms non-repeated fields do not get
// description/minItems/maxItems injected from the array-wrapper path.
func TestSingularField_ArrayMetadataNotApplied(t *testing.T) {
	field := makeSingularFieldWithExtension("name", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{
		Description: "a name",
		MinItems:    3,
		MaxItems:    7,
	})
	reg := descriptor.NewRegistry()

	t.Run("with-references variant", func(t *testing.T) {
		schema := buildPropertySchemaWithReferencesFromField(field, reg, map[string]string{})
		if schema == nil {
			t.Fatal("expected non-nil schema")
		}
		if schema.Type == "array" {
			t.Error("singular field must not produce an array schema")
		}
		// MinItems/MaxItems on a scalar schema would be meaningless; the array
		// default-emit applies only to array wrappers, so a singular field
		// leaves MinItems unset (nil).
		if schema.MinItems != nil {
			t.Errorf("singular field schema should have no MinItems, got %d", *schema.MinItems)
		}
		if schema.MaxItems != 0 {
			t.Errorf("singular field schema should have MaxItems=0, got %d", schema.MaxItems)
		}
	})

	t.Run("non-references variant", func(t *testing.T) {
		schema := buildPropertySchemaFromField(field, map[string]*OpenAPIV3SchemaRef{}, map[string]string{}, reg)
		if schema == nil {
			t.Fatal("expected non-nil schema")
		}
		if schema.Type == "array" {
			t.Error("singular field must not produce an array schema")
		}
	})
}

// TestRepeatedField_DescriptionOnly verifies that setting only description (no min/maxItems)
// leaves min/maxItems at zero.
func TestRepeatedField_DescriptionOnly(t *testing.T) {
	field := makeRepeatedFieldWithExtension("labels", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{
		Description: "label list",
	})
	reg := descriptor.NewRegistry()
	schema := buildPropertySchemaWithReferencesFromField(field, reg, map[string]string{})
	if schema == nil {
		t.Fatal("expected non-nil schema")
	}
	if schema.Description != "label list" {
		t.Errorf("expected %q, got %q", "label list", schema.Description)
	}
	// Arrays always emit minItems (default 0), even with only a description.
	wantMinItems(t, schema.MinItems, 0)
	if schema.MaxItems != 0 {
		t.Errorf("expected maxItems=0 when only description set, got %d", schema.MaxItems)
	}
}

// TestRepeatedField_MinItemsOnly verifies that setting only minItems leaves description empty.
func TestRepeatedField_MinItemsOnly(t *testing.T) {
	field := makeRepeatedFieldWithExtension("ids", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{
		MinItems: 5,
	})
	reg := descriptor.NewRegistry()
	schema := buildPropertySchemaWithReferencesFromField(field, reg, map[string]string{})
	if schema == nil {
		t.Fatal("expected non-nil schema")
	}
	wantMinItems(t, schema.MinItems, 5)
	if schema.Description != "" {
		t.Errorf("expected empty description when only minItems set, got %q", schema.Description)
	}
	if schema.MaxItems != 0 {
		t.Errorf("expected maxItems=0, got %d", schema.MaxItems)
	}
}

// --- Fix 2: filterRequired ---

// TestFilterRequired_EmptyRequired verifies that an empty required list is handled gracefully.
func TestFilterRequired_EmptyRequired(t *testing.T) {
	result := filterRequired([]string{}, map[string]*OpenAPIV3SchemaRef{"name": {}})
	if len(result) != 0 {
		t.Errorf("expected empty result for empty required, got %v", result)
	}
}

// TestFilterRequired_NilRequired verifies that a nil required list returns a non-panic empty result.
func TestFilterRequired_NilRequired(t *testing.T) {
	result := filterRequired(nil, map[string]*OpenAPIV3SchemaRef{"name": {}})
	if len(result) != 0 {
		t.Errorf("expected empty result for nil required, got %v", result)
	}
}

// TestFilterRequired_EmptyBodyProperties verifies that when no body properties exist,
// all required fields are filtered out.
func TestFilterRequired_EmptyBodyProperties(t *testing.T) {
	result := filterRequired([]string{"id", "name"}, map[string]*OpenAPIV3SchemaRef{})
	if len(result) != 0 {
		t.Errorf("expected all fields filtered when body is empty, got %v", result)
	}
}

// TestFilterRequired_NilBodyProperties verifies nil bodyProperties is safe (treats as empty).
func TestFilterRequired_NilBodyProperties(t *testing.T) {
	result := filterRequired([]string{"id"}, nil)
	if len(result) != 0 {
		t.Errorf("expected empty result for nil bodyProperties, got %v", result)
	}
}

// TestFilterRequired_OrderPreserved verifies that the relative order of kept fields is preserved.
func TestFilterRequired_OrderPreserved(t *testing.T) {
	props := map[string]*OpenAPIV3SchemaRef{"b": {}, "d": {}}
	result := filterRequired([]string{"a", "b", "c", "d", "e"}, props)
	want := []string{"b", "d"}
	if !slices.Equal(result, want) {
		t.Errorf("expected order %v, got %v", want, result)
	}
}

// --- Fix 3: empty-message additionalProperties ---

// TestBuildSchemaFromFields_EmptyMessage_NonReferencesVariant tests buildSchemaFromFields
// (the non-references variant) with nil fields.
func TestBuildSchemaFromFields_EmptyMessage_NonReferencesVariant(t *testing.T) {
	schema := buildSchemaFromFields(
		nil,
		map[string]*OpenAPIV3SchemaRef{},
		nil,
		"",
		"",
		nil,
		nil,
		map[string]string{},
		descriptor.NewRegistry(),
	)
	if schema == nil {
		t.Fatal("expected non-nil schema")
	}
	if schema.AdditionalProperties != false {
		t.Errorf("expected additionalProperties=false for empty message (non-references), got %v", schema.AdditionalProperties)
	}
}

// TestBuildSchemaFromFields_EmptySlice_AdditionalPropertiesFalse tests that an explicitly
// empty (non-nil) slice of fields also triggers additionalProperties=false.
func TestBuildSchemaFromFields_EmptySlice_AdditionalPropertiesFalse(t *testing.T) {
	t.Run("with-references variant", func(t *testing.T) {
		schema := buildSchemaFromFieldsWithReferences(
			[]*descriptor.Field{},
			descriptor.NewRegistry(),
			nil, "", "", nil, nil,
			map[string]string{},
		)
		if schema == nil {
			t.Fatal("expected non-nil schema")
		}
		if schema.AdditionalProperties != false {
			t.Errorf("expected additionalProperties=false for empty slice, got %v", schema.AdditionalProperties)
		}
	})

	t.Run("non-references variant", func(t *testing.T) {
		schema := buildSchemaFromFields(
			[]*descriptor.Field{},
			map[string]*OpenAPIV3SchemaRef{},
			nil, "", "", nil, nil,
			map[string]string{},
			descriptor.NewRegistry(),
		)
		if schema == nil {
			t.Fatal("expected non-nil schema")
		}
		if schema.AdditionalProperties != false {
			t.Errorf("expected additionalProperties=false for empty slice, got %v", schema.AdditionalProperties)
		}
	})
}

// TestBuildSchemaFromFields_NonEmptyMessage_NoAdditionalPropertiesFalse verifies that a
// message with at least one field does NOT get additionalProperties=false injected.
func TestBuildSchemaFromFields_NonEmptyMessage_NoAdditionalPropertiesFalse(t *testing.T) {
	field := makeRepeatedField("items", descriptorpb.FieldDescriptorProto_TYPE_STRING)

	t.Run("with-references variant", func(t *testing.T) {
		schema := buildSchemaFromFieldsWithReferences(
			[]*descriptor.Field{field},
			descriptor.NewRegistry(),
			nil, "", "", nil, nil,
			map[string]string{},
		)
		if schema == nil {
			t.Fatal("expected non-nil schema")
		}
		if schema.AdditionalProperties == false {
			t.Error("non-empty message must not have additionalProperties=false")
		}
	})

	t.Run("non-references variant", func(t *testing.T) {
		schema := buildSchemaFromFields(
			[]*descriptor.Field{field},
			map[string]*OpenAPIV3SchemaRef{},
			nil, "", "", nil, nil,
			map[string]string{},
			descriptor.NewRegistry(),
		)
		if schema == nil {
			t.Fatal("expected non-nil schema")
		}
		if schema.AdditionalProperties == false {
			t.Error("non-empty message must not have additionalProperties=false")
		}
	})
}

// ---------------------------------------------------------------------------
// Response.examples → OpenAPI v3 MediaType.example
// ---------------------------------------------------------------------------
//
// Tests for the fix that propagates `openapiv3_operation.responses.examples`
// (proto field 4) onto OpenAPI v3 `MediaType.example` for both success (200)
// and non-success responses. This addresses the IBM Cloud OpenAPI ruleset's
// `ibm-success-response-example` rule, which requires response bodies to
// declare a media-type-level example.

func TestMediaTypeExampleValue_JsonMimeWrapsAsRawExample(t *testing.T) {
	got := mediaTypeExampleValue("application/json", `{"foo":"bar"}`)
	raw, ok := got.(RawExample)
	if !ok {
		t.Fatalf("application/json: expected RawExample, got %T", got)
	}
	if string(raw) != `{"foo":"bar"}` {
		t.Errorf("application/json: expected raw JSON preserved, got %q", string(raw))
	}
}

func TestMediaTypeExampleValue_JsonSuffixWrapsAsRawExample(t *testing.T) {
	cases := []string{"application/cloudevents+json", "application/vnd.api+json", "application/problem+json"}
	for _, mime := range cases {
		t.Run(mime, func(t *testing.T) {
			got := mediaTypeExampleValue(mime, `{"ok":true}`)
			if _, ok := got.(RawExample); !ok {
				t.Errorf("%s: expected RawExample wrapping, got %T", mime, got)
			}
		})
	}
}

func TestMediaTypeExampleValue_JsonMediaTypeWithParametersOrCasing(t *testing.T) {
	// RFC 9110: media-type tokens are case-insensitive; charset/parameter
	// suffixes are valid. All of these should still wrap as RawExample so the
	// generated MediaType.example is a JSON object, not a quoted string.
	cases := []string{
		"application/json; charset=utf-8",
		"Application/JSON",
		"APPLICATION/JSON",
		"application/problem+json; charset=utf-8",
		"Application/Cloudevents+JSON",
		"  application/json  ",
	}
	for _, mime := range cases {
		t.Run(mime, func(t *testing.T) {
			got := mediaTypeExampleValue(mime, `{"ok":true}`)
			if _, ok := got.(RawExample); !ok {
				t.Errorf("%q: expected RawExample wrapping, got %T (value=%v)", mime, got, got)
			}
		})
	}
}

func TestMediaTypeExampleValue_NonJsonMimeReturnsRawString(t *testing.T) {
	cases := map[string]string{
		"application/xml": "<foo>bar</foo>",
		"text/plain":      "hello world",
		"text/html":       "<p>hi</p>",
	}
	for mime, want := range cases {
		t.Run(mime, func(t *testing.T) {
			got := mediaTypeExampleValue(mime, want)
			s, ok := got.(string)
			if !ok {
				t.Fatalf("%s: expected string, got %T", mime, got)
			}
			if s != want {
				t.Errorf("%s: expected %q, got %q", mime, want, s)
			}
		})
	}
}

func TestApplyResponseExamples_NilResponseIsNoop(t *testing.T) {
	// Must not panic.
	applyResponseExamples(nil, map[string]string{"application/json": `{"a":1}`})
}

func TestApplyResponseExamples_EmptyMapLeavesContentUnchanged(t *testing.T) {
	resp := &OpenAPIV3Response{
		Content: map[string]OpenAPIV3MediaType{
			"application/json": {Schema: &OpenAPIV3SchemaRef{Ref: "#/components/schemas/Foo"}},
		},
	}
	applyResponseExamples(resp, nil)
	applyResponseExamples(resp, map[string]string{})

	mt, ok := resp.Content["application/json"]
	if !ok {
		t.Fatal("expected application/json entry to still exist")
	}
	if mt.Example != nil {
		t.Errorf("expected no Example to be set, got %v", mt.Example)
	}
	if mt.Schema == nil || mt.Schema.Ref != "#/components/schemas/Foo" {
		t.Error("schema must be preserved when no examples are applied")
	}
}

func TestApplyResponseExamples_NilContentMapIsCreated(t *testing.T) {
	resp := &OpenAPIV3Response{}
	applyResponseExamples(resp, map[string]string{"application/json": `{"k":"v"}`})

	if resp.Content == nil {
		t.Fatal("expected Content map to be created")
	}
	if _, ok := resp.Content["application/json"]; !ok {
		t.Errorf("expected application/json entry to be created, got keys %v", keysOfContent(resp.Content))
	}
}

func TestApplyResponseExamples_PreservesSchemaOnExistingEntry(t *testing.T) {
	resp := &OpenAPIV3Response{
		Content: map[string]OpenAPIV3MediaType{
			"application/json": {Schema: &OpenAPIV3SchemaRef{Ref: "#/components/schemas/Foo"}},
		},
	}
	applyResponseExamples(resp, map[string]string{"application/json": `{"id":1}`})

	mt := resp.Content["application/json"]
	if mt.Schema == nil || mt.Schema.Ref != "#/components/schemas/Foo" {
		t.Error("existing schema must be preserved when example is set")
	}
	if mt.Example == nil {
		t.Fatal("expected Example to be populated")
	}
	if _, ok := mt.Example.(RawExample); !ok {
		t.Errorf("expected RawExample for application/json, got %T", mt.Example)
	}
}

func TestApplyResponseExamples_AddsBrandNewMimeType(t *testing.T) {
	resp := &OpenAPIV3Response{
		Content: map[string]OpenAPIV3MediaType{
			"application/json": {Schema: &OpenAPIV3SchemaRef{Ref: "#/components/schemas/Foo"}},
		},
	}
	applyResponseExamples(resp, map[string]string{"application/xml": "<id>1</id>"})

	if _, ok := resp.Content["application/json"]; !ok {
		t.Error("existing application/json entry must be preserved")
	}
	xml, ok := resp.Content["application/xml"]
	if !ok {
		t.Fatalf("expected application/xml entry to be created, got keys %v", keysOfContent(resp.Content))
	}
	if xml.Example != "<id>1</id>" {
		t.Errorf("expected raw string example for application/xml, got %v", xml.Example)
	}
}

func TestApplyResponseExamples_MultipleMimeTypesAllSet(t *testing.T) {
	resp := &OpenAPIV3Response{}
	applyResponseExamples(resp, map[string]string{
		"application/json": `{"a":1}`,
		"application/xml":  "<a>1</a>",
		"text/plain":       "a=1",
	})

	if len(resp.Content) != 3 {
		t.Fatalf("expected 3 content entries, got %d (keys=%v)", len(resp.Content), keysOfContent(resp.Content))
	}
	if _, ok := resp.Content["application/json"].Example.(RawExample); !ok {
		t.Errorf("application/json: expected RawExample, got %T", resp.Content["application/json"].Example)
	}
	if s, _ := resp.Content["application/xml"].Example.(string); s != "<a>1</a>" {
		t.Errorf("application/xml: expected raw string, got %v", resp.Content["application/xml"].Example)
	}
	if s, _ := resp.Content["text/plain"].Example.(string); s != "a=1" {
		t.Errorf("text/plain: expected raw string, got %v", resp.Content["text/plain"].Example)
	}
}

// testErrorSchemaRef is the default error body schema reference (google.rpc.Status)
// that buildOpenAPIV3Paths threads into the response builder in real generation.
const testErrorSchemaRef = "#/components/schemas/Status"

func TestExtractOpenAPIV3ResponsesFromProtoExtension_EmitsExamplesForNonSuccess(t *testing.T) {
	op := &options.Operation{
		Responses: map[string]*options.Response{
			"404": {
				Description: "Not Found",
				Examples: map[string]string{
					"application/json": `{"error":"not found","code":404}`,
				},
			},
		},
	}
	// No error schema ref here so the test isolates the example round-trip.
	got := extractOpenAPIV3ResponsesFromProtoExtension(op, "")

	resp, ok := got["404"]
	if !ok || resp.OpenAPIV3Response == nil {
		t.Fatalf("expected 404 response, got keys %v", keysOfResponses(got))
	}
	if resp.Description != "Not Found" {
		t.Errorf("expected description preserved, got %q", resp.Description)
	}
	mt, ok := resp.Content["application/json"]
	if !ok {
		t.Fatalf("expected application/json content, got keys %v", keysOfContent(resp.Content))
	}
	raw, ok := mt.Example.(RawExample)
	if !ok {
		t.Fatalf("expected RawExample on 404 application/json, got %T", mt.Example)
	}
	if string(raw) != `{"error":"not found","code":404}` {
		t.Errorf("expected raw JSON preserved, got %q", string(raw))
	}
}

func TestExtractOpenAPIV3ResponsesFromProtoExtension_NoContentWhenNoErrorSchema(t *testing.T) {
	op := &options.Operation{
		Responses: map[string]*options.Response{
			"500": {Description: "Internal Server Error"},
		},
	}
	// When no default error schema is available (empty ref), a description-only
	// response stays bodyless rather than fabricating a schemaless content entry.
	got := extractOpenAPIV3ResponsesFromProtoExtension(op, "")

	resp, ok := got["500"]
	if !ok || resp.OpenAPIV3Response == nil {
		t.Fatalf("expected 500 response, got keys %v", keysOfResponses(got))
	}
	if len(resp.Content) != 0 {
		t.Errorf("expected no content entry for a description-only response without an error schema, got %v", resp.Content)
	}
}

func TestExtractOpenAPIV3ResponsesFromProtoExtension_ErrorFallsBackToErrorSchema(t *testing.T) {
	op := &options.Operation{
		Responses: map[string]*options.Response{
			"500": {Description: "Internal Server Error"},
		},
	}
	// A description-only error response (4xx/5xx) references the default error
	// schema so it documents a JSON body — satisfying both
	// ibm-content-contains-schema and ibm-request-and-response-content.
	got := extractOpenAPIV3ResponsesFromProtoExtension(op, testErrorSchemaRef)

	mt, ok := got["500"].Content["application/json"]
	if !ok || mt.Schema == nil {
		t.Fatalf("expected the 500 error to carry the default error schema, got %#v", got["500"].Content)
	}
	if mt.Schema.Ref != testErrorSchemaRef {
		t.Errorf("expected schema $ref %q, got %q", testErrorSchemaRef, mt.Schema.Ref)
	}
}

// TestExtractOpenAPIV3ResponsesFromProtoExtension_SuccessStatusStillSkipped guards the
// existing behavior that the 200 entry is not emitted here — the success response is
// built downstream from the gRPC response type. The success-response examples are
// merged in via applyResponseExamples at the call site, not from this function.
func TestExtractOpenAPIV3ResponsesFromProtoExtension_SuccessStatusStillSkipped(t *testing.T) {
	op := &options.Operation{
		Responses: map[string]*options.Response{
			"200": {
				Description: "OK",
				Examples: map[string]string{
					"application/json": `{"foo":"bar"}`,
				},
			},
			"404": {Description: "Not Found"},
		},
	}
	got := extractOpenAPIV3ResponsesFromProtoExtension(op, testErrorSchemaRef)
	if _, ok := got["200"]; ok {
		t.Error("200 response must not be emitted by extractOpenAPIV3ResponsesFromProtoExtension (success path is handled separately)")
	}
	if _, ok := got["404"]; !ok {
		t.Error("404 response must still be emitted")
	}
}

func TestExtractOpenAPIV3ResponsesFromProtoExtension_MultipleNonSuccessResponses(t *testing.T) {
	op := &options.Operation{
		Responses: map[string]*options.Response{
			"400": {
				Description: "Bad Request",
				Examples: map[string]string{
					"application/json": `{"error":"bad request"}`,
				},
			},
			"500": {Description: "Internal Server Error"}, // no examples
		},
	}
	got := extractOpenAPIV3ResponsesFromProtoExtension(op, testErrorSchemaRef)
	if len(got) != 2 {
		t.Fatalf("expected 2 responses, got %d (keys=%v)", len(got), keysOfResponses(got))
	}

	if mt := got["400"].Content["application/json"]; mt.Example == nil {
		t.Error("400 must have an example")
	} else if _, ok := mt.Example.(RawExample); !ok {
		t.Errorf("400 example: expected RawExample, got %T", mt.Example)
	}

	if mt := got["500"].Content["application/json"]; mt.Example != nil {
		t.Errorf("500 must not have an example, got %v", mt.Example)
	}
}

// TestMediaTypeExample_JsonRoundTripsAsJsonObject verifies that the rendered
// OpenAPI v3 spec emits a JSON example object (not a stringified blob) for
// application/json examples. This is the key behavior the IBM rule checks for.
func TestMediaTypeExample_JsonRoundTripsAsJsonObject(t *testing.T) {
	resp := &OpenAPIV3Response{}
	applyResponseExamples(resp, map[string]string{
		"application/json": `{"estimated_bytes":"13251739648","count":42}`,
	})

	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	// The example must appear as a JSON object literal, not a quoted string.
	if !strings.Contains(string(raw), `"example":{"estimated_bytes":"13251739648","count":42}`) {
		t.Errorf("application/json example must be emitted as a JSON object, got:\n%s", string(raw))
	}
}

// TestMediaTypeExample_NonJsonRendersAsString verifies the symmetric behavior
// for non-JSON mime types — the example is emitted as a JSON string.
func TestMediaTypeExample_NonJsonRendersAsString(t *testing.T) {
	resp := &OpenAPIV3Response{}
	applyResponseExamples(resp, map[string]string{
		"application/xml": "<root><id>1</id></root>",
	})

	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	if !strings.Contains(string(raw), `"example":"\u003croot\u003e\u003cid\u003e1\u003c/id\u003e\u003c/root\u003e"`) &&
		!strings.Contains(string(raw), `"example":"<root><id>1</id></root>"`) {
		t.Errorf("application/xml example must be emitted as a JSON string, got:\n%s", string(raw))
	}
}

// keysOfContent and keysOfResponses produce deterministic key listings for
// error messages.
func keysOfContent(m map[string]OpenAPIV3MediaType) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func keysOfResponses(m OpenAPIV3Responses) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func responseWithRefSchema(description, ref string) *options.Response {
	return &options.Response{
		Description: description,
		Schema: &options.Schema{
			JsonSchema: &options.JSONSchema{Ref: ref},
		},
	}
}

func TestExtractResponses_CustomResponseCarriesRefSchema(t *testing.T) {
	op := &options.Operation{
		Responses: map[string]*options.Response{
			"201": responseWithRefSchema("Created", "CreateFooResponse"),
		},
	}
	responses := extractOpenAPIV3ResponsesFromProtoExtension(op, testErrorSchemaRef)

	resp, ok := responses["201"]
	if !ok {
		t.Fatal("expected a 201 response")
	}
	if resp.OpenAPIV3Response == nil {
		t.Fatal("expected non-nil response body")
	}
	if resp.Description != "Created" {
		t.Errorf("expected description %q, got %q", "Created", resp.Description)
	}
	media, ok := resp.Content["application/json"]
	if !ok {
		t.Fatal("expected application/json content on the 201 response")
	}
	if media.Schema == nil {
		t.Fatal("expected the 201 response content to carry a schema (was dropped before the fix)")
	}
	if want := "#/components/schemas/CreateFooResponse"; media.Schema.Ref != want {
		t.Errorf("expected schema $ref %q, got %q", want, media.Schema.Ref)
	}
}

func TestExtractResponses_CustomErrorResponseDescriptionOnly(t *testing.T) {
	op := &options.Operation{
		Responses: map[string]*options.Response{
			"404": {Description: "Not Found"},
		},
	}
	responses := extractOpenAPIV3ResponsesFromProtoExtension(op, testErrorSchemaRef)

	resp, ok := responses["404"]
	if !ok {
		t.Fatal("expected a 404 response")
	}
	if resp.Description != "Not Found" {
		t.Errorf("expected description %q, got %q", "Not Found", resp.Description)
	}
	// A description-only error response falls back to the default error schema.
	media, ok := resp.Content["application/json"]
	if !ok || media.Schema == nil {
		t.Fatalf("expected the default error schema on a description-only error, got %#v", resp.Content)
	}
	if media.Schema.Ref != testErrorSchemaRef {
		t.Errorf("expected schema $ref %q, got %q", testErrorSchemaRef, media.Schema.Ref)
	}
}

// TestExtractResponses_ExamplesWithoutSchemaStillEmitContent guards that
// dropping the fabricated empty media type does not lose an annotated example:
// applyResponseExamples must still lazily create the content entry so a custom
// response with examples (but no schema) carries the example.
func TestExtractResponses_ExamplesWithoutSchemaStillEmitContent(t *testing.T) {
	op := &options.Operation{
		Responses: map[string]*options.Response{
			"400": {
				Description: "Bad Request",
				Examples: map[string]string{
					"application/json": `{"error":"bad"}`,
				},
			},
		},
	}
	// Empty error ref so the only content source is the annotated example.
	responses := extractOpenAPIV3ResponsesFromProtoExtension(op, "")

	resp, ok := responses["400"]
	if !ok {
		t.Fatal("expected a 400 response")
	}
	mt, ok := resp.Content["application/json"]
	if !ok {
		t.Fatal("expected a content entry to be created for an annotated example")
	}
	if mt.Example == nil {
		t.Error("expected the annotated example to be carried on the media type")
	}
	if mt.Schema != nil {
		t.Errorf("expected no schema on an example-only response, got %v", mt.Schema)
	}
}

// TestExtractResponses_InlineSchemaIsRendered guards that an annotated non-$ref
// (inline) response schema is rendered as content rather than dropped.
func TestExtractResponses_InlineSchemaIsRendered(t *testing.T) {
	op := &options.Operation{
		Responses: map[string]*options.Response{
			"400": {
				Description: "Bad Request",
				Schema: &options.Schema{
					JsonSchema: &options.JSONSchema{
						Type:   []options.JSONSchema_JSONSchemaSimpleTypes{options.JSONSchema_STRING},
						Format: "uuid",
					},
				},
			},
		},
	}
	responses := extractOpenAPIV3ResponsesFromProtoExtension(op, testErrorSchemaRef)

	resp, ok := responses["400"]
	if !ok {
		t.Fatal("expected a 400 response")
	}
	mt, ok := resp.Content["application/json"]
	if !ok || mt.Schema == nil || mt.Schema.OpenAPIV3Schema == nil {
		t.Fatalf("expected an inline schema to be rendered, got %#v", resp.Content)
	}
	if got := mt.Schema.OpenAPIV3Schema.Type; got != "string" {
		t.Errorf("expected type %q, got %q", "string", got)
	}
	if got := mt.Schema.OpenAPIV3Schema.Format; got != "uuid" {
		t.Errorf("expected format %q, got %q", "uuid", got)
	}
}

// TestExtractResponses_InlineSchemaPreservesConstraints guards that array/object
// validation constraints on an inline response schema are not dropped.
func TestExtractResponses_InlineSchemaPreservesConstraints(t *testing.T) {
	op := &options.Operation{
		Responses: map[string]*options.Response{
			"400": {
				Description: "Bad Request",
				Schema: &options.Schema{
					JsonSchema: &options.JSONSchema{
						Type:        []options.JSONSchema_JSONSchemaSimpleTypes{options.JSONSchema_ARRAY},
						MinItems:    1,
						MaxItems:    10,
						UniqueItems: true,
						Default:     "[]",
					},
				},
			},
		},
	}
	responses := extractOpenAPIV3ResponsesFromProtoExtension(op, testErrorSchemaRef)

	s := responses["400"].Content["application/json"].Schema.OpenAPIV3Schema
	if s == nil {
		t.Fatalf("expected an inline schema, got %#v", responses["400"].Content)
	}
	if s.MinItems == nil || *s.MinItems != 1 {
		t.Errorf("expected minItems 1, got %v", s.MinItems)
	}
	if s.MaxItems != 10 {
		t.Errorf("expected maxItems 10, got %d", s.MaxItems)
	}
	if !s.UniqueItems {
		t.Error("expected uniqueItems true")
	}
	if s.Default == nil {
		t.Error("expected default to be preserved")
	}
}

func TestExtractResponses_MultipleCustomResponsesEachCarrySchema(t *testing.T) {
	op := &options.Operation{
		Responses: map[string]*options.Response{
			"201": responseWithRefSchema("Created", "CreateFooResponse"),
			"202": responseWithRefSchema("Accepted", "AcceptFooResponse"),
			"409": {Description: "Conflict"},
		},
	}
	responses := extractOpenAPIV3ResponsesFromProtoExtension(op, testErrorSchemaRef)

	for code, wantRef := range map[string]string{
		"201": "#/components/schemas/CreateFooResponse",
		"202": "#/components/schemas/AcceptFooResponse",
	} {
		resp, ok := responses[code]
		if !ok {
			t.Fatalf("expected a %s response", code)
		}
		media, ok := resp.Content["application/json"]
		if !ok || media.Schema == nil {
			t.Fatalf("expected %s response to carry a schema", code)
		}
		if media.Schema.Ref != wantRef {
			t.Errorf("%s: expected schema $ref %q, got %q", code, wantRef, media.Schema.Ref)
		}
	}
	// 409 is description-only but an error, so it falls back to the default error schema.
	if media, ok := responses["409"].Content["application/json"]; !ok || media.Schema == nil {
		t.Errorf("expected the default error schema on description-only 409, got %#v", responses["409"].Content)
	} else if media.Schema.Ref != testErrorSchemaRef {
		t.Errorf("expected 409 schema $ref %q, got %q", testErrorSchemaRef, media.Schema.Ref)
	}
}

func TestExtractResponses_SuccessStatusReserved(t *testing.T) {
	op := &options.Operation{
		Responses: map[string]*options.Response{
			"200": responseWithRefSchema("OK", "GetFooResponse"),
		},
	}
	responses := extractOpenAPIV3ResponsesFromProtoExtension(op, testErrorSchemaRef)
	if _, ok := responses["200"]; ok {
		t.Error("expected the 200 response to be reserved for the main response body and skipped")
	}
}

// makeFieldWithExtension builds a singular field carrying an openapiv3_field
// extension; a nil ext attaches none.
func makeFieldWithExtension(name string, fieldType descriptorpb.FieldDescriptorProto_Type, ext *options.JSONSchema) *descriptor.Field {
	opts := &descriptorpb.FieldOptions{}
	if ext != nil {
		proto.SetExtension(opts, options.E_Openapiv3Field, ext)
	}
	return &descriptor.Field{
		FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{
			Name:    proto.String(name),
			Type:    &fieldType,
			Options: opts,
		},
	}
}

func makeMapFieldWithExtension(t *testing.T, name string, valueType descriptorpb.FieldDescriptorProto_Type, ext *options.JSONSchema) (*descriptor.Field, *descriptor.Registry) {
	t.Helper()

	opts := &descriptorpb.FieldOptions{}
	if ext != nil {
		proto.SetExtension(opts, options.E_Openapiv3Field, ext)
	}

	optional := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	repeated := descriptorpb.FieldDescriptorProto_LABEL_REPEATED
	stringType := descriptorpb.FieldDescriptorProto_TYPE_STRING
	messageType := descriptorpb.FieldDescriptorProto_TYPE_MESSAGE
	entryName := mapEntryName(name)
	entryTypeName := ".example.MapMsg." + entryName

	entry := &descriptorpb.DescriptorProto{
		Name: proto.String(entryName),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:   proto.String("key"),
				Number: proto.Int32(1),
				Label:  &optional,
				Type:   &stringType,
			},
			{
				Name:   proto.String("value"),
				Number: proto.Int32(2),
				Label:  &optional,
				Type:   &valueType,
			},
		},
		Options: &descriptorpb.MessageOptions{
			MapEntry: proto.Bool(true),
		},
	}

	msgDesc := &descriptorpb.DescriptorProto{
		Name: proto.String("MapMsg"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:     proto.String(name),
				Number:   proto.Int32(1),
				Label:    &repeated,
				Type:     &messageType,
				TypeName: proto.String(entryTypeName),
				Options:  opts,
			},
		},
		NestedType: []*descriptorpb.DescriptorProto{entry},
	}

	file := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("map_value_schema.proto"),
		Package: proto.String("example"),
		Syntax:  proto.String("proto3"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/path/to/example;example"),
		},
		MessageType: []*descriptorpb.DescriptorProto{msgDesc},
	}

	reg := descriptor.NewRegistry()
	if err := reg.Load(&pluginpb.CodeGeneratorRequest{ProtoFile: []*descriptorpb.FileDescriptorProto{file}}); err != nil {
		t.Fatalf("reg.Load: %v", err)
	}

	msg, err := reg.LookupMsg("", ".example.MapMsg")
	if err != nil {
		t.Fatalf("LookupMsg: %v", err)
	}
	if len(msg.Fields) != 1 {
		t.Fatalf("expected one field, got %d", len(msg.Fields))
	}
	return msg.Fields[0], reg
}

func makeMessageMapFieldWithExtension(t *testing.T, name string, ext *options.JSONSchema) (*descriptor.Field, *descriptor.Registry, map[string]string, map[string]*OpenAPIV3SchemaRef) {
	t.Helper()

	opts := &descriptorpb.FieldOptions{}
	if ext != nil {
		proto.SetExtension(opts, options.E_Openapiv3Field, ext)
	}

	optional := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	repeated := descriptorpb.FieldDescriptorProto_LABEL_REPEATED
	stringType := descriptorpb.FieldDescriptorProto_TYPE_STRING
	messageType := descriptorpb.FieldDescriptorProto_TYPE_MESSAGE
	entryName := mapEntryName(name)
	entryTypeName := ".example.MapMsg." + entryName
	valueTypeName := ".example.ValueMsg"

	valueMsg := &descriptorpb.DescriptorProto{
		Name: proto.String("ValueMsg"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:   proto.String("name"),
				Number: proto.Int32(1),
				Label:  &optional,
				Type:   &stringType,
			},
		},
	}
	entry := &descriptorpb.DescriptorProto{
		Name: proto.String(entryName),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:   proto.String("key"),
				Number: proto.Int32(1),
				Label:  &optional,
				Type:   &stringType,
			},
			{
				Name:     proto.String("value"),
				Number:   proto.Int32(2),
				Label:    &optional,
				Type:     &messageType,
				TypeName: proto.String(valueTypeName),
			},
		},
		Options: &descriptorpb.MessageOptions{
			MapEntry: proto.Bool(true),
		},
	}
	msgDesc := &descriptorpb.DescriptorProto{
		Name: proto.String("MapMsg"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:     proto.String(name),
				Number:   proto.Int32(1),
				Label:    &repeated,
				Type:     &messageType,
				TypeName: proto.String(entryTypeName),
				Options:  opts,
			},
		},
		NestedType: []*descriptorpb.DescriptorProto{entry},
	}
	file := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("message_map_value_schema.proto"),
		Package: proto.String("example"),
		Syntax:  proto.String("proto3"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/path/to/example;example"),
		},
		MessageType: []*descriptorpb.DescriptorProto{msgDesc, valueMsg},
	}

	reg := descriptor.NewRegistry()
	if err := reg.Load(&pluginpb.CodeGeneratorRequest{ProtoFile: []*descriptorpb.FileDescriptorProto{file}}); err != nil {
		t.Fatalf("reg.Load: %v", err)
	}

	msg, err := reg.LookupMsg("", ".example.MapMsg")
	if err != nil {
		t.Fatalf("LookupMsg: %v", err)
	}
	if len(msg.Fields) != 1 {
		t.Fatalf("expected one field, got %d", len(msg.Fields))
	}
	resolvedNames := map[string]string{
		valueTypeName: "ValueMsg",
	}
	schemaMap := map[string]*OpenAPIV3SchemaRef{
		valueTypeName: {OpenAPIV3Schema: &OpenAPIV3Schema{Type: "object"}},
	}
	return msg.Fields[0], reg, resolvedNames, schemaMap
}

func makeWellKnownMapFieldWithExtension(t *testing.T, name, valueTypeName string, dependency *descriptorpb.FileDescriptorProto, ext *options.JSONSchema) (*descriptor.Field, *descriptor.Registry) {
	t.Helper()

	opts := &descriptorpb.FieldOptions{}
	if ext != nil {
		proto.SetExtension(opts, options.E_Openapiv3Field, ext)
	}

	optional := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	repeated := descriptorpb.FieldDescriptorProto_LABEL_REPEATED
	stringType := descriptorpb.FieldDescriptorProto_TYPE_STRING
	messageType := descriptorpb.FieldDescriptorProto_TYPE_MESSAGE
	entryName := mapEntryName(name)
	entryTypeName := ".example.MapMsg." + entryName

	entry := &descriptorpb.DescriptorProto{
		Name: proto.String(entryName),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:   proto.String("key"),
				Number: proto.Int32(1),
				Label:  &optional,
				Type:   &stringType,
			},
			{
				Name:     proto.String("value"),
				Number:   proto.Int32(2),
				Label:    &optional,
				Type:     &messageType,
				TypeName: proto.String(valueTypeName),
			},
		},
		Options: &descriptorpb.MessageOptions{
			MapEntry: proto.Bool(true),
		},
	}
	msgDesc := &descriptorpb.DescriptorProto{
		Name: proto.String("MapMsg"),
		Field: []*descriptorpb.FieldDescriptorProto{
			{
				Name:     proto.String(name),
				Number:   proto.Int32(1),
				Label:    &repeated,
				Type:     &messageType,
				TypeName: proto.String(entryTypeName),
				Options:  opts,
			},
		},
		NestedType: []*descriptorpb.DescriptorProto{entry},
	}
	file := &descriptorpb.FileDescriptorProto{
		Name:       proto.String("well_known_map_value_schema.proto"),
		Package:    proto.String("example"),
		Syntax:     proto.String("proto3"),
		Dependency: []string{dependency.GetName()},
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/path/to/example;example"),
		},
		MessageType: []*descriptorpb.DescriptorProto{msgDesc},
	}

	reg := descriptor.NewRegistry()
	if err := reg.Load(&pluginpb.CodeGeneratorRequest{ProtoFile: []*descriptorpb.FileDescriptorProto{dependency, file}}); err != nil {
		t.Fatalf("reg.Load: %v", err)
	}

	msg, err := reg.LookupMsg("", ".example.MapMsg")
	if err != nil {
		t.Fatalf("LookupMsg: %v", err)
	}
	if len(msg.Fields) != 1 {
		t.Fatalf("expected one field, got %d", len(msg.Fields))
	}
	return msg.Fields[0], reg
}

func mapEntryName(fieldName string) string {
	var b strings.Builder
	for _, part := range strings.Split(fieldName, "_") {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]))
		b.WriteString(part[1:])
	}
	b.WriteString("Entry")
	return b.String()
}

// exampleFromBothSwitches returns the emitted Example via both rendering
// functions, so tests can assert they agree.
func exampleFromBothSwitches(t *testing.T, field *descriptor.Field) (withRefs RawExample, plain RawExample) {
	t.Helper()
	reg := descriptor.NewRegistry()
	refSchema, _ := buildPropertySchemaWithReferencesFromFieldType(field, reg, map[string]string{})
	plainSchema, _ := buildPropertySchemaFromFieldType(field, map[string]*OpenAPIV3SchemaRef{}, map[string]string{}, reg)
	if refSchema == nil || refSchema.OpenAPIV3Schema == nil {
		t.Fatal("buildPropertySchemaWithReferencesFromFieldType returned no inline schema")
	}
	if plainSchema == nil || plainSchema.OpenAPIV3Schema == nil {
		t.Fatal("buildPropertySchemaFromFieldType returned no inline schema")
	}
	return refSchema.OpenAPIV3Schema.Example, plainSchema.OpenAPIV3Schema.Example
}

func TestExample_StringNoExample_OmitsExample(t *testing.T) {
	field := makeFieldWithExtension("name", descriptorpb.FieldDescriptorProto_TYPE_STRING, nil)
	withRefs, plain := exampleFromBothSwitches(t, field)
	if withRefs != nil {
		t.Errorf("with-refs: expected no example, got %q", string(withRefs))
	}
	if plain != nil {
		t.Errorf("plain: expected no example, got %q (fabricated empty example before the fix)", string(plain))
	}
}

func TestExample_StringWithExplicitExample_EmittedVerbatim(t *testing.T) {
	field := makeFieldWithExtension("name", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{
		Example: "hello",
	})
	withRefs, plain := exampleFromBothSwitches(t, field)
	const want = `"hello"`
	if string(withRefs) != want {
		t.Errorf("with-refs: expected example %s, got %q", want, string(withRefs))
	}
	if string(plain) != want {
		t.Errorf("plain: expected example %s, got %q", want, string(plain))
	}
}

// derefMinLength reads a *uint64 MinLength for test assertions; nil reads as a
// sentinel that never matches an expected concrete length.
func derefMinLength(p *uint64) uint64 {
	if p == nil {
		return ^uint64(0)
	}
	return *p
}

func TestExample_StringMinLength1NoExample_NoViolation(t *testing.T) {
	field := makeFieldWithExtension("name", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{
		MinLength: 1,
	})
	reg := descriptor.NewRegistry()
	for _, tc := range []struct {
		name   string
		schema *OpenAPIV3SchemaRef
	}{
		{"withRefs", mustSchema(t, func() *OpenAPIV3SchemaRef {
			s, _ := buildPropertySchemaWithReferencesFromFieldType(field, reg, map[string]string{})
			return s
		})},
		{"plain", mustSchema(t, func() *OpenAPIV3SchemaRef {
			s, _ := buildPropertySchemaFromFieldType(field, map[string]*OpenAPIV3SchemaRef{}, map[string]string{}, reg)
			return s
		})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if derefMinLength(tc.schema.OpenAPIV3Schema.MinLength) != 1 {
				t.Errorf("expected minLength=1, got %d", derefMinLength(tc.schema.OpenAPIV3Schema.MinLength))
			}
			if ex := tc.schema.OpenAPIV3Schema.Example; ex != nil {
				t.Errorf("expected no example (empty example would violate minLength:1), got %q", string(ex))
			}
		})
	}
}

func TestExample_BytesNoExample_OmitsExample(t *testing.T) {
	field := makeFieldWithExtension("blob", descriptorpb.FieldDescriptorProto_TYPE_BYTES, nil)
	withRefs, plain := exampleFromBothSwitches(t, field)
	if withRefs != nil {
		t.Errorf("with-refs: expected no example, got %q", string(withRefs))
	}
	if plain != nil {
		t.Errorf("plain: expected no example, got %q", string(plain))
	}
}

// An empty Example field means no annotation was set: emit no example.
func TestExample_StringEmptyExampleField_TreatedAsAbsent(t *testing.T) {
	field := makeFieldWithExtension("name", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{
		Example: "",
	})
	withRefs, plain := exampleFromBothSwitches(t, field)
	if withRefs != nil || plain != nil {
		t.Errorf("expected empty example field to be treated as absent; got with-refs=%q plain=%q",
			string(withRefs), string(plain))
	}
}

// A deliberate empty-string example (JSON literal `""`) is distinct from an
// absent annotation and must be preserved.
func TestExample_StringDeliberateEmptyStringExample_Preserved(t *testing.T) {
	field := makeFieldWithExtension("name", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{
		Example: `""`,
	})
	withRefs, plain := exampleFromBothSwitches(t, field)
	const want = `""`
	if string(withRefs) != want {
		t.Errorf("with-refs: expected deliberate empty-string example %s, got %q", want, string(withRefs))
	}
	if string(plain) != want {
		t.Errorf("plain: expected deliberate empty-string example %s, got %q", want, string(plain))
	}
}

func mustSchema(t *testing.T, fn func() *OpenAPIV3SchemaRef) *OpenAPIV3SchemaRef {
	t.Helper()
	s := fn()
	if s == nil || s.OpenAPIV3Schema == nil {
		t.Fatal("expected non-nil inline schema")
	}
	return s
}

// wrapper type (e.g. ".google.protobuf.Int64Value").
func makeWrapperField(name, typeName string, ext *options.JSONSchema) *descriptor.Field {
	opts := &descriptorpb.FieldOptions{}
	if ext != nil {
		proto.SetExtension(opts, options.E_Openapiv3Field, ext)
	}
	msgType := descriptorpb.FieldDescriptorProto_TYPE_MESSAGE
	return &descriptor.Field{
		FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{
			Name:     proto.String(name),
			Type:     &msgType,
			TypeName: proto.String(typeName),
			Options:  opts,
		},
	}
}

// inlineSchemasBothSwitches returns the inline schema produced by both
// near-duplicate field-rendering functions so a test can assert they agree.
func inlineSchemasBothSwitches(t *testing.T, field *descriptor.Field) (withRefs *OpenAPIV3Schema, plain *OpenAPIV3Schema) {
	t.Helper()
	reg := descriptor.NewRegistry()
	a, _ := buildPropertySchemaWithReferencesFromFieldType(field, reg, map[string]string{})
	b, _ := buildPropertySchemaFromFieldType(field, map[string]*OpenAPIV3SchemaRef{}, map[string]string{}, reg)
	if a == nil || a.OpenAPIV3Schema == nil {
		t.Fatal("buildPropertySchemaWithReferencesFromFieldType returned no inline schema")
	}
	if b == nil || b.OpenAPIV3Schema == nil {
		t.Fatal("buildPropertySchemaFromFieldType returned no inline schema")
	}
	return a.OpenAPIV3Schema, b.OpenAPIV3Schema
}

func assertStringIntSchema(t *testing.T, s *OpenAPIV3Schema, wantPattern string) {
	t.Helper()
	if s.Type != "string" {
		t.Errorf("expected type=string, got %q", s.Type)
	}
	if s.Format != "" {
		t.Errorf("expected no format (int64/uint64 is not a valid string format), got %q", s.Format)
	}
	if s.Pattern != wantPattern {
		t.Errorf("expected pattern %q, got %q", wantPattern, s.Pattern)
	}
	if s.Minimum != nil || s.Maximum != 0 {
		t.Errorf("expected no numeric minimum/maximum on a string schema, got min=%v max=%v", s.Minimum, s.Maximum)
	}
	if s.ExclusiveMinimum || s.ExclusiveMaximum {
		t.Errorf("expected no exclusive bounds on a string schema")
	}
}

func TestStringInt_ScalarTypes_DefaultPatterns(t *testing.T) {
	cases := []struct {
		name        string
		fieldType   descriptorpb.FieldDescriptorProto_Type
		wantPattern string
	}{
		{"uint64", descriptorpb.FieldDescriptorProto_TYPE_UINT64, "^[0-9]+$"},
		{"fixed64", descriptorpb.FieldDescriptorProto_TYPE_FIXED64, "^[0-9]+$"},
		{"int64", descriptorpb.FieldDescriptorProto_TYPE_INT64, "^-?[0-9]+$"},
		{"sint64", descriptorpb.FieldDescriptorProto_TYPE_SINT64, "^-?[0-9]+$"},
		{"sfixed64", descriptorpb.FieldDescriptorProto_TYPE_SFIXED64, "^-?[0-9]+$"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			field := makeFieldWithExtension("n", tc.fieldType, nil)
			withRefs, plain := inlineSchemasBothSwitches(t, field)
			assertStringIntSchema(t, withRefs, tc.wantPattern)
			assertStringIntSchema(t, plain, tc.wantPattern)
		})
	}
}

func TestStringInt_StrayNumericBoundsDoNotLeak(t *testing.T) {
	field := makeFieldWithExtension("n", descriptorpb.FieldDescriptorProto_TYPE_UINT64, &options.JSONSchema{
		Minimum: proto.Float64(5),
		Maximum: 100,
	})
	withRefs, plain := inlineSchemasBothSwitches(t, field)
	for _, s := range []*OpenAPIV3Schema{withRefs, plain} {
		if s.Minimum != nil || s.Maximum != 0 {
			t.Errorf("stray numeric bounds leaked onto string schema: min=%v max=%v", s.Minimum, s.Maximum)
		}
		if s.Type != "string" {
			t.Errorf("expected type=string, got %q", s.Type)
		}
	}
}

func TestStringInt_StringOverridesHonored(t *testing.T) {
	field := makeFieldWithExtension("n", descriptorpb.FieldDescriptorProto_TYPE_UINT64, &options.JSONSchema{
		MinLength: 2,
		MaxLength: 20,
		Pattern:   "^[1-9][0-9]*$",
		Format:    "custom",
	})
	withRefs, plain := inlineSchemasBothSwitches(t, field)
	for _, s := range []*OpenAPIV3Schema{withRefs, plain} {
		if s.Type != "string" {
			t.Errorf("expected type=string, got %q", s.Type)
		}
		if derefMinLength(s.MinLength) != 2 || s.MaxLength != 20 {
			t.Errorf("expected minLength=2 maxLength=20, got %d/%d", derefMinLength(s.MinLength), s.MaxLength)
		}
		if s.Pattern != "^[1-9][0-9]*$" {
			t.Errorf("expected override pattern to win, got %q", s.Pattern)
		}
		if s.Format != "custom" {
			t.Errorf("expected honored format override %q, got %q", "custom", s.Format)
		}
	}
}

func TestStringInt_InvalidFormatOverrideDropped(t *testing.T) {
	// A proto author may explicitly annotate format: uint64 on a 64-bit field.
	// That is an integer format and is invalid on a string schema, so it must
	// be dropped rather than honored (otherwise it re-trips ibm-schema-type-format).
	field := makeFieldWithExtension("n", descriptorpb.FieldDescriptorProto_TYPE_UINT64, &options.JSONSchema{
		Format: "uint64",
	})
	withRefs, plain := inlineSchemasBothSwitches(t, field)
	for _, s := range []*OpenAPIV3Schema{withRefs, plain} {
		if s.Format != "" {
			t.Errorf("expected invalid format override to be dropped, got %q", s.Format)
		}
		if s.Type != "string" {
			t.Errorf("expected type=string, got %q", s.Type)
		}
	}
}

func TestStringInt_Wrappers(t *testing.T) {
	cases := []struct {
		name        string
		typeName    string
		wantPattern string
	}{
		{"Int64Value", ".google.protobuf.Int64Value", "^-?[0-9]+$"},
		{"UInt64Value", ".google.protobuf.UInt64Value", "^[0-9]+$"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			field := makeWrapperField("n", tc.typeName, nil)
			withRefs, plain := inlineSchemasBothSwitches(t, field)
			assertStringIntSchema(t, withRefs, tc.wantPattern)
			assertStringIntSchema(t, plain, tc.wantPattern)
		})
	}
}

func TestGoogleTypeDecimal_RendersAsObject(t *testing.T) {
	field := makeWrapperField("cx_quota_units", ".google.type.Decimal", nil)
	withRefs, plain := inlineSchemasBothSwitches(t, field)
	for name, s := range map[string]*OpenAPIV3Schema{"withRefs": withRefs, "plain": plain} {
		t.Run(name, func(t *testing.T) {
			if s.Type != "object" {
				t.Fatalf("expected google.type.Decimal to render as type=object, got %q", s.Type)
			}
			value := s.Properties["value"]
			if value == nil || value.OpenAPIV3Schema == nil {
				t.Fatal("expected google.type.Decimal schema to contain a value property")
			}
			if value.Type != "string" {
				t.Errorf("expected value property type=string, got %q", value.Type)
			}
			if value.Description != "Decimal value encoded as a string, using optional sign, fraction, and exponent notation." {
				t.Errorf("expected value property description, got %q", value.Description)
			}
			if derefMinLength(value.MinLength) != 0 {
				t.Errorf("expected value property minLength=0, got %d", derefMinLength(value.MinLength))
			}
			if len(s.Required) != 0 {
				t.Errorf("expected no required fields for google.type.Decimal, got %v", s.Required)
			}
		})
	}
}

func TestGoogleTypeDecimal_AppliesStringOptionsToValue(t *testing.T) {
	field := makeWrapperField("cx_quota_units", ".google.type.Decimal", &options.JSONSchema{
		Pattern:   "^-?[0-9]+(\\.[0-9]+)?$",
		Format:    "decimal",
		MinLength: 1,
		MaxLength: 32,
	})
	withRefs, plain := inlineSchemasBothSwitches(t, field)
	for name, s := range map[string]*OpenAPIV3Schema{"withRefs": withRefs, "plain": plain} {
		t.Run(name, func(t *testing.T) {
			if s.Type != "object" {
				t.Fatalf("expected google.type.Decimal to render as type=object, got %q", s.Type)
			}
			if s.Pattern != "" || s.Format != "" || s.MaxLength != 0 || s.MinLength != nil {
				t.Fatalf("expected string options to stay off Decimal object, got pattern=%q format=%q maxLength=%d minLength=%v",
					s.Pattern, s.Format, s.MaxLength, s.MinLength)
			}
			value := s.Properties["value"]
			if value == nil || value.OpenAPIV3Schema == nil {
				t.Fatal("expected google.type.Decimal schema to contain a value property")
			}
			if value.Pattern != "^-?[0-9]+(\\.[0-9]+)?$" {
				t.Errorf("expected value pattern override, got %q", value.Pattern)
			}
			if value.Format != "decimal" {
				t.Errorf("expected value format override, got %q", value.Format)
			}
			if value.MaxLength != 32 || derefMinLength(value.MinLength) != 1 {
				t.Errorf("expected value minLength=1 maxLength=32, got %d/%d", derefMinLength(value.MinLength), value.MaxLength)
			}
		})
	}
}

func TestGoogleTypeDecimal_ObjectExamplePreserved(t *testing.T) {
	field := makeWrapperField("cx_quota_units", ".google.type.Decimal", &options.JSONSchema{
		Example: `{"value":"1.5"}`,
	})
	withRefs, plain := inlineSchemasBothSwitches(t, field)
	for name, s := range map[string]*OpenAPIV3Schema{"withRefs": withRefs, "plain": plain} {
		t.Run(name, func(t *testing.T) {
			if got := string(s.Example); got != `{"value":"1.5"}` {
				t.Errorf("expected Decimal object example to be preserved, got %q", got)
			}
		})
	}
}

func TestStringInt_TopLevelWrapperResponse_Cleaned(t *testing.T) {
	// A top-level Int64Value/UInt64Value RPC response is emitted straight from
	// the well-known map (bypassing the field switch); cleanWellKnownStringInt
	// applies the same cleanup: drop the invalid format, add pattern + lengths.
	for _, tc := range []struct {
		fqmn        string
		wantPattern string
	}{
		{".google.protobuf.Int64Value", "^-?[0-9]+$"},
		{".google.protobuf.UInt64Value", "^[0-9]+$"},
	} {
		t.Run(tc.fqmn, func(t *testing.T) {
			s := cleanWellKnownStringInt(wellKnownTypesToOpenAPIV3SchemaMapping[tc.fqmn])
			if s.Type != "string" || s.Format != "" {
				t.Errorf("expected {string, no format}, got {%q,%q}", s.Type, s.Format)
			}
			if s.Pattern != tc.wantPattern {
				t.Errorf("expected pattern %q, got %q", tc.wantPattern, s.Pattern)
			}
			if derefMinLength(s.MinLength) != 1 || s.MaxLength != 20 {
				t.Errorf("expected minLength=1 maxLength=20, got %d/%d", derefMinLength(s.MinLength), s.MaxLength)
			}
		})
	}
	// Must not mutate the shared map entry.
	if wellKnownTypesToOpenAPIV3SchemaMapping[".google.protobuf.Int64Value"].Format != "int64" {
		t.Error("cleanWellKnownStringInt mutated the shared well-known map entry")
	}
	// A non-string-int well-known schema is returned unchanged (same pointer).
	ts := wellKnownTypesToOpenAPIV3SchemaMapping[".google.protobuf.Timestamp"]
	if cleanWellKnownStringInt(ts) != ts {
		t.Error("expected non-string-int well-known schema to be returned unchanged")
	}
}

func TestStringInt_RepeatedUint64_ArrayOfStrings(t *testing.T) {
	field := makeRepeatedFieldWithExtension("ids", descriptorpb.FieldDescriptorProto_TYPE_UINT64, &options.JSONSchema{})
	reg := descriptor.NewRegistry()
	schema := buildPropertySchemaWithReferencesFromField(field, reg, map[string]string{})
	if schema == nil || schema.Type != "array" {
		t.Fatalf("expected array schema, got %+v", schema)
	}
	if schema.Items == nil || schema.Items.OpenAPIV3Schema == nil {
		t.Fatal("expected array items schema")
	}
	item := schema.Items.OpenAPIV3Schema
	assertStringIntSchema(t, item, "^[0-9]+$")
}

func TestStringInt_RepeatedNumericArrayExample_Coerced(t *testing.T) {
	// A repeated 64-bit int with a numeric array example must emit a string
	// array to match the type: string items (else oas3-valid-schema-example).
	for _, tc := range []struct {
		fieldType descriptorpb.FieldDescriptorProto_Type
		example   string
		want      string
	}{
		{descriptorpb.FieldDescriptorProto_TYPE_UINT64, "[1, 2]", `["1","2"]`},
		{descriptorpb.FieldDescriptorProto_TYPE_INT64, "[-3,4]", `["-3","4"]`},
	} {
		field := makeRepeatedFieldWithExtension("ids", tc.fieldType, &options.JSONSchema{Example: tc.example})
		schema := buildPropertySchemaWithReferencesFromField(field, descriptor.NewRegistry(), map[string]string{})
		if schema == nil || schema.Type != "array" {
			t.Fatalf("expected array schema, got %+v", schema)
		}
		if got := string(schema.Example); got != tc.want {
			t.Errorf("%v: expected array example %s, got %q", tc.fieldType, tc.want, got)
		}
	}
}

func TestStringInt_ScalarExampleNormalized(t *testing.T) {
	for _, tc := range []struct {
		name      string
		fieldType descriptorpb.FieldDescriptorProto_Type
		example   string
		want      string // "" => no example expected
	}{
		{"zero-fraction normalized", descriptorpb.FieldDescriptorProto_TYPE_INT64, "99.00", `"99"`},
		{"quoted int", descriptorpb.FieldDescriptorProto_TYPE_UINT64, "\"42\"", `"42"`},
		{"non-integer dropped", descriptorpb.FieldDescriptorProto_TYPE_UINT64, "3.14", ""},
		{"large uint64 precision", descriptorpb.FieldDescriptorProto_TYPE_UINT64, "18446744073709551615", `"18446744073709551615"`},
		{"negative on unsigned dropped", descriptorpb.FieldDescriptorProto_TYPE_UINT64, "-1", ""},
		{"negative on signed kept", descriptorpb.FieldDescriptorProto_TYPE_INT64, "-5", `"-5"`},
		{"oversized uint64 dropped", descriptorpb.FieldDescriptorProto_TYPE_UINT64, "123456789012345678901", ""},
		{"int64 max kept", descriptorpb.FieldDescriptorProto_TYPE_INT64, "9223372036854775807", `"9223372036854775807"`},
		{"int64 overflow dropped", descriptorpb.FieldDescriptorProto_TYPE_INT64, "9223372036854775808", ""},
		{"uint64 max on signed dropped", descriptorpb.FieldDescriptorProto_TYPE_INT64, "18446744073709551615", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			field := makeFieldWithExtension("n", tc.fieldType, &options.JSONSchema{Example: tc.example})
			withRefs, plain := inlineSchemasBothSwitches(t, field)
			for _, s := range []*OpenAPIV3Schema{withRefs, plain} {
				got := string(s.Example)
				if tc.want == "" && s.Example != nil {
					t.Errorf("expected no example, got %q", got)
				}
				if tc.want != "" && got != tc.want {
					t.Errorf("expected example %s, got %q", tc.want, got)
				}
			}
		})
	}
}

func TestStringInt_RepeatedWrapperNumericArrayExample_Coerced(t *testing.T) {
	// Repeated Int64Value/UInt64Value with a numeric array example must also
	// coerce to a string array to match the type: string items.
	repeated := descriptorpb.FieldDescriptorProto_LABEL_REPEATED
	msgType := descriptorpb.FieldDescriptorProto_TYPE_MESSAGE
	opts := &descriptorpb.FieldOptions{}
	proto.SetExtension(opts, options.E_Openapiv3Field, &options.JSONSchema{Example: "[1, 2]"})
	field := &descriptor.Field{
		FieldDescriptorProto: &descriptorpb.FieldDescriptorProto{
			Name:     proto.String("ids"),
			Label:    &repeated,
			Type:     &msgType,
			TypeName: proto.String(".google.protobuf.UInt64Value"),
			Options:  opts,
		},
	}
	schema := buildPropertySchemaWithReferencesFromField(field, descriptor.NewRegistry(), map[string]string{})
	if schema == nil || schema.Type != "array" {
		t.Fatalf("expected array schema, got %+v", schema)
	}
	if got := string(schema.Example); got != `["1","2"]` {
		t.Errorf("expected array example [\"1\",\"2\"], got %q", got)
	}
}

// --- regressions: 32-bit ints are unchanged ---

func TestStringInt_Int64ScalarBecomesString(t *testing.T) {
	// proto3 JSON encodes scalar int64 as a decimal string, so it renders as a
	// signed-pattern string schema just like the Int64Value wrapper.
	field := makeFieldWithExtension("n", descriptorpb.FieldDescriptorProto_TYPE_INT64, nil)
	withRefs, plain := inlineSchemasBothSwitches(t, field)
	assertStringIntSchema(t, withRefs, "^-?[0-9]+$")
	assertStringIntSchema(t, plain, "^-?[0-9]+$")
}

func TestStringInt_DefaultLengthBounds(t *testing.T) {
	// A 64-bit int renders as type: string, so it must carry minLength/maxLength
	// to satisfy ibm-string-attributes. With no annotation we fabricate 1..20.
	field := makeFieldWithExtension("n", descriptorpb.FieldDescriptorProto_TYPE_UINT64, nil)
	withRefs, plain := inlineSchemasBothSwitches(t, field)
	for _, s := range []*OpenAPIV3Schema{withRefs, plain} {
		if derefMinLength(s.MinLength) != 1 || s.MaxLength != 20 {
			t.Errorf("expected default minLength=1 maxLength=20, got %d/%d", derefMinLength(s.MinLength), s.MaxLength)
		}
	}
}

func TestStringInt_LengthOverridesHonored(t *testing.T) {
	field := makeFieldWithExtension("n", descriptorpb.FieldDescriptorProto_TYPE_UINT64, &options.JSONSchema{
		MinLength: 3,
		MaxLength: 12,
	})
	withRefs, plain := inlineSchemasBothSwitches(t, field)
	for _, s := range []*OpenAPIV3Schema{withRefs, plain} {
		if derefMinLength(s.MinLength) != 3 || s.MaxLength != 12 {
			t.Errorf("expected override minLength=3 maxLength=12, got %d/%d", derefMinLength(s.MinLength), s.MaxLength)
		}
	}
}

func TestStringInt_Int32AndUint32Unchanged(t *testing.T) {
	cases := []struct {
		name       string
		fieldType  descriptorpb.FieldDescriptorProto_Type
		wantFormat string
	}{
		{"int32", descriptorpb.FieldDescriptorProto_TYPE_INT32, "int32"},
		{"uint32", descriptorpb.FieldDescriptorProto_TYPE_UINT32, "int64"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			field := makeFieldWithExtension("n", tc.fieldType, &options.JSONSchema{Maximum: 50})
			withRefs, plain := inlineSchemasBothSwitches(t, field)
			for _, s := range []*OpenAPIV3Schema{withRefs, plain} {
				if s.Type != "integer" {
					t.Errorf("%s: expected type=integer, got %q", tc.name, s.Type)
				}
				if s.Format != tc.wantFormat {
					t.Errorf("%s: expected format=%q, got %q", tc.name, tc.wantFormat, s.Format)
				}
				if s.Maximum != 50 {
					t.Errorf("%s: expected numeric maximum=50 intact, got %v", tc.name, s.Maximum)
				}
			}
		})
	}
}

// --- minItems:0 (arrays) and minimum:0 (unsigned ints) ---
// proto3 cannot serialize an explicit 0 (zero-value scalar), and the schema
// struct fields used omitempty, so a deliberate minItems:0 / minimum:0 was
// dropped. MinItems/Minimum are now pointers and these defaults are emitted.
// Partially unblocks ibm-array-attributes / ibm-integer-attributes (the
// maxItems/maximum halves still can't be fabricated).

// wantMinItems asserts an array schema emits minItems with the given value.
func wantMinItems(t *testing.T, got *uint64, want uint64) {
	t.Helper()
	if got == nil {
		t.Fatalf("expected minItems=%d to be emitted, got nil", want)
	}
	if *got != want {
		t.Errorf("expected minItems=%d, got %d", want, *got)
	}
}

func mustInlinePlain(t *testing.T, field *descriptor.Field) *OpenAPIV3Schema {
	t.Helper()
	ref, _ := buildPropertySchemaFromFieldType(field, map[string]*OpenAPIV3SchemaRef{}, map[string]string{}, descriptor.NewRegistry())
	if ref == nil || ref.OpenAPIV3Schema == nil {
		t.Fatal("expected non-nil inline schema")
	}
	return ref.OpenAPIV3Schema
}

func mapSchemasFromBothSwitches(t *testing.T, field *descriptor.Field, reg *descriptor.Registry) map[string]*OpenAPIV3Schema {
	t.Helper()
	withRefs := buildPropertySchemaWithReferencesFromField(field, reg, map[string]string{})
	plain := buildPropertySchemaFromField(field, map[string]*OpenAPIV3SchemaRef{}, map[string]string{}, reg)
	for name, ref := range map[string]*OpenAPIV3SchemaRef{"withRefs": withRefs, "plain": plain} {
		if ref == nil || ref.OpenAPIV3Schema == nil {
			t.Fatalf("%s: expected non-nil inline schema", name)
		}
	}
	return map[string]*OpenAPIV3Schema{
		"withRefs": withRefs.OpenAPIV3Schema,
		"plain":    plain.OpenAPIV3Schema,
	}
}

func mustAdditionalPropertiesSchema(t *testing.T, schema *OpenAPIV3Schema) *OpenAPIV3Schema {
	t.Helper()
	ref, ok := schema.AdditionalProperties.(*OpenAPIV3SchemaRef)
	if !ok || ref == nil || ref.OpenAPIV3Schema == nil {
		t.Fatalf("expected additionalProperties schema ref, got %#v", schema.AdditionalProperties)
	}
	return ref.OpenAPIV3Schema
}

func TestApplyValueSchema_GenericMetadata(t *testing.T) {
	base := &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{Type: "boolean"}}

	got := applyValueSchemaForMapValue(base, &options.JSONSchema{
		Title:       "Map value",
		Description: "Value metadata.",
		ReadOnly:    true,
		Example:     "true",
	}, makeFieldWithExtension("value", descriptorpb.FieldDescriptorProto_TYPE_BOOL, nil), "")

	if base.Title != "" || base.Description != "" {
		t.Fatal("applyValueSchema must copy the base schema instead of mutating it")
	}
	s := got.OpenAPIV3Schema
	if s.Title != "Map value" || s.Description != "Value metadata." {
		t.Fatalf("expected title/description overrides, got %q/%q", s.Title, s.Description)
	}
	if !s.ReadOnly {
		t.Fatal("expected readOnly=true")
	}
	if string(s.Example) != "true" {
		t.Fatalf("expected example=true, got %s", s.Example)
	}
}

func TestApplyValueSchema_NumericConstraints(t *testing.T) {
	got := applyValueSchemaForMapValue(&OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
		Type:   "integer",
		Format: "int32",
	}}, &options.JSONSchema{
		MultipleOf:       2,
		Maximum:          100,
		Minimum:          proto.Float64(0),
		ExclusiveMaximum: true,
		ExclusiveMinimum: true,
		MaxLength:        99,
		Pattern:          "^[0-9]+$",
		MaxItems:         9,
		MaxProperties:    7,
		Format:           "float",
		Enum:             []string{"1", "2"},
	}, makeFieldWithExtension("value", descriptorpb.FieldDescriptorProto_TYPE_INT32, nil), "")

	s := got.OpenAPIV3Schema
	if s.MultipleOf != 2 || s.Maximum != 100 {
		t.Fatalf("expected numeric multipleOf/maximum, got %v/%v", s.MultipleOf, s.Maximum)
	}
	if s.Minimum == nil || *s.Minimum != 0 {
		t.Fatalf("expected numeric minimum=0, got %v", s.Minimum)
	}
	if !s.ExclusiveMaximum || !s.ExclusiveMinimum {
		t.Fatal("expected exclusive numeric bounds")
	}
	if s.Format != "int32" {
		t.Fatalf("numeric value_schema must not override inferred format, got %q", s.Format)
	}
	if s.MaxLength != 0 || s.Pattern != "" || s.MaxItems != 0 || s.MaxProperties != 0 {
		t.Fatalf("non-numeric constraints leaked onto numeric schema: maxLength=%d pattern=%q maxItems=%d maxProperties=%d",
			s.MaxLength, s.Pattern, s.MaxItems, s.MaxProperties)
	}
	if len(s.Enum) > 0 {
		t.Fatalf("string enum values must not be applied to numeric schema, got %v", s.Enum)
	}
}

func TestApplyValueSchema_NumericMinimumCannotLowerExistingFloor(t *testing.T) {
	got := applyValueSchemaForMapValue(&OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
		Type:    "integer",
		Format:  "int64",
		Minimum: proto.Float64(0),
	}}, &options.JSONSchema{
		Minimum: proto.Float64(-1),
	}, makeFieldWithExtension("value", descriptorpb.FieldDescriptorProto_TYPE_UINT32, nil), "")

	s := got.OpenAPIV3Schema
	if s.Minimum == nil || *s.Minimum != 0 {
		t.Fatalf("value_schema minimum must not lower existing floor, got %v", s.Minimum)
	}
}

func TestApplyValueSchema_NumericMinimumCanRaiseExistingFloor(t *testing.T) {
	got := applyValueSchemaForMapValue(&OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
		Type:    "integer",
		Format:  "int64",
		Minimum: proto.Float64(0),
	}}, &options.JSONSchema{
		Minimum: proto.Float64(5),
	}, makeFieldWithExtension("value", descriptorpb.FieldDescriptorProto_TYPE_UINT32, nil), "")

	s := got.OpenAPIV3Schema
	if s.Minimum == nil || *s.Minimum != 5 {
		t.Fatalf("value_schema minimum should raise existing floor, got %v", s.Minimum)
	}
}

func TestApplyValueSchema_NumericMinimumCanSetSignedFloor(t *testing.T) {
	got := applyValueSchemaForMapValue(&OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
		Type:   "integer",
		Format: "int32",
	}}, &options.JSONSchema{
		Minimum: proto.Float64(-10),
	}, makeFieldWithExtension("value", descriptorpb.FieldDescriptorProto_TYPE_INT32, nil), "")

	s := got.OpenAPIV3Schema
	if s.Minimum == nil || *s.Minimum != -10 {
		t.Fatalf("value_schema minimum should set missing signed floor, got %v", s.Minimum)
	}
}

func TestApplyValueSchema_StringConstraints(t *testing.T) {
	got := applyValueSchemaForMapValue(&OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
		Type:   "string",
		Format: "uuid",
	}}, &options.JSONSchema{
		MaxLength:        64,
		MinLength:        1,
		Pattern:          "^[a-z]+$",
		Format:           "email",
		Example:          "hello",
		Maximum:          100,
		Minimum:          proto.Float64(0),
		ExclusiveMaximum: true,
		MaxItems:         9,
		MaxProperties:    7,
		Enum:             []string{"A", "B"},
	}, makeFieldWithExtension("value", descriptorpb.FieldDescriptorProto_TYPE_STRING, nil), "")

	s := got.OpenAPIV3Schema
	if s.MaxLength != 64 || s.MinLength == nil || *s.MinLength != 1 {
		t.Fatalf("expected string length constraints, got max=%d min=%v", s.MaxLength, s.MinLength)
	}
	if s.Pattern != "^[a-z]+$" || s.Format != "email" {
		t.Fatalf("expected string pattern/format, got %q/%q", s.Pattern, s.Format)
	}
	if !slices.Equal(s.Enum, []string{"A", "B"}) {
		t.Fatalf("expected string enum override, got %v", s.Enum)
	}
	if string(s.Example) != `"hello"` {
		t.Fatalf("expected string example to be coerced to JSON string, got %s", s.Example)
	}
	if _, err := json.Marshal(s); err != nil {
		t.Fatalf("expected coerced string example to marshal as valid JSON: %v", err)
	}
	if s.Maximum != 0 || s.Minimum != nil || s.ExclusiveMaximum || s.MaxItems != 0 || s.MaxProperties != 0 {
		t.Fatalf("non-string constraints leaked onto string schema: max=%v min=%v exclusiveMax=%v maxItems=%d maxProperties=%d",
			s.Maximum, s.Minimum, s.ExclusiveMaximum, s.MaxItems, s.MaxProperties)
	}
}

func TestApplyValueSchema_ArrayConstraints(t *testing.T) {
	got := applyValueSchemaForMapValue(&OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
		Type: "array",
	}}, &options.JSONSchema{
		MaxItems:      10,
		MinItems:      1,
		UniqueItems:   true,
		Maximum:       100,
		MaxLength:     64,
		Pattern:       "^[a-z]+$",
		MaxProperties: 7,
	}, makeFieldWithExtension("value", descriptorpb.FieldDescriptorProto_TYPE_STRING, nil), "")

	s := got.OpenAPIV3Schema
	if s.MaxItems != 10 || s.MinItems == nil || *s.MinItems != 1 {
		t.Fatalf("expected array item constraints, got max=%d min=%v", s.MaxItems, s.MinItems)
	}
	if !s.UniqueItems {
		t.Fatal("expected uniqueItems=true")
	}
	if s.Maximum != 0 || s.MaxLength != 0 || s.Pattern != "" || s.MaxProperties != 0 {
		t.Fatalf("non-array constraints leaked onto array schema: max=%v maxLength=%d pattern=%q maxProperties=%d",
			s.Maximum, s.MaxLength, s.Pattern, s.MaxProperties)
	}
}

func TestApplyValueSchema_ObjectConstraints(t *testing.T) {
	got := applyValueSchemaForMapValue(&OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
		Type: "object",
	}}, &options.JSONSchema{
		MaxProperties: 8,
		MinProperties: 1,
		Maximum:       100,
		MaxLength:     64,
		MaxItems:      10,
	}, makeFieldWithExtension("value", descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, nil), "")

	s := got.OpenAPIV3Schema
	if s.MaxProperties != 8 || s.MinProperties != 1 {
		t.Fatalf("expected object property constraints, got max=%d min=%d", s.MaxProperties, s.MinProperties)
	}
	if s.Maximum != 0 || s.MaxLength != 0 || s.MaxItems != 0 {
		t.Fatalf("non-object constraints leaked onto object schema: max=%v maxLength=%d maxItems=%d",
			s.Maximum, s.MaxLength, s.MaxItems)
	}
}

func TestApplyValueSchema_BooleanIgnoresEnum(t *testing.T) {
	got := applyValueSchemaForMapValue(&OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
		Type: "boolean",
	}}, &options.JSONSchema{
		Enum: []string{"true", "false"},
	}, makeFieldWithExtension("value", descriptorpb.FieldDescriptorProto_TYPE_BOOL, nil), "")

	s := got.OpenAPIV3Schema
	if len(s.Enum) > 0 {
		t.Fatalf("string enum values must not be applied to boolean schema, got %v", s.Enum)
	}
}

func TestApplyValueSchema_RefSchemaWrapsWithAllOf(t *testing.T) {
	got := applyValueSchemaForMapValue(&OpenAPIV3SchemaRef{
		Ref: "#/components/schemas/ValueMsg",
	}, &options.JSONSchema{
		Description:   "Map value object.",
		MaxProperties: 8,
	}, makeFieldWithExtension("value", descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, nil), "object")

	if got.Ref != "" {
		t.Fatalf("expected top-level ref to be cleared for allOf wrapper, got %q", got.Ref)
	}
	s := got.OpenAPIV3Schema
	if s == nil {
		t.Fatal("expected inline wrapper schema")
	}
	if s.Type != "object" {
		t.Fatalf("expected wrapper type=object, got %q", s.Type)
	}
	if len(s.AllOf) != 1 || s.AllOf[0].Ref != "#/components/schemas/ValueMsg" {
		t.Fatalf("expected allOf wrapper around ValueMsg ref, got %#v", s.AllOf)
	}
	if s.Description != "Map value object." || s.MaxProperties != 8 {
		t.Fatalf("expected value_schema overrides on wrapper, got description=%q maxProperties=%d", s.Description, s.MaxProperties)
	}
}

func TestApplyValueSchema_EnumRefSchemaWrapsWithAllOf(t *testing.T) {
	got := applyValueSchemaForMapValue(&OpenAPIV3SchemaRef{
		Ref: "#/components/schemas/ValueEnum",
	}, &options.JSONSchema{
		Description: "Map value enum.",
		Pattern:     "^(VALUE_A|VALUE_B)$",
	}, makeFieldWithExtension("value", descriptorpb.FieldDescriptorProto_TYPE_ENUM, nil), "string")

	if got.Ref != "" {
		t.Fatalf("expected top-level ref to be cleared for allOf wrapper, got %q", got.Ref)
	}
	s := got.OpenAPIV3Schema
	if s == nil {
		t.Fatal("expected inline wrapper schema")
	}
	if s.Type != "string" {
		t.Fatalf("expected wrapper type=string, got %q", s.Type)
	}
	if len(s.AllOf) != 1 || s.AllOf[0].Ref != "#/components/schemas/ValueEnum" {
		t.Fatalf("expected allOf wrapper around ValueEnum ref, got %#v", s.AllOf)
	}
	if s.Description != "Map value enum." || s.Pattern != "^(VALUE_A|VALUE_B)$" {
		t.Fatalf("expected value_schema overrides on wrapper, got description=%q pattern=%q", s.Description, s.Pattern)
	}
}

func TestMinItems_ArrayUnsetEmitsZero(t *testing.T) {
	field := makeRepeatedField("tags", descriptorpb.FieldDescriptorProto_TYPE_STRING)
	reg := descriptor.NewRegistry()
	t.Run("withRefs", func(t *testing.T) {
		s := buildPropertySchemaWithReferencesFromField(field, reg, map[string]string{})
		wantMinItems(t, s.MinItems, 0)
		if s.MaxItems != 0 {
			t.Errorf("maxItems must stay unset (0), got %d", s.MaxItems)
		}
	})
	t.Run("plain", func(t *testing.T) {
		s := buildPropertySchemaFromField(field, map[string]*OpenAPIV3SchemaRef{}, map[string]string{}, reg)
		wantMinItems(t, s.MinItems, 0)
	})
}

func TestMinItems_ArrayExplicitNonZero(t *testing.T) {
	field := makeRepeatedFieldWithExtension("tags", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{
		MinItems: 3,
		MaxItems: 9,
	})
	reg := descriptor.NewRegistry()
	s := buildPropertySchemaWithReferencesFromField(field, reg, map[string]string{})
	wantMinItems(t, s.MinItems, 3)
	if s.MaxItems != 9 {
		t.Errorf("expected maxItems=9, got %d", s.MaxItems)
	}
}

func TestMinimum_Uint32EmitsZeroByDefault(t *testing.T) {
	field := makeSingularFieldWithExtension("count", descriptorpb.FieldDescriptorProto_TYPE_UINT32, &options.JSONSchema{})
	reg := descriptor.NewRegistry()
	withRefs, _ := buildPropertySchemaWithReferencesFromFieldType(field, reg, map[string]string{})
	plain := mustInlinePlain(t, field)
	for name, s := range map[string]*OpenAPIV3Schema{"withRefs": withRefs.OpenAPIV3Schema, "plain": plain} {
		t.Run(name, func(t *testing.T) {
			if s.Type != "integer" {
				t.Fatalf("expected type=integer, got %q", s.Type)
			}
			if s.Minimum == nil || *s.Minimum != 0 {
				t.Errorf("expected minimum=0 to be emitted on an unsigned integer, got %v", s.Minimum)
			}
		})
	}
}

func TestMinimum_Uint32OverrideRaisesFloor(t *testing.T) {
	field := makeSingularFieldWithExtension("count", descriptorpb.FieldDescriptorProto_TYPE_UINT32, &options.JSONSchema{Minimum: proto.Float64(5)})
	s := mustInlinePlain(t, field)
	if s.Minimum == nil || *s.Minimum != 5 {
		t.Errorf("expected minimum=5 from override, got %v", s.Minimum)
	}
}

func TestMinimum_SignedIntNoFabricatedMinimum(t *testing.T) {
	// int64 now renders as type: string, so only int32 remains a signed integer
	// schema; it must not get a fabricated minimum when no minimum annotation is
	// present.
	field := makeSingularFieldWithExtension("n", descriptorpb.FieldDescriptorProto_TYPE_INT32, &options.JSONSchema{})
	s := mustInlinePlain(t, field)
	if s.Minimum != nil {
		t.Errorf("signed int must not get a fabricated minimum, got %v", *s.Minimum)
	}
}

func TestMinimum_SignedIntExplicitZeroSchemaProperty(t *testing.T) {
	field := makeSingularFieldWithExtension("page_offset", descriptorpb.FieldDescriptorProto_TYPE_INT32, &options.JSONSchema{
		Description: "Zero-based page offset.",
		Minimum:     proto.Float64(0),
		Maximum:     10000,
	})
	reg := descriptor.NewRegistry()
	withRefs, _ := buildPropertySchemaWithReferencesFromFieldType(field, reg, map[string]string{})
	plain := mustInlinePlain(t, field)

	for name, s := range map[string]*OpenAPIV3Schema{"withRefs": withRefs.OpenAPIV3Schema, "plain": plain} {
		t.Run(name, func(t *testing.T) {
			if s.Type != "integer" || s.Format != "int32" {
				t.Fatalf("expected int32 integer schema, got type=%q format=%q", s.Type, s.Format)
			}
			if s.Minimum == nil || *s.Minimum != 0 {
				t.Fatalf("expected explicit minimum=0 to be emitted, got %v", s.Minimum)
			}
			if s.Maximum != 10000 {
				t.Errorf("expected maximum=10000, got %v", s.Maximum)
			}
		})
	}
}

func TestMinimum_SignedIntExplicitZeroQueryParameter(t *testing.T) {
	opts := &descriptorpb.FieldOptions{}
	proto.SetExtension(opts, options.E_Openapiv3Field, &options.JSONSchema{
		Description: "Zero-based page offset.",
		Minimum:     proto.Float64(0),
		Maximum:     10000,
	})
	fieldType := descriptorpb.FieldDescriptorProto_TYPE_INT32
	label := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	binding, reg := newQueryParamFixtureWithFields(t, &descriptorpb.FieldDescriptorProto{
		Name:    proto.String("page_offset"),
		Number:  proto.Int32(1),
		Type:    &fieldType,
		Label:   &label,
		Options: opts,
	})

	params := buildQueryParameters(binding, map[string]*OpenAPIV3SchemaRef{}, map[string]string{}, reg)
	if len(params) != 1 {
		t.Fatalf("expected 1 query parameter, got %d", len(params))
	}
	param := params[0]
	if param.Name != "page_offset" || param.In != "query" {
		t.Fatalf("unexpected parameter: name=%q in=%q", param.Name, param.In)
	}
	if param.Schema == nil || param.Schema.OpenAPIV3Schema == nil {
		t.Fatal("expected inline query parameter schema")
	}
	s := param.Schema.OpenAPIV3Schema
	if s.Type != "integer" || s.Format != "int32" {
		t.Fatalf("expected int32 integer schema, got type=%q format=%q", s.Type, s.Format)
	}
	if s.Minimum == nil || *s.Minimum != 0 {
		t.Fatalf("expected explicit minimum=0 to be emitted, got %v", s.Minimum)
	}
	if s.Maximum != 10000 {
		t.Errorf("expected maximum=10000, got %v", s.Maximum)
	}
}

func TestMapValueSchema_Int32MapValueConstraints(t *testing.T) {
	field, reg := makeMapFieldWithExtension(t, "counts", descriptorpb.FieldDescriptorProto_TYPE_INT32, &options.JSONSchema{
		Description: "Map of names to counts.",
		ValueSchema: &options.JSONSchema{
			Description: "Count value.",
			Minimum:     proto.Float64(0),
			Maximum:     10000,
		},
	})

	for name, s := range mapSchemasFromBothSwitches(t, field, reg) {
		t.Run(name, func(t *testing.T) {
			if s.Type != "object" {
				t.Fatalf("expected map object schema, got type=%q", s.Type)
			}
			if s.Description != "Map of names to counts." {
				t.Errorf("expected outer map description, got %q", s.Description)
			}
			value := mustAdditionalPropertiesSchema(t, s)
			if value.Type != "integer" || value.Format != "int32" {
				t.Fatalf("expected int32 map value schema, got type=%q format=%q", value.Type, value.Format)
			}
			if value.Description != "Count value." {
				t.Errorf("expected value schema description, got %q", value.Description)
			}
			if value.Minimum == nil || *value.Minimum != 0 {
				t.Fatalf("expected value minimum=0, got %v", value.Minimum)
			}
			if value.Maximum != 10000 {
				t.Errorf("expected value maximum=10000, got %v", value.Maximum)
			}
		})
	}
}

func TestMapValueSchema_Uint32MapValueConstraints(t *testing.T) {
	field, reg := makeMapFieldWithExtension(t, "counts", descriptorpb.FieldDescriptorProto_TYPE_UINT32, &options.JSONSchema{
		Description: "Map of names to counts.",
		ValueSchema: &options.JSONSchema{
			Maximum: 10000,
		},
	})

	for name, s := range mapSchemasFromBothSwitches(t, field, reg) {
		t.Run(name, func(t *testing.T) {
			value := mustAdditionalPropertiesSchema(t, s)
			if value.Type != "integer" || value.Format != "int64" {
				t.Fatalf("expected uint32 map value schema, got type=%q format=%q", value.Type, value.Format)
			}
			if value.Minimum == nil || *value.Minimum != 0 {
				t.Fatalf("expected unsigned map value minimum=0, got %v", value.Minimum)
			}
			if value.Maximum != 10000 {
				t.Errorf("expected value maximum=10000, got %v", value.Maximum)
			}
		})
	}
}

func TestMapValueSchema_Uint32MapValueNegativeMinimumClamped(t *testing.T) {
	field, reg := makeMapFieldWithExtension(t, "counts", descriptorpb.FieldDescriptorProto_TYPE_UINT32, &options.JSONSchema{
		Description: "Map of names to counts.",
		ValueSchema: &options.JSONSchema{
			Minimum: proto.Float64(-1),
			Maximum: 10000,
		},
	})

	for name, s := range mapSchemasFromBothSwitches(t, field, reg) {
		t.Run(name, func(t *testing.T) {
			value := mustAdditionalPropertiesSchema(t, s)
			if value.Type != "integer" || value.Format != "int64" {
				t.Fatalf("expected uint32 map value schema, got type=%q format=%q", value.Type, value.Format)
			}
			if value.Minimum == nil || *value.Minimum != 0 {
				t.Fatalf("negative minimum must not lower unsigned map value floor, got %v", value.Minimum)
			}
			if value.Maximum != 10000 {
				t.Errorf("expected value maximum=10000, got %v", value.Maximum)
			}
		})
	}
}

func TestMapValueSchema_UInt32ValueMapValueNegativeMinimumClamped(t *testing.T) {
	field, reg := makeWellKnownMapFieldWithExtension(t, "counts", ".google.protobuf.UInt32Value", protodesc.ToFileDescriptorProto(wrapperspb.File_google_protobuf_wrappers_proto), &options.JSONSchema{
		Description: "Map of names to counts.",
		ValueSchema: &options.JSONSchema{
			Minimum: proto.Float64(-1),
			Maximum: 10000,
		},
	})

	for name, s := range mapSchemasFromBothSwitches(t, field, reg) {
		t.Run(name, func(t *testing.T) {
			value := mustAdditionalPropertiesSchema(t, s)
			if value.Type != "integer" || value.Format != "int64" {
				t.Fatalf("expected UInt32Value map value schema, got type=%q format=%q", value.Type, value.Format)
			}
			if value.Minimum == nil || *value.Minimum != 0 {
				t.Fatalf("negative minimum must not lower UInt32Value map value floor, got %v", value.Minimum)
			}
			if value.Maximum != 10000 {
				t.Errorf("expected value maximum=10000, got %v", value.Maximum)
			}
		})
	}
}

func TestMapValueSchema_MessageMapValueConstraints(t *testing.T) {
	field, reg, resolvedNames, schemaMap := makeMessageMapFieldWithExtension(t, "values", &options.JSONSchema{
		Description: "Map of names to values.",
		ValueSchema: &options.JSONSchema{
			Description:   "Value object.",
			MaxProperties: 8,
		},
	})

	withRefs := buildPropertySchemaWithReferencesFromField(field, reg, resolvedNames)
	plain := buildPropertySchemaFromField(field, schemaMap, resolvedNames, reg)

	for name, s := range map[string]*OpenAPIV3Schema{
		"withRefs": withRefs.OpenAPIV3Schema,
		"plain":    plain.OpenAPIV3Schema,
	} {
		t.Run(name, func(t *testing.T) {
			if s.Type != "object" {
				t.Fatalf("expected map object schema, got type=%q", s.Type)
			}
			if s.Description != "Map of names to values." {
				t.Errorf("expected outer map description, got %q", s.Description)
			}
			value := mustAdditionalPropertiesSchema(t, s)
			if value.Type != "object" {
				t.Fatalf("expected object map value schema, got type=%q", value.Type)
			}
			if value.Description != "Value object." || value.MaxProperties != 8 {
				t.Fatalf("expected value_schema overrides, got description=%q maxProperties=%d", value.Description, value.MaxProperties)
			}
			if name == "withRefs" {
				if len(value.AllOf) != 1 || value.AllOf[0].Ref != "#/components/schemas/ValueMsg" {
					t.Fatalf("expected referenced map value to be wrapped with allOf, got %#v", value.AllOf)
				}
			}
			if _, err := json.Marshal(s); err != nil {
				t.Fatalf("expected map schema with message value constraints to marshal as valid JSON: %v", err)
			}
		})
	}
}

func TestMapValueSchema_StringMapValueExampleCoerced(t *testing.T) {
	field, reg := makeMapFieldWithExtension(t, "labels", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{
		Description: "Map of label names to label values.",
		ValueSchema: &options.JSONSchema{
			Example: "hello",
		},
	})

	for name, s := range mapSchemasFromBothSwitches(t, field, reg) {
		t.Run(name, func(t *testing.T) {
			value := mustAdditionalPropertiesSchema(t, s)
			if value.Type != "string" {
				t.Fatalf("expected string map value schema, got type=%q", value.Type)
			}
			if string(value.Example) != `"hello"` {
				t.Fatalf("expected string map value example to be coerced to JSON string, got %s", value.Example)
			}
			if _, err := json.Marshal(s); err != nil {
				t.Fatalf("expected map schema with string value example to marshal as valid JSON: %v", err)
			}
		})
	}
}

func TestMapValueSchema_StringMapValueFormatOverride(t *testing.T) {
	field, reg := makeMapFieldWithExtension(t, "labels", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{
		Description: "Map of label names to label values.",
		ValueSchema: &options.JSONSchema{
			Format: "uuid",
		},
	})

	for name, s := range mapSchemasFromBothSwitches(t, field, reg) {
		t.Run(name, func(t *testing.T) {
			value := mustAdditionalPropertiesSchema(t, s)
			if value.Type != "string" {
				t.Fatalf("expected string map value schema, got type=%q", value.Type)
			}
			if value.Format != "uuid" {
				t.Fatalf("expected plain string map value format override, got %q", value.Format)
			}
		})
	}
}

func TestMapValueSchema_BytesMapValuePreservesFormat(t *testing.T) {
	field, reg := makeMapFieldWithExtension(t, "blobs", descriptorpb.FieldDescriptorProto_TYPE_BYTES, &options.JSONSchema{
		Description: "Map of names to blobs.",
		ValueSchema: &options.JSONSchema{
			Format: "uuid",
		},
	})

	for name, s := range mapSchemasFromBothSwitches(t, field, reg) {
		t.Run(name, func(t *testing.T) {
			value := mustAdditionalPropertiesSchema(t, s)
			if value.Type != "string" {
				t.Fatalf("expected bytes map value schema to render as string, got type=%q", value.Type)
			}
			if value.Format != "byte" {
				t.Fatalf("bytes map value must preserve format=byte, got %q", value.Format)
			}
		})
	}
}

func TestMapValueSchema_TimestampMapValuePreservesFormat(t *testing.T) {
	field, reg := makeWellKnownMapFieldWithExtension(t, "timestamps", ".google.protobuf.Timestamp", protodesc.ToFileDescriptorProto(timestamppb.File_google_protobuf_timestamp_proto), &options.JSONSchema{
		Description: "Map of names to timestamps.",
		ValueSchema: &options.JSONSchema{
			Format: "email",
		},
	})

	for name, s := range mapSchemasFromBothSwitches(t, field, reg) {
		t.Run(name, func(t *testing.T) {
			value := mustAdditionalPropertiesSchema(t, s)
			if value.Type != "string" {
				t.Fatalf("expected timestamp map value schema to render as string, got type=%q", value.Type)
			}
			if value.Format != "date-time" {
				t.Fatalf("timestamp map value must preserve format=date-time, got %q", value.Format)
			}
		})
	}
}

func TestMinimum_UInt32ValueWrapperEmitsZero(t *testing.T) {
	field := makeWrapperField("count", ".google.protobuf.UInt32Value", nil)
	s := mustInlinePlain(t, field)
	if s.Minimum == nil || *s.Minimum != 0 {
		t.Errorf("expected UInt32Value wrapper to emit minimum=0, got %v", s.Minimum)
	}
}

func TestMinimum_TopLevelUInt32ValueResponse_EmitsZero(t *testing.T) {
	// A top-level UInt32Value RPC response is emitted straight from the
	// well-known map (bypassing the field switch); cleanWellKnownResponseSchema
	// must still emit minimum: 0 to match the field-level case.
	mapped := wellKnownTypesToOpenAPIV3SchemaMapping[".google.protobuf.UInt32Value"]
	s := cleanWellKnownResponseSchema(mapped, ".google.protobuf.UInt32Value")
	if s.Type != "integer" {
		t.Fatalf("expected type=integer, got %q", s.Type)
	}
	if s.Minimum == nil || *s.Minimum != 0 {
		t.Errorf("expected minimum=0 on a top-level UInt32Value response, got %v", s.Minimum)
	}
	// Must not mutate the shared map entry.
	if mapped.Minimum != nil {
		t.Error("cleanWellKnownResponseSchema mutated the shared well-known map entry")
	}
}

func TestMinLength_TopLevelStringWrapperResponse_EmitsZero(t *testing.T) {
	// Top-level string-wrapper RPC responses are emitted straight from the
	// well-known map (bypassing the field switch); cleanWellKnownResponseSchema
	// must emit minLength: 0 so they satisfy ibm-string-attributes.
	for _, fqmn := range []string{
		".google.protobuf.StringValue",
		".google.protobuf.BytesValue",
		".google.protobuf.FieldMask",
		".google.protobuf.Timestamp",
		".google.protobuf.Duration",
	} {
		t.Run(fqmn, func(t *testing.T) {
			mapped := wellKnownTypesToOpenAPIV3SchemaMapping[fqmn]
			s := cleanWellKnownResponseSchema(mapped, fqmn)
			if s.Type != "string" {
				t.Fatalf("expected type=string, got %q", s.Type)
			}
			if derefMinLength(s.MinLength) != 0 {
				t.Errorf("expected minLength=0, got %d", derefMinLength(s.MinLength))
			}
			// Must not mutate the shared map entry.
			if mapped.MinLength != nil {
				t.Error("cleanWellKnownResponseSchema mutated the shared well-known map entry")
			}
		})
	}
}

func TestMinZero_MarshalRoundTrip(t *testing.T) {
	// A deliberate minItems:0 / minimum:0 must serialize (the whole point of the
	// pointer change) and survive a marshal -> unmarshal -> marshal round-trip.
	zeroU := uint64(0)
	zeroF := float64(0)
	schema := &OpenAPIV3Schema{Type: "array", MinItems: &zeroU, Minimum: &zeroF}

	b, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"minItems":0`) {
		t.Errorf("expected minItems:0 in output, got %s", b)
	}
	if !strings.Contains(string(b), `"minimum":0`) {
		t.Errorf("expected minimum:0 in output, got %s", b)
	}

	var back OpenAPIV3Schema
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.MinItems == nil || *back.MinItems != 0 {
		t.Errorf("round-trip lost minItems:0, got %v", back.MinItems)
	}
	if back.Minimum == nil || *back.Minimum != 0 {
		t.Errorf("round-trip lost minimum:0, got %v", back.Minimum)
	}
	b2, _ := json.Marshal(&back)
	if string(b) != string(b2) {
		t.Errorf("re-marshal not stable:\n  %s\n  %s", b, b2)
	}
}

func TestMinZero_NilMinItemsOmitted(t *testing.T) {
	// A non-array schema leaves MinItems nil, which must stay omitted.
	schema := &OpenAPIV3Schema{Type: "string"}
	b, _ := json.Marshal(schema)
	if strings.Contains(string(b), "minItems") {
		t.Errorf("expected no minItems for a nil pointer, got %s", b)
	}
	if strings.Contains(string(b), "minimum") {
		t.Errorf("expected no minimum for a nil pointer, got %s", b)
	}
}

func hoistPathParam(name, desc string) OpenAPIV3ParameterRef {
	return OpenAPIV3ParameterRef{OpenAPIV3Parameter: &OpenAPIV3Parameter{
		Name:        name,
		In:          "path",
		Required:    true,
		Description: desc,
		Schema:      &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{Type: "string"}},
	}}
}

func hoistQueryParam(name string) OpenAPIV3ParameterRef {
	return OpenAPIV3ParameterRef{OpenAPIV3Parameter: &OpenAPIV3Parameter{
		Name:   name,
		In:     "query",
		Schema: &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{Type: "string"}},
	}}
}

func paramNames(refs []OpenAPIV3ParameterRef) []string {
	names := make([]string, 0, len(refs))
	for _, r := range refs {
		if r.OpenAPIV3Parameter != nil {
			names = append(names, r.Name)
		}
	}
	return names
}

func TestHoistSharedPathParameters(t *testing.T) {
	t.Run("identical path param across two ops is hoisted", func(t *testing.T) {
		item := &OpenAPIV3PathItem{
			Get:    &OpenAPIV3Operation{Parameters: []OpenAPIV3ParameterRef{hoistPathParam("id", "Resource id.")}},
			Delete: &OpenAPIV3Operation{Parameters: []OpenAPIV3ParameterRef{hoistPathParam("id", "Resource id.")}},
		}
		hoistSharedPathParameters(OpenAPIV3Paths{"/v1/things/{id}": item})

		if got := paramNames(item.Parameters); !slices.Equal(got, []string{"id"}) {
			t.Errorf("path-item parameters = %v, want [id]", got)
		}
		if got := paramNames(item.Get.Parameters); len(got) != 0 {
			t.Errorf("GET parameters = %v, want empty", got)
		}
		if got := paramNames(item.Delete.Parameters); len(got) != 0 {
			t.Errorf("DELETE parameters = %v, want empty", got)
		}
	})

	t.Run("differing path param defs are not hoisted", func(t *testing.T) {
		item := &OpenAPIV3PathItem{
			Get:    &OpenAPIV3Operation{Parameters: []OpenAPIV3ParameterRef{hoistPathParam("id", "Resource id.")}},
			Delete: &OpenAPIV3Operation{Parameters: []OpenAPIV3ParameterRef{hoistPathParam("id", "The id to delete.")}},
		}
		hoistSharedPathParameters(OpenAPIV3Paths{"/v1/things/{id}": item})

		if len(item.Parameters) != 0 {
			t.Errorf("path-item parameters = %v, want empty (defs differ)", paramNames(item.Parameters))
		}
		if got := paramNames(item.Get.Parameters); !slices.Equal(got, []string{"id"}) {
			t.Errorf("GET parameters = %v, want [id]", got)
		}
		if got := paramNames(item.Delete.Parameters); !slices.Equal(got, []string{"id"}) {
			t.Errorf("DELETE parameters = %v, want [id]", got)
		}
	})

	t.Run("single operation is not hoisted", func(t *testing.T) {
		item := &OpenAPIV3PathItem{
			Get: &OpenAPIV3Operation{Parameters: []OpenAPIV3ParameterRef{hoistPathParam("id", "Resource id.")}},
		}
		hoistSharedPathParameters(OpenAPIV3Paths{"/v1/things/{id}": item})

		if len(item.Parameters) != 0 {
			t.Errorf("path-item parameters = %v, want empty (single op)", paramNames(item.Parameters))
		}
		if got := paramNames(item.Get.Parameters); !slices.Equal(got, []string{"id"}) {
			t.Errorf("GET parameters = %v, want [id]", got)
		}
	})

	t.Run("query parameters are never hoisted", func(t *testing.T) {
		item := &OpenAPIV3PathItem{
			Get:  &OpenAPIV3Operation{Parameters: []OpenAPIV3ParameterRef{hoistQueryParam("filter")}},
			Post: &OpenAPIV3Operation{Parameters: []OpenAPIV3ParameterRef{hoistQueryParam("filter")}},
		}
		hoistSharedPathParameters(OpenAPIV3Paths{"/v1/things": item})

		if len(item.Parameters) != 0 {
			t.Errorf("path-item parameters = %v, want empty (query not hoisted)", paramNames(item.Parameters))
		}
		if got := paramNames(item.Get.Parameters); !slices.Equal(got, []string{"filter"}) {
			t.Errorf("GET parameters = %v, want [filter]", got)
		}
	})

	t.Run("only the identical param is hoisted; query and differing params stay", func(t *testing.T) {
		item := &OpenAPIV3PathItem{
			Get: &OpenAPIV3Operation{Parameters: []OpenAPIV3ParameterRef{
				hoistPathParam("id", "Resource id."),
				hoistPathParam("rev", "Read revision."),
				hoistQueryParam("filter"),
			}},
			Put: &OpenAPIV3Operation{Parameters: []OpenAPIV3ParameterRef{
				hoistPathParam("id", "Resource id."),
				hoistPathParam("rev", "Write revision."),
				hoistQueryParam("filter"),
			}},
		}
		hoistSharedPathParameters(OpenAPIV3Paths{"/v1/things/{id}/{rev}": item})

		if got := paramNames(item.Parameters); !slices.Equal(got, []string{"id"}) {
			t.Errorf("path-item parameters = %v, want [id]", got)
		}
		if got := paramNames(item.Get.Parameters); !slices.Equal(got, []string{"rev", "filter"}) {
			t.Errorf("GET parameters = %v, want [rev filter]", got)
		}
		if got := paramNames(item.Put.Parameters); !slices.Equal(got, []string{"rev", "filter"}) {
			t.Errorf("PUT parameters = %v, want [rev filter]", got)
		}
	})

	t.Run("multiple identical path params are hoisted in order", func(t *testing.T) {
		item := &OpenAPIV3PathItem{
			Get: &OpenAPIV3Operation{Parameters: []OpenAPIV3ParameterRef{
				hoistPathParam("x", "X."),
				hoistPathParam("y", "Y."),
			}},
			Delete: &OpenAPIV3Operation{Parameters: []OpenAPIV3ParameterRef{
				hoistPathParam("x", "X."),
				hoistPathParam("y", "Y."),
			}},
		}
		hoistSharedPathParameters(OpenAPIV3Paths{"/v1/a/{x}/b/{y}": item})

		if got := paramNames(item.Parameters); !slices.Equal(got, []string{"x", "y"}) {
			t.Errorf("path-item parameters = %v, want [x y]", got)
		}
		if got := paramNames(item.Get.Parameters); len(got) != 0 {
			t.Errorf("GET parameters = %v, want empty", got)
		}
	})
}
