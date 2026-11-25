package genopenapi

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/grpc-ecosystem/grpc-gateway/v2/internal/descriptor"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// jsonFieldExtensionNumber is the field number of the
// `com.coralogixapis.common.v1.json_field` FieldOptions extension. The option
// is defined in the API repository, not in this module, so it is read from the
// raw option bytes instead of through a generated Go type.
const jsonFieldExtensionNumber protowire.Number = 5003

// jsonFieldSchemaPath returns the schema path declared by the json_field
// option on field, and whether the option is present.
func jsonFieldSchemaPath(field *descriptor.Field) (string, bool) {
	if field == nil || field.Options == nil {
		return "", false
	}
	// Marshal rather than read GetUnknown: an extension this module does not
	// register is carried as unknown bytes, but marshaling is correct either way.
	unknown, err := proto.Marshal(field.Options)
	if err != nil {
		return "", false
	}
	for len(unknown) > 0 {
		num, typ, n := protowire.ConsumeTag(unknown)
		if n < 0 {
			return "", false
		}
		unknown = unknown[n:]
		if num == jsonFieldExtensionNumber && typ == protowire.BytesType {
			msg, n := protowire.ConsumeBytes(unknown)
			if n < 0 {
				return "", false
			}
			return parseJSONFieldSchema(msg), true
		}
		n = protowire.ConsumeFieldValue(num, typ, unknown)
		if n < 0 {
			return "", false
		}
		unknown = unknown[n:]
	}
	return "", false
}

// parseJSONFieldSchema reads `schema` (field 1) out of a serialized JsonField.
func parseJSONFieldSchema(msg []byte) string {
	for len(msg) > 0 {
		num, typ, n := protowire.ConsumeTag(msg)
		if n < 0 {
			return ""
		}
		msg = msg[n:]
		if num == 1 && typ == protowire.BytesType {
			v, n := protowire.ConsumeBytes(msg)
			if n < 0 {
				return ""
			}
			return string(v)
		}
		n = protowire.ConsumeFieldValue(num, typ, msg)
		if n < 0 {
			return ""
		}
		msg = msg[n:]
	}
	return ""
}

// jsonSiblingName returns the property name of the JSON form of name:
// `policy_json` for snake_case names, `policyJson` for camelCase ones.
func jsonSiblingName(name string) string {
	if strings.Contains(name, "_") {
		return name + "_json"
	}
	return name + "Json"
}

// jsonFieldSibling returns the extra `<field>_json` property for a string field
// carrying the json_field option, or ok=false when the field has no such option.
// The property is an object schema loaded from the declared schema file.
// A bad option or an unreadable/invalid schema stops generation: a silently
// missing JSON property would leave the published contract wrong.
func jsonFieldSibling(field *descriptor.Field, registry *descriptor.Registry) (name string, schema *OpenAPIV3SchemaRef, ok bool) {
	schemaPath, has := jsonFieldSchemaPath(field)
	if !has {
		return "", nil, false
	}
	fqn := field.Message.FQMN() + "." + field.GetName()
	if field.GetType() != descriptorpb.FieldDescriptorProto_TYPE_STRING || field.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED {
		log.Fatalf("json_field on %s: only singular string fields are supported", fqn)
	}
	if schemaPath == "" {
		log.Fatalf("json_field on %s: schema is required", fqn)
	}
	root := registry.GetJSONSchemaRoot()
	if root == "" {
		log.Fatalf("json_field on %s: set the json_schema_root plugin option to the directory the proto imports resolve from", fqn)
	}
	path := filepath.Join(root, filepath.Dir(field.Message.File.GetName()), schemaPath)
	raw, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("json_field on %s: %v", fqn, err)
	}
	var s OpenAPIV3Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		log.Fatalf("json_field on %s: invalid schema %s: %v", fqn, path, err)
	}
	if s.Type == "" {
		s.Type = "object"
	}
	if s.Description == "" {
		s.Description = fmt.Sprintf("Structured JSON form of `%s`, validated against a schema. Set either this or `%s`, not both. It is encoded to a compact JSON string before it reaches the service.", field.GetJsonName(), field.GetJsonName())
	}
	return jsonSiblingName(field.GetName()), &OpenAPIV3SchemaRef{OpenAPIV3Schema: &s}, true
}
