package genopenapi

import (
	"encoding/json"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/internal/descriptor"
	options "github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv3/options"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/structpb"
)

const factsEnumType = ".example.Kind"

var factsResolvedNames = map[string]string{factsEnumType: "Kind"}

func withProto3Optional(field *descriptor.Field) *descriptor.Field {
	field.Proto3Optional = proto.Bool(true)
	return field
}

func withFieldBehavior(field *descriptor.Field, behaviors ...annotations.FieldBehavior) *descriptor.Field {
	if field.Options == nil {
		field.Options = &descriptorpb.FieldOptions{}
	}
	proto.SetExtension(field.Options, annotations.E_FieldBehavior, behaviors)
	return field
}

func makeRepeatedEnumField(name string) *descriptor.Field {
	field := makeEnumRefFieldWithExtension(name, factsEnumType, nil)
	field.Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
	return field
}

// buildWithBothBuilders runs the field through the component builder (with
// references) and the request builder (without references).
func buildWithBothBuilders(t *testing.T, field *descriptor.Field, reg *descriptor.Registry, resolvedNames map[string]string) map[string]*OpenAPIV3SchemaRef {
	t.Helper()
	if reg == nil {
		reg = descriptor.NewRegistry()
	}
	return map[string]*OpenAPIV3SchemaRef{
		"with references":    buildPropertySchemaWithReferencesFromField(field, reg, resolvedNames),
		"without references": buildPropertySchemaFromField(field, map[string]*OpenAPIV3SchemaRef{}, resolvedNames, reg),
	}
}

func extension(schema *OpenAPIV3SchemaRef, key string) interface{} {
	if schema == nil || schema.OpenAPIV3Schema == nil {
		return nil
	}
	return schema.OpenAPIV3Extensions[key]
}

func marshalDefault(t *testing.T, schema *OpenAPIV3SchemaRef) string {
	t.Helper()
	if schema == nil || schema.OpenAPIV3Schema == nil || schema.Default == nil {
		return ""
	}
	b, err := json.Marshal(schema.Default)
	if err != nil {
		t.Fatalf("json.Marshal(default): %v", err)
	}
	return string(b)
}

func TestProtoFieldFacts_PresenceOnOptionalScalarsAndEnums(t *testing.T) {
	cases := map[string]*descriptor.Field{
		"string": withProto3Optional(makeFieldWithExtension("name", descriptorpb.FieldDescriptorProto_TYPE_STRING, nil)),
		"bool":   withProto3Optional(makeFieldWithExtension("enabled", descriptorpb.FieldDescriptorProto_TYPE_BOOL, nil)),
		"double": withProto3Optional(makeFieldWithExtension("threshold", descriptorpb.FieldDescriptorProto_TYPE_DOUBLE, nil)),
		"int64":  withProto3Optional(makeFieldWithExtension("size", descriptorpb.FieldDescriptorProto_TYPE_INT64, nil)),
	}
	for name, field := range cases {
		for builder, schema := range buildWithBothBuilders(t, field, nil, nil) {
			if extension(schema, presenceExtension) != true {
				t.Errorf("%s (%s): %s = %v, want true", name, builder, presenceExtension, extension(schema, presenceExtension))
			}
		}
	}
}

func TestProtoFieldFacts_PresenceOnOptionalEnumGoesNextToRef(t *testing.T) {
	field := withProto3Optional(makeEnumRefFieldWithExtension("kind", factsEnumType, nil))
	for builder, schema := range buildWithBothBuilders(t, field, nil, factsResolvedNames) {
		if schema.Ref != "#/components/schemas/Kind" {
			t.Fatalf("%s: $ref = %q, want the enum $ref", builder, schema.Ref)
		}
		if schema.OpenAPIV3Schema == nil || len(schema.AllOf) != 0 {
			t.Fatalf("%s: got %#v, want keys next to the $ref", builder, schema.OpenAPIV3Schema)
		}
		if extension(schema, presenceExtension) != true {
			t.Fatalf("%s: %s = %v, want true", builder, presenceExtension, extension(schema, presenceExtension))
		}
	}
}

func TestProtoFieldFacts_PresenceKeepsExistingAllOfWrapper(t *testing.T) {
	field := withProto3Optional(makeEnumRefFieldWithExtension("kind", factsEnumType, &options.JSONSchema{Description: "The kind."}))
	for builder, schema := range buildWithBothBuilders(t, field, nil, factsResolvedNames) {
		wrapper := assertAnnotatedRefWrapper(t, schema, "#/components/schemas/Kind", "The kind.")
		if wrapper.OpenAPIV3Extensions[presenceExtension] != true {
			t.Fatalf("%s: %s = %v, want true", builder, presenceExtension, wrapper.OpenAPIV3Extensions[presenceExtension])
		}
	}
}

func TestProtoFieldFacts_NoPresenceWithoutProto3Optional(t *testing.T) {
	messageField, reg, resolvedNames := makeMessageRefFieldWithExtension(t, "config", "Config", nil)
	cases := map[string]struct {
		field         *descriptor.Field
		reg           *descriptor.Registry
		resolvedNames map[string]string
	}{
		"plain scalar":     {field: makeFieldWithExtension("name", descriptorpb.FieldDescriptorProto_TYPE_STRING, nil)},
		"wrapper":          {field: makeWrapperField("enabled", ".google.protobuf.BoolValue", nil)},
		"optional message": {field: withProto3Optional(messageField), reg: reg, resolvedNames: resolvedNames},
	}
	for name, c := range cases {
		for builder, schema := range buildWithBothBuilders(t, c.field, c.reg, c.resolvedNames) {
			if got := extension(schema, presenceExtension); got != nil {
				t.Errorf("%s (%s): %s = %v, want no mark", name, builder, presenceExtension, got)
			}
		}
	}
}

func TestProtoFieldFacts_ExplicitPresenceExtensionWins(t *testing.T) {
	field := withProto3Optional(makeFieldWithExtension("name", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{
		Extensions: map[string]*structpb.Value{presenceExtension: structpb.NewBoolValue(false)},
	}))
	for builder, schema := range buildWithBothBuilders(t, field, nil, nil) {
		value, ok := extension(schema, presenceExtension).(*structpb.Value)
		if !ok || value.GetBoolValue() {
			t.Fatalf("%s: %s = %v, want the proto value false", builder, presenceExtension, extension(schema, presenceExtension))
		}
	}
}

func TestProtoFieldFacts_TypedDefaults(t *testing.T) {
	withDefault := func(value string) *options.JSONSchema { return &options.JSONSchema{Default: value} }
	cases := map[string]struct {
		field *descriptor.Field
		want  string
	}{
		"bool":              {field: makeFieldWithExtension("enabled", descriptorpb.FieldDescriptorProto_TYPE_BOOL, withDefault("true")), want: `true`},
		"int32":             {field: makeFieldWithExtension("count", descriptorpb.FieldDescriptorProto_TYPE_INT32, withDefault("3")), want: `3`},
		"double":            {field: makeFieldWithExtension("ratio", descriptorpb.FieldDescriptorProto_TYPE_DOUBLE, withDefault("0.5")), want: `0.5`},
		"int64 is text":     {field: makeFieldWithExtension("size", descriptorpb.FieldDescriptorProto_TYPE_INT64, withDefault("100")), want: `"100"`},
		"string":            {field: makeFieldWithExtension("name", descriptorpb.FieldDescriptorProto_TYPE_STRING, withDefault("hello")), want: `"hello"`},
		"string of JSON":    {field: makeFieldWithExtension("name", descriptorpb.FieldDescriptorProto_TYPE_STRING, withDefault("[]")), want: `"[]"`},
		"enum":              {field: makeEnumRefFieldWithExtension("kind", factsEnumType, withDefault("KIND_FAST")), want: `"KIND_FAST"`},
		"repeated":          {field: makeRepeatedFieldWithExtension("labels", descriptorpb.FieldDescriptorProto_TYPE_STRING, withDefault("[]")), want: `[]`},
		"bool wrapper":      {field: makeWrapperField("enabled", ".google.protobuf.BoolValue", withDefault("false")), want: `false`},
		"uint32 wrapper":    {field: makeWrapperField("limit", ".google.protobuf.UInt32Value", withDefault("0")), want: `0`},
		"not a number":      {field: makeFieldWithExtension("count", descriptorpb.FieldDescriptorProto_TYPE_INT32, withDefault("many")), want: `"many"`},
		"not a JSON bool":   {field: makeFieldWithExtension("enabled", descriptorpb.FieldDescriptorProto_TYPE_BOOL, withDefault("TRUE")), want: `"TRUE"`},
		"not a JSON array":  {field: makeRepeatedFieldWithExtension("labels", descriptorpb.FieldDescriptorProto_TYPE_STRING, withDefault("[a")), want: `"[a"`},
		"NaN is not a JSON": {field: makeFieldWithExtension("ratio", descriptorpb.FieldDescriptorProto_TYPE_DOUBLE, withDefault("NaN")), want: `"NaN"`},
	}
	for name, c := range cases {
		for builder, schema := range buildWithBothBuilders(t, c.field, nil, factsResolvedNames) {
			if got := marshalDefault(t, schema); got != c.want {
				t.Errorf("%s (%s): default = %s, want %s", name, builder, got, c.want)
			}
		}
	}
}

func TestProtoFieldFacts_EnumDefaultGoesNextToRef(t *testing.T) {
	field := makeEnumRefFieldWithExtension("kind", factsEnumType, &options.JSONSchema{Default: "KIND_FAST"})
	for builder, schema := range buildWithBothBuilders(t, field, nil, factsResolvedNames) {
		if schema.Ref != "#/components/schemas/Kind" || schema.OpenAPIV3Schema == nil || len(schema.AllOf) != 0 {
			t.Fatalf("%s: got ref %q and %#v, want keys next to the $ref", builder, schema.Ref, schema.OpenAPIV3Schema)
		}
	}
}

func TestProtoFieldFacts_UnorderedListIsASet(t *testing.T) {
	messageField, reg, resolvedNames := makeRepeatedMessageRefFieldWithExtension(t, "rules", "Rule", nil)
	cases := map[string]struct {
		field         *descriptor.Field
		reg           *descriptor.Registry
		resolvedNames map[string]string
	}{
		"repeated scalar":  {field: makeRepeatedField("labels", descriptorpb.FieldDescriptorProto_TYPE_STRING)},
		"repeated enum":    {field: makeRepeatedEnumField("kinds"), resolvedNames: factsResolvedNames},
		"repeated message": {field: messageField, reg: reg, resolvedNames: resolvedNames},
	}
	for name, c := range cases {
		withFieldBehavior(c.field, annotations.FieldBehavior_UNORDERED_LIST)
		for builder, schema := range buildWithBothBuilders(t, c.field, c.reg, c.resolvedNames) {
			if schema.Type != "array" || !schema.UniqueItems || extension(schema, collectionExtension) != "set" {
				t.Errorf("%s (%s): type %q, uniqueItems %v, %s %v; want an array set",
					name, builder, schema.Type, schema.UniqueItems, collectionExtension, extension(schema, collectionExtension))
			}
		}
	}
}

func TestProtoFieldFacts_UnorderedListIgnoredOnScalarsAndMaps(t *testing.T) {
	mapField, reg := makeMapFieldWithExtension(t, "tags", descriptorpb.FieldDescriptorProto_TYPE_STRING, nil)
	cases := map[string]struct {
		field *descriptor.Field
		reg   *descriptor.Registry
	}{
		"scalar": {field: makeFieldWithExtension("name", descriptorpb.FieldDescriptorProto_TYPE_STRING, nil)},
		"map":    {field: mapField, reg: reg},
	}
	for name, c := range cases {
		withFieldBehavior(c.field, annotations.FieldBehavior_UNORDERED_LIST)
		for builder, schema := range buildWithBothBuilders(t, c.field, c.reg, nil) {
			if schema.UniqueItems || extension(schema, collectionExtension) != nil {
				t.Errorf("%s (%s): got uniqueItems %v and %s %v, want neither",
					name, builder, schema.UniqueItems, collectionExtension, extension(schema, collectionExtension))
			}
		}
	}
}

func TestProtoFieldFacts_UniqueItemsAloneIsNotASet(t *testing.T) {
	field := makeRepeatedFieldWithExtension("labels", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{UniqueItems: true})
	for builder, schema := range buildWithBothBuilders(t, field, nil, nil) {
		if !schema.UniqueItems || extension(schema, collectionExtension) != nil {
			t.Errorf("%s: uniqueItems %v, %s %v; want uniqueItems only",
				builder, schema.UniqueItems, collectionExtension, extension(schema, collectionExtension))
		}
	}
}

func TestProtoFieldFacts_OutputOnlyIsReadOnly(t *testing.T) {
	messageField, messageReg, messageNames := makeMessageRefFieldWithExtension(t, "owner", "Owner", nil)
	mapField, mapReg := makeMapFieldWithExtension(t, "tags", descriptorpb.FieldDescriptorProto_TYPE_STRING, nil)
	cases := map[string]struct {
		field         *descriptor.Field
		reg           *descriptor.Registry
		resolvedNames map[string]string
		wantRef       string
	}{
		"scalar":  {field: makeFieldWithExtension("revision", descriptorpb.FieldDescriptorProto_TYPE_STRING, nil)},
		"enum":    {field: makeEnumRefFieldWithExtension("state", factsEnumType, nil), resolvedNames: factsResolvedNames, wantRef: "#/components/schemas/Kind"},
		"message": {field: messageField, reg: messageReg, resolvedNames: messageNames},
		"list":    {field: makeRepeatedField("ids", descriptorpb.FieldDescriptorProto_TYPE_STRING)},
		"map":     {field: mapField, reg: mapReg},
	}
	for name, c := range cases {
		withFieldBehavior(c.field, annotations.FieldBehavior_OUTPUT_ONLY)
		for builder, schema := range buildWithBothBuilders(t, c.field, c.reg, c.resolvedNames) {
			if schema.OpenAPIV3Schema == nil || !schema.ReadOnly {
				t.Errorf("%s (%s): want readOnly: true", name, builder)
			}
			if c.wantRef != "" && schema.Ref != c.wantRef {
				t.Errorf("%s (%s): $ref = %q, want %q next to readOnly", name, builder, schema.Ref, c.wantRef)
			}
		}
	}
}

func TestProtoFieldFacts_ReadOnlyAnnotationStillWorks(t *testing.T) {
	cases := map[string]*descriptor.Field{
		"read_only": makeFieldWithExtension("revision", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{ReadOnly: true}),
		"both": withFieldBehavior(
			makeFieldWithExtension("revision", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{ReadOnly: true}),
			annotations.FieldBehavior_OUTPUT_ONLY,
		),
	}
	for name, field := range cases {
		for builder, schema := range buildWithBothBuilders(t, field, nil, nil) {
			if !schema.ReadOnly {
				t.Errorf("%s (%s): want readOnly: true", name, builder)
			}
		}
	}
}

func TestProtoFieldFacts_OtherFieldBehaviorsAreIgnored(t *testing.T) {
	for _, behavior := range []annotations.FieldBehavior{
		annotations.FieldBehavior_REQUIRED,
		annotations.FieldBehavior_IMMUTABLE,
		annotations.FieldBehavior_INPUT_ONLY,
		annotations.FieldBehavior_OPTIONAL,
	} {
		plain := makeFieldWithExtension("name", descriptorpb.FieldDescriptorProto_TYPE_STRING, nil)
		marked := withFieldBehavior(makeFieldWithExtension("name", descriptorpb.FieldDescriptorProto_TYPE_STRING, nil), behavior)
		want := buildWithBothBuilders(t, plain, nil, nil)
		for builder, got := range buildWithBothBuilders(t, marked, nil, nil) {
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(want[builder])
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("%s (%s): schema = %s, want %s", behavior, builder, gotJSON, wantJSON)
			}
		}
	}
}

func TestProtoFieldFacts_SharedSchemasAreNotChanged(t *testing.T) {
	field, reg, resolvedNames := makeMessageRefFieldWithExtension(t, "owner", "Owner", nil)
	withFieldBehavior(field, annotations.FieldBehavior_OUTPUT_ONLY)
	shared := &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
		Type:       "object",
		Properties: map[string]*OpenAPIV3SchemaRef{"id": {OpenAPIV3Schema: &OpenAPIV3Schema{Type: "string"}}},
	}}
	schemaMap := map[string]*OpenAPIV3SchemaRef{field.GetTypeName(): shared}

	schema := buildPropertySchemaFromField(field, schemaMap, resolvedNames, reg)
	if !schema.ReadOnly {
		t.Fatal("want readOnly: true on the inline message schema")
	}
	if shared.ReadOnly {
		t.Fatal("the shared schemaMap entry changed")
	}

	wrapper := makeWrapperField("enabled", ".google.protobuf.BoolValue", &options.JSONSchema{Default: "true"})
	buildWithBothBuilders(t, wrapper, nil, nil)
	if wellKnownTypesToOpenAPIV3SchemaMapping[".google.protobuf.BoolValue"].Default != nil {
		t.Fatal("the shared well-known type schema changed")
	}
}

func TestOpenAPIV3SchemaRefMarshalJSON_KeysNextToRef(t *testing.T) {
	cases := map[string]struct {
		schema *OpenAPIV3SchemaRef
		want   string
	}{
		"bare ref": {
			schema: &OpenAPIV3SchemaRef{Ref: "#/components/schemas/Kind"},
			want:   `{"$ref":"#/components/schemas/Kind"}`,
		},
		"keys next to ref": {
			schema: &OpenAPIV3SchemaRef{Ref: "#/components/schemas/Kind", OpenAPIV3Schema: &OpenAPIV3Schema{
				Default:             RawExample(`"KIND_FAST"`),
				OpenAPIV3Extensions: OpenAPIV3Extensions{presenceExtension: true},
			}},
			want: `{"$ref":"#/components/schemas/Kind","default":"KIND_FAST","x-coralogix-presence":true}`,
		},
	}
	for name, c := range cases {
		got, err := json.Marshal(c.schema)
		if err != nil {
			t.Fatalf("%s: json.Marshal: %v", name, err)
		}
		if string(got) != c.want {
			t.Errorf("%s: got %s, want %s", name, got, c.want)
		}
	}
}
