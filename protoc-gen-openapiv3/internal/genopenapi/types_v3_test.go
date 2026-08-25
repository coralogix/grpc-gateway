package genopenapi

import (
	"encoding/json"
	"testing"
)

// A schema's x-* extensions (e.g. x-stability, plumbed through from an
// openapiv3_field / openapiv3_enum option) must be flattened onto the emitted
// object. The embedded OpenAPIV3Extensions map is tagged json:"-", so this
// relies on OpenAPIV3Schema.MarshalJSON merging them back in.
func TestOpenAPIV3SchemaMarshalsExtensions(t *testing.T) {
	t.Parallel()

	ref := &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
		Type:                "string",
		OpenAPIV3Extensions: OpenAPIV3Extensions{"x-stability": "preview"},
	}}

	b, err := json.Marshal(ref)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["type"] != "string" {
		t.Errorf("type = %v, want string; json=%s", m["type"], b)
	}
	if m["x-stability"] != "preview" {
		t.Errorf("x-stability = %v, want preview; json=%s", m["x-stability"], b)
	}
}

// A nested property's extensions must flatten too (properties marshal through
// the same OpenAPIV3SchemaRef path), and a schema with no extensions must be
// byte-identical to the plain struct encoding.
func TestOpenAPIV3SchemaMarshalNestedAndEmpty(t *testing.T) {
	t.Parallel()

	parent := &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
		Type: "object",
		Properties: map[string]*OpenAPIV3SchemaRef{
			"fresh": {OpenAPIV3Schema: &OpenAPIV3Schema{
				Type:                "string",
				OpenAPIV3Extensions: OpenAPIV3Extensions{"x-stability": "preview"},
			}},
			"stable": {OpenAPIV3Schema: &OpenAPIV3Schema{Type: "string"}},
		},
	}}

	b, err := json.Marshal(parent)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m struct {
		Properties map[string]map[string]interface{} `json:"properties"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := m.Properties["fresh"]["x-stability"]; got != "preview" {
		t.Errorf("fresh x-stability = %v, want preview; json=%s", got, b)
	}
	if _, ok := m.Properties["stable"]["x-stability"]; ok {
		t.Errorf("stable property unexpectedly carries x-stability; json=%s", b)
	}
}
