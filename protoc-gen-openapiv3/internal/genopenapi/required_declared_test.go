package genopenapi

import (
	"encoding/json"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/internal/descriptor"
	options "github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv3/options"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// json_schema.required is a repeated field, so "required: []" and no required
// line are the same bytes to the plugin. A present json_schema is the signal
// that the author declared requiredness, and an object with no required field
// must then emit required: [] (not omit the key).

func newDeclaredMessageFixture(t *testing.T, jsonSchema *options.JSONSchema, fieldNames ...string) *descriptor.Message {
	t.Helper()

	fieldType := descriptorpb.FieldDescriptorProto_TYPE_STRING
	label := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	msgOptions := &descriptorpb.MessageOptions{}
	if jsonSchema != nil {
		proto.SetExtension(msgOptions, options.E_Openapiv3Schema, &options.Schema{JsonSchema: jsonSchema})
	}
	msgDesc := &descriptorpb.DescriptorProto{Name: proto.String("Labels"), Options: msgOptions}
	for i, name := range fieldNames {
		msgDesc.Field = append(msgDesc.Field, &descriptorpb.FieldDescriptorProto{
			Name:   proto.String(name),
			Number: proto.Int32(int32(i + 1)),
			Label:  &label,
			Type:   &fieldType,
		})
	}
	return descriptorMessageFromProto(msgDesc)
}

func marshalSchemaObject(t *testing.T, schema *OpenAPIV3Schema) map[string]json.RawMessage {
	t.Helper()
	b, err := json.Marshal(&OpenAPIV3SchemaRef{OpenAPIV3Schema: schema})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal %s: %v", b, err)
	}
	return m
}

func assertRequiredJSON(t *testing.T, schema *OpenAPIV3Schema, want string) {
	t.Helper()
	got, ok := marshalSchemaObject(t, schema)["required"]
	if want == "" {
		if ok {
			t.Fatalf("required = %s, want the key to be omitted", got)
		}
		return
	}
	if !ok {
		t.Fatalf("required key is missing, want %s", want)
	}
	if string(got) != want {
		t.Fatalf("required = %s, want %s", got, want)
	}
}

func TestBuildOpenAPIV3SchemaFromMessage_EmitsEmptyRequiredWhenJSONSchemaDeclared(t *testing.T) {
	msg := newDeclaredMessageFixture(t, &options.JSONSchema{Required: []string{}}, "environment", "team")

	schema, _ := buildOpenAPIV3SchemaFromMessage(msg, nil, map[string]string{msg.FQMN(): "Labels"}, descriptor.NewRegistry())

	assertRequiredJSON(t, schema, "[]")
}

func TestBuildOpenAPIV3SchemaFromMessageWithReferences_EmitsEmptyRequiredWhenJSONSchemaDeclared(t *testing.T) {
	msg := newDeclaredMessageFixture(t, &options.JSONSchema{Title: "Labels"}, "environment", "team")

	schema := buildOpenAPIV3SchemaFromMessageWithReferences(msg, descriptor.NewRegistry(), map[string]string{msg.FQMN(): "Labels"})

	assertRequiredJSON(t, schema, "[]")
}

func TestBuildOpenAPIV3SchemaFromMessage_OmitsRequiredWithoutJSONSchema(t *testing.T) {
	msg := newDeclaredMessageFixture(t, nil, "environment", "team")

	schema, _ := buildOpenAPIV3SchemaFromMessage(msg, nil, map[string]string{msg.FQMN(): "Labels"}, descriptor.NewRegistry())

	assertRequiredJSON(t, schema, "")
}

func TestBuildOpenAPIV3SchemaFromMessage_KeepsListedRequiredFields(t *testing.T) {
	msg := newDeclaredMessageFixture(t, &options.JSONSchema{Required: []string{"environment"}}, "environment", "team")

	schema, _ := buildOpenAPIV3SchemaFromMessage(msg, nil, map[string]string{msg.FQMN(): "Labels"}, descriptor.NewRegistry())

	assertRequiredJSON(t, schema, `["environment"]`)
}

func TestBuildOpenAPIV3SchemaFromMessage_OmitsRequiredForObjectWithoutProperties(t *testing.T) {
	msg := newDeclaredMessageFixture(t, &options.JSONSchema{})

	schema, _ := buildOpenAPIV3SchemaFromMessage(msg, nil, map[string]string{msg.FQMN(): "Labels"}, descriptor.NewRegistry())

	assertRequiredJSON(t, schema, "")
}

func TestBuildRequestBody_EmitsEmptyRequiredWhenJSONSchemaDeclared(t *testing.T) {
	binding := newRequestBodyFixture(t, []string{"name", "kind"}, nil, nil)
	proto.SetExtension(binding.Method.RequestType.Options, options.E_Openapiv3Schema, &options.Schema{JsonSchema: &options.JSONSchema{}})

	body, _ := buildRequestBody(binding, map[string]*OpenAPIV3SchemaRef{}, descriptor.NewRegistry(), map[string]string{})

	assertRequiredJSON(t, body.Content["application/json"].Schema.OpenAPIV3Schema, "[]")
}

func TestBuildRequestBody_OmitsRequiredWithoutJSONSchema(t *testing.T) {
	binding := newRequestBodyFixture(t, []string{"name", "kind"}, nil, nil)

	body, _ := buildRequestBody(binding, map[string]*OpenAPIV3SchemaRef{}, descriptor.NewRegistry(), map[string]string{})

	assertRequiredJSON(t, body.Content["application/json"].Schema.OpenAPIV3Schema, "")
}

// The empty required list must survive next to x-* extensions and a $ref, the
// two other paths that re-encode the schema.
func TestOpenAPIV3SchemaMarshalEmptyRequiredWithExtensionsAndRef(t *testing.T) {
	t.Parallel()

	schema := &OpenAPIV3Schema{
		Type:                "object",
		Properties:          map[string]*OpenAPIV3SchemaRef{"team": {OpenAPIV3Schema: &OpenAPIV3Schema{Type: "string"}}},
		RequiredDeclared:    true,
		OpenAPIV3Extensions: OpenAPIV3Extensions{"x-stability": "preview"},
	}
	for name, ref := range map[string]*OpenAPIV3SchemaRef{
		"inline": {OpenAPIV3Schema: schema},
		"ref":    {Ref: "#/components/schemas/Labels", OpenAPIV3Schema: schema},
	} {
		b, err := json.Marshal(ref)
		if err != nil {
			t.Fatalf("%s: marshal: %v", name, err)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("%s: unmarshal: %v", name, err)
		}
		if string(m["required"]) != "[]" {
			t.Errorf("%s: required = %s, want []; json=%s", name, m["required"], b)
		}
		if string(m["x-stability"]) != `"preview"` {
			t.Errorf("%s: x-stability = %s, want \"preview\"; json=%s", name, m["x-stability"], b)
		}
	}
}
