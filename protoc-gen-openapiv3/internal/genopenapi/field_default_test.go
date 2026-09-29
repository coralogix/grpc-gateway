package genopenapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/internal/descriptor"
	options "github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv3/options"
	"go.yaml.in/yaml/v3"
	"google.golang.org/genproto/googleapis/api/visibility"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

// buildBothVariants runs a field through both property-schema builders, which
// back component schemas (with references) and query parameters (without).
func buildBothVariants(t *testing.T, field *descriptor.Field, reg *descriptor.Registry, resolvedNames map[string]string) map[string]*OpenAPIV3SchemaRef {
	t.Helper()
	return map[string]*OpenAPIV3SchemaRef{
		"with-references":    buildPropertySchemaWithReferencesFromField(field, reg, resolvedNames),
		"without-references": buildPropertySchemaFromField(field, map[string]*OpenAPIV3SchemaRef{}, resolvedNames, reg),
	}
}

// defaultJSON returns the schema's default as compact JSON, or "" when unset.
func defaultJSON(t *testing.T, s *OpenAPIV3SchemaRef) string {
	t.Helper()
	if s == nil || s.OpenAPIV3Schema == nil || s.Default == nil {
		return ""
	}
	b, err := json.Marshal(s.Default)
	if err != nil {
		t.Fatalf("marshal default: %v", err)
	}
	return string(b)
}

func TestFieldDefault_Scalars(t *testing.T) {
	type tc struct {
		name      string
		fieldType descriptorpb.FieldDescriptorProto_Type
		ext       *options.JSONSchema
		want      string // compact JSON; "" means the default is dropped
	}
	cases := []tc{
		{"bool true", descriptorpb.FieldDescriptorProto_TYPE_BOOL, &options.JSONSchema{Default: "true"}, `true`},
		{"bool quoted", descriptorpb.FieldDescriptorProto_TYPE_BOOL, &options.JSONSchema{Default: `"false"`}, `false`},
		{"bool invalid", descriptorpb.FieldDescriptorProto_TYPE_BOOL, &options.JSONSchema{Default: "yes"}, ``},
		{"bool array", descriptorpb.FieldDescriptorProto_TYPE_BOOL, &options.JSONSchema{Default: "[]"}, ``},
		{"int32", descriptorpb.FieldDescriptorProto_TYPE_INT32, &options.JSONSchema{Default: "5"}, `5`},
		{"int32 quoted", descriptorpb.FieldDescriptorProto_TYPE_INT32, &options.JSONSchema{Default: `"-3"`}, `-3`},
		{"int32 fraction", descriptorpb.FieldDescriptorProto_TYPE_INT32, &options.JSONSchema{Default: "1.5"}, ``},
		{"int32 object", descriptorpb.FieldDescriptorProto_TYPE_INT32, &options.JSONSchema{Default: "{}"}, ``},
		{"uint32 zero", descriptorpb.FieldDescriptorProto_TYPE_UINT32, &options.JSONSchema{Default: "0"}, `0`},
		{"uint32 negative violates implicit minimum 0", descriptorpb.FieldDescriptorProto_TYPE_UINT32, &options.JSONSchema{Default: "-1"}, ``},
		{"double", descriptorpb.FieldDescriptorProto_TYPE_DOUBLE, &options.JSONSchema{Default: "1.5"}, `1.5`},
		{"float not a number", descriptorpb.FieldDescriptorProto_TYPE_FLOAT, &options.JSONSchema{Default: "abc"}, ``},
		{"string bare", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{Default: "abc"}, `"abc"`},
		{"string quoted", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{Default: `"abc"`}, `"abc"`},
		{"string that looks like JSON stays a string", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{Default: "[]"}, `"[]"`},
		{"string escaped", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{Default: `a"b`}, `"a\"b"`},
		{"int64 renders as string", descriptorpb.FieldDescriptorProto_TYPE_INT64, &options.JSONSchema{Default: "42"}, `"42"`},
		{"int64 quoted", descriptorpb.FieldDescriptorProto_TYPE_INT64, &options.JSONSchema{Default: `"-42"`}, `"-42"`},
		{"uint64 negative", descriptorpb.FieldDescriptorProto_TYPE_UINT64, &options.JSONSchema{Default: "-1"}, ``},
		{"uint64 non-integer", descriptorpb.FieldDescriptorProto_TYPE_UINT64, &options.JSONSchema{Default: "1.5"}, ``},
		{"int32 above maximum", descriptorpb.FieldDescriptorProto_TYPE_INT32, &options.JSONSchema{Default: "20", Maximum: 10}, ``},
		{"int32 at maximum", descriptorpb.FieldDescriptorProto_TYPE_INT32, &options.JSONSchema{Default: "10", Maximum: 10}, `10`},
		{"int32 at exclusive maximum", descriptorpb.FieldDescriptorProto_TYPE_INT32, &options.JSONSchema{Default: "10", Maximum: 10, ExclusiveMaximum: true}, ``},
		{"int32 below minimum", descriptorpb.FieldDescriptorProto_TYPE_INT32, &options.JSONSchema{Default: "0", Minimum: proto.Float64(1)}, ``},
		{"int32 at exclusive minimum", descriptorpb.FieldDescriptorProto_TYPE_INT32, &options.JSONSchema{Default: "1", Minimum: proto.Float64(1), ExclusiveMinimum: true}, ``},
		{"string within bounds", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{Default: "ab", MinLength: 1, MaxLength: 2, Pattern: "^[a-z]+$"}, `"ab"`},
		{"string above maxLength", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{Default: "abc", MaxLength: 2}, ``},
		{"string maxLength counts runes", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{Default: "éé", MaxLength: 2}, `"éé"`},
		{"string below minLength", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{Default: `""`, MinLength: 1}, ``},
		{"string pattern mismatch", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{Default: "ABC", Pattern: "^[a-z]+$"}, ``},
		{"string non-RE2 pattern is not enforced", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{Default: "abc", Pattern: `^(?!x)[a-z]+$`}, `"abc"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			field := makeSingularFieldWithExtension("f", c.fieldType, c.ext)
			for variant, schema := range buildBothVariants(t, field, descriptor.NewRegistry(), map[string]string{}) {
				if got := defaultJSON(t, schema); got != c.want {
					t.Errorf("%s: default = %s, want %s", variant, orNone(got), orNone(c.want))
				}
			}
		})
	}
}

func orNone(s string) string {
	if s == "" {
		return "<none>"
	}
	return s
}

func TestFieldDefault_NoAnnotationEmitsNoDefault(t *testing.T) {
	field := makeFieldWithExtension("f", descriptorpb.FieldDescriptorProto_TYPE_BOOL, &options.JSONSchema{Description: "d"})
	for variant, schema := range buildBothVariants(t, field, descriptor.NewRegistry(), map[string]string{}) {
		if got := defaultJSON(t, schema); got != "" {
			t.Errorf("%s: expected no default, got %s", variant, got)
		}
	}
}

func TestFieldDefault_WellKnownWrappers(t *testing.T) {
	cases := []struct {
		typeName, def, want string
	}{
		{".google.protobuf.BoolValue", "false", `false`},
		{".google.protobuf.BoolValue", "nope", ``},
		{".google.protobuf.UInt32Value", "0", `0`},
		{".google.protobuf.UInt32Value", "-1", ``},
		{".google.protobuf.Int32Value", "7", `7`},
		{".google.protobuf.Int64Value", "7", `"7"`},
		{".google.protobuf.UInt64Value", "-7", ``},
		{".google.protobuf.StringValue", "x", `"x"`},
		{".google.protobuf.DoubleValue", "0.25", `0.25`},
	}
	for _, c := range cases {
		t.Run(c.typeName+"="+c.def, func(t *testing.T) {
			field := makeWrapperField("f", c.typeName, &options.JSONSchema{Default: c.def})
			for variant, schema := range buildBothVariants(t, field, descriptor.NewRegistry(), map[string]string{}) {
				if got := defaultJSON(t, schema); got != c.want {
					t.Errorf("%s: default = %s, want %s", variant, orNone(got), orNone(c.want))
				}
			}
		})
	}
}

// Setting a default on a wrapper must not leak into the shared well-known-type
// schema used by every other field of that type.
func TestFieldDefault_WrapperDoesNotMutateSharedMapping(t *testing.T) {
	field := makeWrapperField("f", ".google.protobuf.BoolValue", &options.JSONSchema{Default: "true"})
	buildBothVariants(t, field, descriptor.NewRegistry(), map[string]string{})
	if shared := wellKnownTypesToOpenAPIV3SchemaMapping[".google.protobuf.BoolValue"]; shared.Default != nil {
		t.Fatalf("shared BoolValue schema was mutated: default = %v", shared.Default)
	}
}

func TestFieldDefault_RepeatedScalars(t *testing.T) {
	cases := []struct {
		name      string
		fieldType descriptorpb.FieldDescriptorProto_Type
		ext       *options.JSONSchema
		want      string
	}{
		{"empty array", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{Default: "[]"}, `[]`},
		{"string items", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{Default: `["a", "b"]`}, `["a","b"]`},
		{"not an array", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{Default: "a"}, ``},
		{"null", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{Default: "null"}, ``},
		{"wrong item type", descriptorpb.FieldDescriptorProto_TYPE_INT32, &options.JSONSchema{Default: `[1, "x"]`}, ``},
		{"int32 items", descriptorpb.FieldDescriptorProto_TYPE_INT32, &options.JSONSchema{Default: `[1, 2]`}, `[1,2]`},
		{"int64 items become strings", descriptorpb.FieldDescriptorProto_TYPE_INT64, &options.JSONSchema{Default: `[1, "2"]`}, `["1","2"]`},
		{"bool items", descriptorpb.FieldDescriptorProto_TYPE_BOOL, &options.JSONSchema{Default: `[true]`}, `[true]`},
		{"below minItems", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{Default: "[]", MinItems: 1}, ``},
		{"above maxItems", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{Default: `["a","b"]`, MaxItems: 1}, ``},
		{"duplicates with uniqueItems", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{Default: `["a","a"]`, UniqueItems: true}, ``},
		{"distinct with uniqueItems", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{Default: `["a","b"]`, UniqueItems: true}, `["a","b"]`},
		// Item-level string constraints come from the same openapiv3_field annotation.
		{"item violates maxLength", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{Default: `["abc"]`, MaxLength: 2}, ``},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			field := makeRepeatedFieldWithExtension("f", c.fieldType, c.ext)
			for variant, schema := range buildBothVariants(t, field, descriptor.NewRegistry(), map[string]string{}) {
				if schema.Type != "array" {
					t.Fatalf("%s: expected array schema, got %q", variant, schema.Type)
				}
				if got := defaultJSON(t, schema); got != c.want {
					t.Errorf("%s: default = %s, want %s", variant, orNone(got), orNone(c.want))
				}
				if schema.Items != nil && schema.Items.OpenAPIV3Schema != nil && schema.Items.Default != nil {
					t.Errorf("%s: default must sit on the array, not its items", variant)
				}
			}
		})
	}
}

func TestFieldDefault_RepeatedMessage(t *testing.T) {
	field, reg, names := makeRepeatedMessageRefFieldWithExtension(t, "items", "Item", &options.JSONSchema{Default: "[]"})
	schema := buildPropertySchemaWithReferencesFromField(field, reg, names)
	if got := defaultJSON(t, schema); got != `[]` {
		t.Fatalf("default = %s, want []", orNone(got))
	}
	if schema.Items == nil || schema.Items.Ref != "#/components/schemas/Item" {
		t.Fatalf("items must stay a bare $ref, got %#v", schema.Items)
	}

	field, reg, names = makeRepeatedMessageRefFieldWithExtension(t, "items", "Item", &options.JSONSchema{Default: `["x"]`})
	if got := defaultJSON(t, buildPropertySchemaWithReferencesFromField(field, reg, names)); got != "" {
		t.Fatalf("non-object message item must be dropped, got %s", got)
	}
}

func TestFieldDefault_MessageRef(t *testing.T) {
	t.Run("bare ref is wrapped so the default can sit beside it", func(t *testing.T) {
		field, reg, names := makeMessageRefFieldWithExtension(t, "item", "Item", &options.JSONSchema{Default: `{"id": "a"}`})
		schema := buildPropertySchemaWithReferencesFromField(field, reg, names)
		if schema.Ref != "" || schema.OpenAPIV3Schema == nil {
			t.Fatalf("expected allOf wrapper, got %#v", schema)
		}
		if len(schema.AllOf) != 1 || schema.AllOf[0].Ref != "#/components/schemas/Item" {
			t.Fatalf("expected allOf [$ref Item], got %#v", schema.AllOf)
		}
		if schema.Type != "" {
			t.Errorf("wrapper must not set type, got %q", schema.Type)
		}
		if got := defaultJSON(t, schema); got != `{"id":"a"}` {
			t.Errorf("default = %s", orNone(got))
		}
	})
	t.Run("existing annotation wrapper keeps its annotations", func(t *testing.T) {
		field, reg, names := makeMessageRefFieldWithExtension(t, "item", "Item", &options.JSONSchema{Default: `{}`, Description: "the item"})
		schema := assertAnnotatedRefWrapper(t, buildPropertySchemaWithReferencesFromField(field, reg, names), "#/components/schemas/Item", "the item")
		if schema.Default == nil {
			t.Fatal("expected default on the annotation wrapper")
		}
	})
	t.Run("non-object default is dropped and the ref stays bare", func(t *testing.T) {
		field, reg, names := makeMessageRefFieldWithExtension(t, "item", "Item", &options.JSONSchema{Default: `[]`})
		assertBareRef(t, buildPropertySchemaWithReferencesFromField(field, reg, names), "#/components/schemas/Item")
	})
	t.Run("no default leaves a bare ref", func(t *testing.T) {
		field, reg, names := makeMessageRefFieldWithExtension(t, "item", "Item", nil)
		assertBareRef(t, buildPropertySchemaWithReferencesFromField(field, reg, names), "#/components/schemas/Item")
	})
}

// The non-reference builder inlines message schemas straight out of schemaMap;
// a default on one field must not leak onto the shared component schema.
func TestFieldDefault_InlineMessageDoesNotMutateSchemaMap(t *testing.T) {
	field, reg, names := makeMessageRefFieldWithExtension(t, "item", "Item", &options.JSONSchema{Default: `{"id": "a"}`})
	shared := &OpenAPIV3Schema{
		Type:       "object",
		Properties: map[string]*OpenAPIV3SchemaRef{"id": {OpenAPIV3Schema: &OpenAPIV3Schema{Type: "string"}}},
	}
	schemaMap := map[string]*OpenAPIV3SchemaRef{".example.Item": {OpenAPIV3Schema: shared}}
	schema := buildPropertySchemaFromField(field, schemaMap, names, reg)
	if got := defaultJSON(t, schema); got != `{"id":"a"}` {
		t.Fatalf("default = %s", orNone(got))
	}
	if shared.Default != nil {
		t.Fatalf("shared schemaMap entry was mutated: default = %v", shared.Default)
	}
}

func TestFieldDefault_Map(t *testing.T) {
	field, reg := makeMapFieldWithExtension(t, "labels", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{Default: `{"a": "b"}`})
	if got := defaultJSON(t, buildPropertySchemaWithReferencesFromField(field, reg, map[string]string{})); got != `{"a":"b"}` {
		t.Fatalf("default = %s", orNone(got))
	}
	field, reg = makeMapFieldWithExtension(t, "labels", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{Default: `[]`})
	if got := defaultJSON(t, buildPropertySchemaWithReferencesFromField(field, reg, map[string]string{})); got != "" {
		t.Fatalf("array default on a map must be dropped, got %s", got)
	}
}

// newEnumDefaultFixture loads a Parent message with a singular and a repeated
// field of enum Color, whose COLOR_HIDDEN value carries a DEV restriction.
func newEnumDefaultFixture(t *testing.T, singular, repeated string, selectors ...string) (single, many *descriptor.Field, reg *descriptor.Registry, names map[string]string) {
	t.Helper()
	fieldOpts := func(def string) *descriptorpb.FieldOptions {
		opts := &descriptorpb.FieldOptions{}
		proto.SetExtension(opts, options.E_Openapiv3Field, &options.JSONSchema{Default: def})
		return opts
	}
	hiddenOpts := &descriptorpb.EnumValueOptions{}
	proto.SetExtension(hiddenOpts, visibility.E_ValueVisibility, &visibility.VisibilityRule{Restriction: "DEV"})
	file := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("enum_default.proto"),
		Package: proto.String("example"),
		Syntax:  proto.String("proto3"),
		Options: &descriptorpb.FileOptions{GoPackage: proto.String("example.com/example;example")},
		EnumType: []*descriptorpb.EnumDescriptorProto{{
			Name: proto.String("Color"),
			Value: []*descriptorpb.EnumValueDescriptorProto{
				{Name: proto.String("COLOR_UNSPECIFIED"), Number: proto.Int32(0)},
				{Name: proto.String("COLOR_RED"), Number: proto.Int32(1)},
				{Name: proto.String("COLOR_HIDDEN"), Number: proto.Int32(2), Options: hiddenOpts},
			},
		}},
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("Parent"),
			Field: []*descriptorpb.FieldDescriptorProto{
				{
					Name: proto.String("color"), Number: proto.Int32(1),
					Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
					Type:     descriptorpb.FieldDescriptorProto_TYPE_ENUM.Enum(),
					TypeName: proto.String(".example.Color"),
					Options:  fieldOpts(singular),
				},
				{
					Name: proto.String("colors"), Number: proto.Int32(2),
					Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
					Type:     descriptorpb.FieldDescriptorProto_TYPE_ENUM.Enum(),
					TypeName: proto.String(".example.Color"),
					Options:  fieldOpts(repeated),
				},
			},
		}},
	}
	reg = descriptor.NewRegistry()
	for _, s := range selectors {
		reg.SetVisibilityRestrictionSelectors([]string{s})
	}
	if err := reg.Load(&pluginpb.CodeGeneratorRequest{ProtoFile: []*descriptorpb.FileDescriptorProto{file}}); err != nil {
		t.Fatalf("reg.Load: %v", err)
	}
	msg, err := reg.LookupMsg("", ".example.Parent")
	if err != nil {
		t.Fatalf("LookupMsg: %v", err)
	}
	return msg.Fields[0], msg.Fields[1], reg, map[string]string{".example.Color": "Color"}
}

func TestFieldDefault_Enum(t *testing.T) {
	cases := []struct {
		name, singular, repeated string
		selectors                []string
		wantSingular, wantMany   string
	}{
		{"value name", "COLOR_RED", `["COLOR_RED", "COLOR_UNSPECIFIED"]`, nil, `"COLOR_RED"`, `["COLOR_RED","COLOR_UNSPECIFIED"]`},
		{"quoted value name", `"COLOR_RED"`, `[]`, nil, `"COLOR_RED"`, `[]`},
		{"unknown value", "COLOR_BLUE", `["COLOR_BLUE"]`, nil, ``, ``},
		{"numeric value is not the JSON wire form", "1", `[1]`, nil, ``, ``},
		{"value hidden from this spec", "COLOR_HIDDEN", `["COLOR_HIDDEN"]`, nil, ``, ``},
		{"hidden value visible under its selector", "COLOR_HIDDEN", `["COLOR_HIDDEN"]`, []string{"DEV"}, `"COLOR_HIDDEN"`, `["COLOR_HIDDEN"]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			single, many, reg, names := newEnumDefaultFixture(t, c.singular, c.repeated, c.selectors...)
			for variant, schema := range buildBothVariants(t, single, reg, names) {
				if got := defaultJSON(t, schema); got != c.wantSingular {
					t.Errorf("%s singular: default = %s, want %s", variant, orNone(got), orNone(c.wantSingular))
				}
				if c.wantSingular == "" {
					assertBareRef(t, schema, "#/components/schemas/Color")
				} else if len(schema.AllOf) != 1 || schema.AllOf[0].Ref != "#/components/schemas/Color" {
					t.Errorf("%s singular: expected allOf [$ref Color], got %#v", variant, schema)
				}
			}
			for variant, schema := range buildBothVariants(t, many, reg, names) {
				if got := defaultJSON(t, schema); got != c.wantMany {
					t.Errorf("%s repeated: default = %s, want %s", variant, orNone(got), orNone(c.wantMany))
				}
			}
		})
	}
}

func TestFieldDefault_YAMLEncodesTypedValue(t *testing.T) {
	field := makeSingularFieldWithExtension("f", descriptorpb.FieldDescriptorProto_TYPE_INT32, &options.JSONSchema{Default: "5"})
	out, err := yaml.Marshal(buildPropertySchemaWithReferencesFromField(field, descriptor.NewRegistry(), map[string]string{}).OpenAPIV3Schema)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	if !strings.Contains(string(out), "default: 5\n") {
		t.Fatalf("expected an unquoted integer default in YAML, got:\n%s", out)
	}
}

const fieldDefaultSpecRequest = `
file_to_generate: "defaults/v1/defaults.proto"
proto_file: {
  name: "defaults/v1/defaults.proto"
  package: "defaults.v1"
  enum_type: {
    name: "Mode"
    value: { name: "MODE_UNSPECIFIED" number: 0 }
    value: { name: "MODE_FAST" number: 1 }
  }
  message_type: {
    name: "Widget"
    field: { name: "id" number: 1 label: LABEL_OPTIONAL type: TYPE_STRING json_name: "id" }
    field: { name: "enabled" number: 2 label: LABEL_OPTIONAL type: TYPE_BOOL json_name: "enabled"
      options: { [grpc.gateway.protoc_gen_openapiv3.options.openapiv3_field]: { default: "true" description: "Whether it runs." } } }
    field: { name: "retries" number: 3 label: LABEL_OPTIONAL type: TYPE_INT32 json_name: "retries"
      options: { [grpc.gateway.protoc_gen_openapiv3.options.openapiv3_field]: { default: "3" maximum: 10 } } }
    field: { name: "size" number: 4 label: LABEL_OPTIONAL type: TYPE_INT64 json_name: "size"
      options: { [grpc.gateway.protoc_gen_openapiv3.options.openapiv3_field]: { default: "0" } } }
    field: { name: "tags" number: 5 label: LABEL_REPEATED type: TYPE_STRING json_name: "tags"
      options: { [grpc.gateway.protoc_gen_openapiv3.options.openapiv3_field]: { default: "[]" } } }
    field: { name: "mode" number: 6 label: LABEL_OPTIONAL type: TYPE_ENUM type_name: ".defaults.v1.Mode" json_name: "mode"
      options: { [grpc.gateway.protoc_gen_openapiv3.options.openapiv3_field]: { default: "MODE_FAST" } } }
    field: { name: "broken" number: 7 label: LABEL_OPTIONAL type: TYPE_INT32 json_name: "broken"
      options: { [grpc.gateway.protoc_gen_openapiv3.options.openapiv3_field]: { default: "lots" } } }
    field: { name: "plain" number: 8 label: LABEL_OPTIONAL type: TYPE_STRING json_name: "plain" }
  }
  message_type: {
    name: "ListWidgetsRequest"
    field: { name: "page_size" number: 1 label: LABEL_OPTIONAL type: TYPE_INT32 json_name: "pageSize"
      options: { [grpc.gateway.protoc_gen_openapiv3.options.openapiv3_field]: { default: "50" } } }
    field: { name: "mode" number: 2 label: LABEL_OPTIONAL type: TYPE_ENUM type_name: ".defaults.v1.Mode" json_name: "mode"
      options: { [grpc.gateway.protoc_gen_openapiv3.options.openapiv3_field]: { default: "MODE_FAST" } } }
  }
  message_type: {
    name: "ListWidgetsResponse"
    field: { name: "widgets" number: 1 label: LABEL_REPEATED type: TYPE_MESSAGE type_name: ".defaults.v1.Widget" json_name: "widgets" }
  }
  service: {
    name: "WidgetService"
    method: { name: "ListWidgets" input_type: ".defaults.v1.ListWidgetsRequest" output_type: ".defaults.v1.ListWidgetsResponse" options: { [google.api.http]: { get: "/v1/widgets" } } }
    method: { name: "CreateWidget" input_type: ".defaults.v1.Widget" output_type: ".defaults.v1.Widget" options: { [google.api.http]: { post: "/v1/widgets" body: "*" } } }
  }
  options: { go_package: "example.com/defaults/v1;defaultsv1" }
  syntax: "proto3"
}
`

// TestFieldDefault_EndToEnd runs the full generator and checks the defaults as
// they land in the emitted document: component properties and query parameters.
func TestFieldDefault_EndToEnd(t *testing.T) {
	spec := generateMergedSpec(t, fieldDefaultSpecRequest)
	var doc struct {
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(spec), &doc); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}
	widget, ok := doc.Components.Schemas["Widget"]
	if !ok {
		t.Fatalf("Widget schema missing; schemas: %v", keys(doc.Components.Schemas))
	}
	wantProps := map[string]string{
		"enabled": `true`,
		"retries": `3`,
		"size":    `"0"`,
		"tags":    `[]`,
		"mode":    `"MODE_FAST"`,
		"broken":  ``,
		"plain":   ``,
		"id":      ``,
	}
	for name, want := range wantProps {
		prop, ok := widget.Properties[name]
		if !ok {
			t.Errorf("Widget.%s missing", name)
			continue
		}
		if got := compactJSON(t, prop["default"]); got != want {
			t.Errorf("Widget.%s default = %s, want %s", name, orNone(got), orNone(want))
		}
	}
	if !strings.Contains(string(widget.Properties["enabled"]["description"]), "Whether it runs.") {
		t.Errorf("Widget.enabled lost its description: %s", widget.Properties["enabled"]["description"])
	}
	if !strings.Contains(string(widget.Properties["mode"]["allOf"]), `#/components/schemas/Mode`) {
		t.Errorf("Widget.mode must keep its $ref inside allOf, got %v", widget.Properties["mode"])
	}

	var op struct {
		Parameters []struct {
			Name   string                     `json:"name"`
			Schema map[string]json.RawMessage `json:"schema"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(doc.Paths["/v1/widgets"]["get"], &op); err != nil {
		t.Fatalf("unmarshal GET /v1/widgets: %v", err)
	}
	wantParams := map[string]string{"page_size": `50`, "mode": `"MODE_FAST"`}
	for _, p := range op.Parameters {
		want, ok := wantParams[p.Name]
		if !ok {
			continue
		}
		delete(wantParams, p.Name)
		if got := compactJSON(t, p.Schema["default"]); got != want {
			t.Errorf("query parameter %s default = %s, want %s", p.Name, orNone(got), want)
		}
	}
	for name := range wantParams {
		t.Errorf("query parameter %s missing", name)
	}
}

func compactJSON(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	if len(raw) == 0 {
		return ""
	}
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("invalid JSON %s: %v", raw, err)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
