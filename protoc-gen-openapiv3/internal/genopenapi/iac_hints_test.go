package genopenapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/internal/descriptor"
	options "github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv3/options"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func withIac(hints *options.IacHints) *options.JSONSchema {
	return &options.JSONSchema{Iac: hints}
}

func marshalIacExtension(t *testing.T, schema *OpenAPIV3SchemaRef) string {
	t.Helper()
	value := extension(schema, iacExtension)
	if value == nil {
		return ""
	}
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal(%s): %v", iacExtension, err)
	}
	return string(b)
}

// iacMessage returns a message that owns the fields, the way the registry
// links them.
func iacMessage(fields ...*descriptor.Field) *descriptor.Message {
	message := &descriptor.Message{
		DescriptorProto: &descriptorpb.DescriptorProto{Name: proto.String("Thing")},
		File: &descriptor.File{FileDescriptorProto: &descriptorpb.FileDescriptorProto{
			Package: proto.String("example.v1"),
		}},
		Fields: fields,
	}
	for _, field := range fields {
		field.Message = message
	}
	return message
}

func inOneof(field *descriptor.Field) *descriptor.Field {
	field.OneofIndex = proto.Int32(0)
	return field
}

func TestIacHints_AllKnobsAreEmittedInCamelCase(t *testing.T) {
	field := makeFieldWithExtension("query_text", descriptorpb.FieldDescriptorProto_TYPE_STRING, withIac(&options.IacHints{
		Examples:      []string{`"source logs | limit 1"`, `"source logs | limit 2"`},
		RequiredByApi: true,
		ConflictsWith: []string{"legacy_query"},
		Requires: []*options.IacHints_Requires{
			{Field: "product_type", Equals: `"apm"`},
			{Field: "service_name"},
		},
		ArmFixed: true,
	}))
	want := `{"armFixed":true,"conflictsWith":["legacyQuery"],` +
		`"examples":["source logs | limit 1","source logs | limit 2"],` +
		`"requiredByApi":true,` +
		`"requires":[{"equals":"apm","field":"productType"},{"field":"serviceName"}]}`
	for builder, schema := range buildWithBothBuilders(t, field, nil, nil) {
		if got := marshalIacExtension(t, schema); got != want {
			t.Errorf("%s: %s = %s, want %s", builder, iacExtension, got, want)
		}
	}
}

func TestIacHints_OnlySetKnobsAreEmitted(t *testing.T) {
	field := makeFieldWithExtension("name", descriptorpb.FieldDescriptorProto_TYPE_STRING, withIac(&options.IacHints{RequiredByApi: true}))
	for builder, schema := range buildWithBothBuilders(t, field, nil, nil) {
		if got, want := marshalIacExtension(t, schema), `{"requiredByApi":true}`; got != want {
			t.Errorf("%s: %s = %s, want %s", builder, iacExtension, got, want)
		}
	}
}

func TestIacHints_ExampleKeepsLargeNumbersAndObjects(t *testing.T) {
	field := makeFieldWithExtension("id", descriptorpb.FieldDescriptorProto_TYPE_STRING, withIac(&options.IacHints{
		Examples: []string{`{"cpu": {"color": "#ff0000"}}`, `12345678901234567890`},
	}))
	for builder, schema := range buildWithBothBuilders(t, field, nil, nil) {
		want := `{"examples":[{"cpu":{"color":"#ff0000"}},12345678901234567890]}`
		if got := marshalIacExtension(t, schema); got != want {
			t.Errorf("%s: %s = %s, want %s", builder, iacExtension, got, want)
		}
	}
}

func TestIacHints_NoExtensionWithoutKnobs(t *testing.T) {
	cases := map[string]*descriptor.Field{
		"no annotation":      makeFieldWithExtension("name", descriptorpb.FieldDescriptorProto_TYPE_STRING, nil),
		"annotation, no iac": makeFieldWithExtension("name", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{Description: "A name."}),
		"empty iac":          makeFieldWithExtension("name", descriptorpb.FieldDescriptorProto_TYPE_STRING, withIac(&options.IacHints{})),
	}
	for name, field := range cases {
		for builder, schema := range buildWithBothBuilders(t, field, nil, nil) {
			if got := extension(schema, iacExtension); got != nil {
				t.Errorf("%s (%s): %s = %v, want none", name, builder, iacExtension, got)
			}
		}
	}
}

func TestIacHints_EmittedOnEveryFieldShape(t *testing.T) {
	hints := withIac(&options.IacHints{RequiredByApi: true})
	messageField, messageReg, messageNames := makeMessageRefFieldWithExtension(t, "config", "Config", hints)
	repeatedMessageField, repeatedReg, repeatedNames := makeRepeatedMessageRefFieldWithExtension(t, "rules", "Rule", hints)
	mapField, mapReg := makeMapFieldWithExtension(t, "tags", descriptorpb.FieldDescriptorProto_TYPE_STRING, hints)
	cases := map[string]struct {
		field         *descriptor.Field
		reg           *descriptor.Registry
		resolvedNames map[string]string
	}{
		"scalar":           {field: makeFieldWithExtension("name", descriptorpb.FieldDescriptorProto_TYPE_STRING, hints)},
		"repeated scalar":  {field: makeRepeatedFieldWithExtension("labels", descriptorpb.FieldDescriptorProto_TYPE_STRING, hints)},
		"enum ref":         {field: makeEnumRefFieldWithExtension("kind", factsEnumType, hints), resolvedNames: factsResolvedNames},
		"wrapper":          {field: makeWrapperField("folder_id", ".google.protobuf.StringValue", hints)},
		"message ref":      {field: messageField, reg: messageReg, resolvedNames: messageNames},
		"repeated message": {field: repeatedMessageField, reg: repeatedReg, resolvedNames: repeatedNames},
		"map":              {field: mapField, reg: mapReg},
	}
	for name, c := range cases {
		for builder, schema := range buildWithBothBuilders(t, c.field, c.reg, c.resolvedNames) {
			if got, want := marshalIacExtension(t, schema), `{"requiredByApi":true}`; got != want {
				t.Errorf("%s (%s): %s = %s, want %s", name, builder, iacExtension, got, want)
			}
		}
	}
}

func TestIacHints_GoesNextToRef(t *testing.T) {
	field := makeEnumRefFieldWithExtension("kind", factsEnumType, withIac(&options.IacHints{RequiredByApi: true}))
	for builder, schema := range buildWithBothBuilders(t, field, nil, factsResolvedNames) {
		if schema.Ref != "#/components/schemas/Kind" || schema.OpenAPIV3Schema == nil || len(schema.AllOf) != 0 {
			t.Fatalf("%s: got ref %q and %#v, want keys next to the $ref", builder, schema.Ref, schema.OpenAPIV3Schema)
		}
		if got := marshalIacExtension(t, schema); got != `{"requiredByApi":true}` {
			t.Fatalf("%s: %s = %s", builder, iacExtension, got)
		}
	}
}

func TestIacHints_ExplicitExtensionWins(t *testing.T) {
	field := makeFieldWithExtension("name", descriptorpb.FieldDescriptorProto_TYPE_STRING, &options.JSONSchema{
		Iac:        &options.IacHints{RequiredByApi: true},
		Extensions: nil,
	})
	schema := &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
		OpenAPIV3Extensions: OpenAPIV3Extensions{iacExtension: "explicit"},
	}}
	got := applyProtoFieldFacts(schema, field)
	if got.OpenAPIV3Extensions[iacExtension] != "explicit" {
		t.Errorf("%s = %v, want the value from openapiv3_field.extensions", iacExtension, got.OpenAPIV3Extensions[iacExtension])
	}
}

func TestValidateIacHints_AcceptsGoodKnobs(t *testing.T) {
	query := makeFieldWithExtension("query", descriptorpb.FieldDescriptorProto_TYPE_STRING, withIac(&options.IacHints{
		Examples:      []string{`"a"`, `"b"`},
		ConflictsWith: []string{"legacy_query"},
		Requires:      []*options.IacHints_Requires{{Field: "product_type", Equals: `"apm"`}, {Field: "product_type"}},
	}))
	legacy := makeFieldWithExtension("legacy_query", descriptorpb.FieldDescriptorProto_TYPE_STRING, nil)
	product := makeFieldWithExtension("product_type", descriptorpb.FieldDescriptorProto_TYPE_STRING, nil)
	arm := inOneof(makeFieldWithExtension("apm", descriptorpb.FieldDescriptorProto_TYPE_STRING, withIac(&options.IacHints{ArmFixed: true})))
	object := makeFieldWithExtension("colors", descriptorpb.FieldDescriptorProto_TYPE_STRING, withIac(&options.IacHints{
		Examples: []string{`{"cpu": {"color": "#ff0000"}}`, `[1, 2]`, `true`, `null`, `1.5`},
	}))
	if err := validateIacHints([]*descriptor.Message{iacMessage(query, legacy, product, arm, object)}); err != nil {
		t.Fatalf("validateIacHints() = %v, want nil", err)
	}
}

func TestValidateIacHints_RejectsBadKnobs(t *testing.T) {
	cases := map[string]struct {
		field *descriptor.Field
		want  []string
	}{
		"example is not JSON": {
			field: makeFieldWithExtension("query", descriptorpb.FieldDescriptorProto_TYPE_STRING, withIac(&options.IacHints{
				Examples: []string{`"fine"`, `source logs`},
			})),
			want: []string{"example.v1.Thing.query", "examples[1]", "not valid JSON", `"source logs"`},
		},
		"example is empty": {
			field: makeFieldWithExtension("query", descriptorpb.FieldDescriptorProto_TYPE_STRING, withIac(&options.IacHints{
				Examples: []string{""},
			})),
			want: []string{"examples[0]", "not valid JSON"},
		},
		"equals is not JSON": {
			field: makeFieldWithExtension("query", descriptorpb.FieldDescriptorProto_TYPE_STRING, withIac(&options.IacHints{
				Requires: []*options.IacHints_Requires{{Field: "other", Equals: "apm"}},
			})),
			want: []string{"example.v1.Thing.query", "requires[0].equals", "not valid JSON", `"apm"`},
		},
		"conflicts_with names a field that does not exist": {
			field: makeFieldWithExtension("query", descriptorpb.FieldDescriptorProto_TYPE_STRING, withIac(&options.IacHints{
				ConflictsWith: []string{"nope"},
			})),
			want: []string{"example.v1.Thing.query", "conflicts_with[0]", `"nope"`, "not a sibling field", "message example.v1.Thing", "other"},
		},
		"conflicts_with names the field itself": {
			field: makeFieldWithExtension("query", descriptorpb.FieldDescriptorProto_TYPE_STRING, withIac(&options.IacHints{
				ConflictsWith: []string{"query"},
			})),
			want: []string{"conflicts_with[0]", `"query" is the field itself`},
		},
		"conflicts_with uses a JSON name": {
			field: makeFieldWithExtension("query", descriptorpb.FieldDescriptorProto_TYPE_STRING, withIac(&options.IacHints{
				ConflictsWith: []string{"otherField"},
			})),
			want: []string{"conflicts_with[0]", `"otherField"`, "Use the proto field name"},
		},
		"requires names a field that does not exist": {
			field: makeFieldWithExtension("query", descriptorpb.FieldDescriptorProto_TYPE_STRING, withIac(&options.IacHints{
				Requires: []*options.IacHints_Requires{{Field: "nope"}},
			})),
			want: []string{"requires[0].field", `"nope"`, "not a sibling field"},
		},
		"requires has an empty field": {
			field: makeFieldWithExtension("query", descriptorpb.FieldDescriptorProto_TYPE_STRING, withIac(&options.IacHints{
				Requires: []*options.IacHints_Requires{{Equals: `"x"`}},
			})),
			want: []string{"requires[0].field", "empty"},
		},
		"arm_fixed on a plain field": {
			field: makeFieldWithExtension("query", descriptorpb.FieldDescriptorProto_TYPE_STRING, withIac(&options.IacHints{
				ArmFixed: true,
			})),
			want: []string{"example.v1.Thing.query", "arm_fixed", "not in a oneof"},
		},
		"arm_fixed on a proto3 optional field": {
			field: withProto3Optional(inOneof(makeFieldWithExtension("query", descriptorpb.FieldDescriptorProto_TYPE_STRING, withIac(&options.IacHints{
				ArmFixed: true,
			})))),
			want: []string{"arm_fixed", "not in a oneof"},
		},
	}
	for name, c := range cases {
		other := makeFieldWithExtension("other", descriptorpb.FieldDescriptorProto_TYPE_STRING, nil)
		err := validateIacHints([]*descriptor.Message{iacMessage(c.field, other)})
		if err == nil {
			t.Errorf("%s: validateIacHints() = nil, want an error", name)
			continue
		}
		for _, part := range c.want {
			if !strings.Contains(err.Error(), part) {
				t.Errorf("%s: error %q does not contain %q", name, err, part)
			}
		}
	}
}

func TestApplyTemplateV3_FailsOnBadIacKnob(t *testing.T) {
	field := makeFieldWithExtension("query", descriptorpb.FieldDescriptorProto_TYPE_STRING, withIac(&options.IacHints{
		ConflictsWith: []string{"nope"},
	}))
	message := iacMessage(field)
	_, err := applyTemplateV3(param{File: &descriptor.File{
		FileDescriptorProto: message.File.FileDescriptorProto,
		Messages:            []*descriptor.Message{message},
	}, reg: descriptor.NewRegistry()})
	if err == nil || !strings.Contains(err.Error(), "conflicts_with[0]") {
		t.Fatalf("applyTemplateV3() error = %v, want the conflicts_with error", err)
	}
}
