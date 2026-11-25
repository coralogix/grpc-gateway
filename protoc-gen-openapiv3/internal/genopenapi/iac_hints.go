package genopenapi

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/grpc-ecosystem/grpc-gateway/v2/internal/casing"
	"github.com/grpc-ecosystem/grpc-gateway/v2/internal/descriptor"
	options "github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv3/options"
	"google.golang.org/protobuf/proto"
)

// iacExtension holds the openapiv3_field.iac knobs of a field. The Terraform
// test generator reads it. It changes only the generated tests.
const iacExtension = "x-coralogix-iac"

// fieldIacHints returns the field's openapiv3_field.iac message, or nil.
func fieldIacHints(field *descriptor.Field) *options.IacHints {
	if field.Options == nil || !proto.HasExtension(field.Options, options.E_Openapiv3Field) {
		return nil
	}
	fieldExtension, ok := proto.GetExtension(field.Options, options.E_Openapiv3Field).(*options.JSONSchema)
	if !ok {
		return nil
	}
	return fieldExtension.GetIac()
}

// iacExtensionValue returns the value of the x-coralogix-iac extension for a
// field, or nil when the field has no knob set. Keys are camelCase. Sibling
// field names are JSON names, the same names as the schema properties.
// Examples and `equals` are copied as JSON. validateIacHints checks them.
func iacExtensionValue(field *descriptor.Field) map[string]interface{} {
	hints := fieldIacHints(field)
	if hints == nil {
		return nil
	}
	value := map[string]interface{}{}
	if examples := hints.GetExamples(); len(examples) > 0 {
		raw := make([]RawExample, 0, len(examples))
		for _, example := range examples {
			raw = append(raw, RawExample(example))
		}
		value["examples"] = raw
	}
	if hints.GetRequiredByApi() {
		value["requiredByApi"] = true
	}
	if names := hints.GetConflictsWith(); len(names) > 0 {
		jsonNames := make([]string, 0, len(names))
		for _, name := range names {
			jsonNames = append(jsonNames, casing.JSONCamelCase(name))
		}
		value["conflictsWith"] = jsonNames
	}
	if requires := hints.GetRequires(); len(requires) > 0 {
		entries := make([]map[string]interface{}, 0, len(requires))
		for _, r := range requires {
			entry := map[string]interface{}{"field": casing.JSONCamelCase(r.GetField())}
			if r.GetEquals() != "" {
				entry["equals"] = RawExample(r.GetEquals())
			}
			entries = append(entries, entry)
		}
		value["requires"] = entries
	}
	if hints.GetArmFixed() {
		value["armFixed"] = true
	}
	if len(value) == 0 {
		return nil
	}
	return value
}

// validateIacHints fails when an iac knob in the messages is wrong. The
// generator does not guess: a wrong knob would change the generated tests in
// a way the author did not mean.
func validateIacHints(messages []*descriptor.Message) error {
	for _, message := range messages {
		for _, field := range message.Fields {
			if err := validateFieldIacHints(field); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateFieldIacHints(field *descriptor.Field) error {
	hints := fieldIacHints(field)
	if hints == nil {
		return nil
	}
	where := fmt.Sprintf("openapiv3_field.iac on field %s", strings.TrimPrefix(field.FQFN(), "."))

	for i, example := range hints.GetExamples() {
		if !json.Valid([]byte(example)) {
			return fmt.Errorf("%s: examples[%d] is not valid JSON: %q. Write the value as the API sends it, for example '\"text\"' for a string", where, i, example)
		}
	}
	for i, name := range hints.GetConflictsWith() {
		if err := checkSiblingField(field, name); err != nil {
			return fmt.Errorf("%s: conflicts_with[%d]: %w", where, i, err)
		}
	}
	for i, requires := range hints.GetRequires() {
		if err := checkSiblingField(field, requires.GetField()); err != nil {
			return fmt.Errorf("%s: requires[%d].field: %w", where, i, err)
		}
		if equals := requires.GetEquals(); equals != "" && !json.Valid([]byte(equals)) {
			return fmt.Errorf("%s: requires[%d].equals is not valid JSON: %q. Write the value as the API sends it, for example '\"apm\"' for a string", where, i, equals)
		}
	}
	if hints.GetArmFixed() && !isOneofArm(field) {
		return fmt.Errorf("%s: arm_fixed is set, but the field is not in a oneof. Use arm_fixed only on a field of a oneof", where)
	}
	return nil
}

// checkSiblingField checks that name is another field of the same message.
func checkSiblingField(field *descriptor.Field, name string) error {
	if name == "" {
		return fmt.Errorf("the field name is empty")
	}
	if name == field.GetName() {
		return fmt.Errorf("%q is the field itself. Name a sibling field", name)
	}
	siblings := make([]string, 0, len(field.Message.Fields))
	for _, sibling := range field.Message.Fields {
		if sibling.GetName() == name {
			return nil
		}
		if sibling != field {
			siblings = append(siblings, sibling.GetName())
		}
	}
	slices.Sort(siblings)
	return fmt.Errorf("%q is not a sibling field of message %s. Use the proto field name. The sibling fields are: %s",
		name, strings.TrimPrefix(field.Message.FQMN(), "."), strings.Join(siblings, ", "))
}

// isOneofArm reports whether the field is in a real oneof. A proto3 optional
// field sits in a synthetic oneof, which does not count.
func isOneofArm(field *descriptor.Field) bool {
	return field.OneofIndex != nil && !field.GetProto3Optional()
}
