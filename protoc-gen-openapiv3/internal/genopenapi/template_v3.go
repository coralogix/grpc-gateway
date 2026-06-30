package genopenapi

import (
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"mime"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"slices"

	"github.com/grpc-ecosystem/grpc-gateway/v2/internal/descriptor"
	"github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv3/options"
	"google.golang.org/genproto/googleapis/api/visibility"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

const successStatusCode = "200"

type protoField struct {
	FullPathToField []string
	Field           *descriptor.Field
}

func (p *protoField) isParentOf(maybeChild protoField) bool {
	if len(maybeChild.FullPathToField) < len(p.FullPathToField) {
		return false
	}
	for i, fieldName := range p.FullPathToField {
		if fieldName != maybeChild.FullPathToField[i] {
			return false
		}
	}
	return true
}

var wellKnownTypesToOpenAPIV3SchemaMapping = map[string]*OpenAPIV3Schema{
	".google.protobuf.FieldMask": {
		Type: "string",
	},
	".google.protobuf.Timestamp": {
		Type:   "string",
		Format: "date-time",
	},
	".google.protobuf.Duration": {
		Type: "string",
	},
	".google.protobuf.StringValue": {
		Type: "string",
	},
	".google.protobuf.BytesValue": {
		Type:   "string",
		Format: "byte",
	},
	".google.protobuf.Int32Value": {
		Type:   "integer",
		Format: "int32",
	},
	".google.protobuf.UInt32Value": {
		Type:   "integer",
		Format: "int64",
	},
	".google.protobuf.Int64Value": {
		Type:   "string",
		Format: "int64",
	},
	".google.protobuf.UInt64Value": {
		Type:   "string",
		Format: "uint64",
	},
	".google.protobuf.FloatValue": {
		Type:   "number",
		Format: "float",
	},
	".google.protobuf.DoubleValue": {
		Type:   "number",
		Format: "double",
	},
	".google.protobuf.BoolValue": {
		Type: "boolean",
	},
	".google.protobuf.Empty": {
		Type: "object",
	},
	".google.protobuf.Struct": {
		Type: "object",
	},
	".google.protobuf.Value": {},
	".google.protobuf.ListValue": {
		Type: "array",
		Items: &OpenAPIV3SchemaRef{
			OpenAPIV3Schema: &OpenAPIV3Schema{
				Type: "object",
			},
		},
	},
	".google.protobuf.NullValue": {
		Type: "string",
	},
	".google.protobuf.Any": {
		Type: "object",
	},
	".google.type.Decimal": {
		Type: "object",
		Properties: map[string]*OpenAPIV3SchemaRef{
			"value": {
				OpenAPIV3Schema: &OpenAPIV3Schema{
					Type:        "string",
					Description: "Decimal value encoded as a string, using optional sign, fraction, and exponent notation.",
					MinLength:   uint64Ptr(0),
				},
			},
		},
	},
}

func isGoogleTypeDecimal(f *descriptor.Field) bool {
	return f.TypeName != nil && *f.TypeName == ".google.type.Decimal"
}

func applyDecimalStringOptions(schema *OpenAPIV3Schema, pattern, format string, maxLength, minLength uint64) {
	value := schema.Properties["value"]
	if value == nil || value.OpenAPIV3Schema == nil {
		return
	}
	valueSchema := *value.OpenAPIV3Schema
	valueSchema.Pattern = pattern
	valueSchema.Format = format
	valueSchema.MaxLength = maxLength
	valueSchema.MinLength = uint64Ptr(minLength)

	properties := maps.Clone(schema.Properties)
	properties["value"] = &OpenAPIV3SchemaRef{
		Ref:             value.Ref,
		OpenAPIV3Schema: &valueSchema,
	}
	schema.Properties = properties
}

func routeDecimalObjectExample(jsonExample string, fieldExample, arrayExample RawExample) (RawExample, RawExample) {
	if strings.HasPrefix(strings.TrimSpace(jsonExample), "{") {
		return RawExample(jsonExample), nil
	}
	return fieldExample, arrayExample
}

func openapiTypeCategory(schema *OpenAPIV3Schema) string {
	if schema.Type == "string" {
		return "string"
	} else if schema.Format == "float" {
		return "float"
	} else if schema.Format == "double" {
		return "double"
	} else if schema.Format == "uint64" {
		return "integer"
	} else if schema.Format == "int64" {
		return "integer"
	} else if schema.Type == "integer" {
		return "integer"
	} else if schema.Type == "boolean" {
		return "boolean"
	} else {
		return "object"
	}

}

// stringInt64Pattern returns the digit pattern for a stringified 64-bit int;
// signed types allow a leading minus. An explicit override wins.
func stringInt64Pattern(override string, signed bool) string {
	if override != "" {
		return override
	}
	if signed {
		return "^-?[0-9]+$"
	}
	return "^[0-9]+$"
}

// sanitizeStringIntFormat drops int64/uint64 (integer-only formats) from a
// stringified 64-bit int; any other format override is preserved.
func sanitizeStringIntFormat(format string) string {
	if format == "int64" || format == "uint64" {
		return ""
	}
	return format
}

// stringIntMinLength/stringIntMaxLength bound a stringified 64-bit int to 1..20
// chars (20 = uint64 max digits, or sign + 19 for int64 min). Override wins.
func stringIntMinLength(override uint64) *uint64 {
	if override != 0 {
		return uint64Ptr(override)
	}
	return uint64Ptr(1)
}

func stringIntMaxLength(override uint64) uint64 {
	if override != 0 {
		return override
	}
	return 20
}

// stringIntExample renders a 64-bit int example for the string schema: a scalar
// becomes a quoted decimal integer ("99.00" -> "99"), a flat array becomes an
// array of those. signed=false rejects negatives. Invalid examples error so the
// caller drops them.
func stringIntExample(jsonExample string, signed bool) (string, error) {
	if strings.HasPrefix(strings.TrimSpace(jsonExample), "[") {
		return coerceIntArrayToStrings(jsonExample, signed)
	}
	canon, ok := canonicalStringInt(jsonExample, signed)
	if !ok {
		return "", fmt.Errorf("example %q is not a valid 64-bit integer", jsonExample)
	}
	b, err := json.Marshal(canon)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// coerceIntArrayToStrings turns a flat JSON array of 64-bit ints into an array
// of quoted decimal integers; errors on a non-array or invalid element.
func coerceIntArrayToStrings(example string, signed bool) (string, error) {
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(example), &raw); err != nil {
		return "", err
	}
	out := make([]string, len(raw))
	for i, el := range raw {
		canon, ok := canonicalStringInt(string(el), signed)
		if !ok {
			return "", fmt.Errorf("array example element %q is not a valid 64-bit integer", string(el))
		}
		b, err := json.Marshal(canon)
		if err != nil {
			return "", err
		}
		out[i] = string(b)
	}
	return "[" + strings.Join(out, ",") + "]", nil
}

// canonicalStringInt normalizes a 64-bit int example to decimal form (stripping
// quotes and a zero-only fraction, "99.00" -> "99"). ok=false unless it is a
// valid integer within the 64-bit range (signed int64 / unsigned uint64); the
// original string is returned to preserve full precision.
func canonicalStringInt(example string, signed bool) (string, bool) {
	s := strings.TrimSpace(stripQuotes(strings.TrimSpace(example)))
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		if frac := s[dot+1:]; frac == "" || strings.Trim(frac, "0") != "" {
			return "", false
		}
		s = s[:dot]
	}
	if signed {
		if _, err := strconv.ParseInt(s, 10, 64); err != nil {
			return "", false
		}
	} else if _, err := strconv.ParseUint(s, 10, 64); err != nil {
		return "", false
	}
	return s, true
}

// wellKnownExample coerces a well-known-type example, using the stringified
// 64-bit coercion for Int64Value/UInt64Value (so [1,2] becomes ["1","2"]) and
// validateAndCoerceJsonExample for everything else.
func wellKnownExample(schema *OpenAPIV3Schema, jsonExample, typeCategory string) (string, error) {
	if schema.Type == "string" && (schema.Format == "int64" || schema.Format == "uint64") {
		return stringIntExample(jsonExample, schema.Format == "int64")
	}
	return validateAndCoerceJsonExample(jsonExample, typeCategory)
}

// cleanWellKnownStringInt applies the stringified-64-bit cleanup to an
// Int64Value/UInt64Value wrapper schema (for top-level wrapper responses, which
// bypass the field-level switch); other schemas pass through unchanged.
func cleanWellKnownStringInt(schema *OpenAPIV3Schema) *OpenAPIV3Schema {
	if schema == nil || schema.Type != "string" ||
		(schema.Format != "int64" && schema.Format != "uint64") {
		return schema
	}
	signed := schema.Format == "int64"
	cleaned := *schema
	cleaned.Format = ""
	cleaned.Pattern = stringInt64Pattern("", signed)
	cleaned.MinLength = stringIntMinLength(0)
	cleaned.MaxLength = stringIntMaxLength(0)
	return &cleaned
}

// cleanWellKnownResponseSchema applies the field-level cleanups to a top-level
// well-known-type response (which bypasses the field switch): the stringified
// 64-bit cleanup for Int64Value/UInt64Value, minimum: 0 for the UInt32Value
// integer wrapper, and minLength: 0 for the remaining string wrappers
// (StringValue/BytesValue/FieldMask/Timestamp/Duration). Others pass through.
func cleanWellKnownResponseSchema(schema *OpenAPIV3Schema, fqmn string) *OpenAPIV3Schema {
	if schema != nil && fqmn == ".google.protobuf.UInt32Value" {
		cleaned := *schema
		cleaned.Minimum = float64Ptr(0)
		return &cleaned
	}
	cleaned := cleanWellKnownStringInt(schema)
	// String wrappers also need minLength: 0 (the int64/uint64 branch above
	// already set one). Copy so the shared mapping entry is not mutated.
	if cleaned != nil && cleaned.Type == "string" && cleaned.MinLength == nil {
		c := *cleaned
		c.MinLength = uint64Ptr(0)
		cleaned = &c
	}
	return cleaned
}

// uint64Ptr / float64Ptr build pointers for schema fields that must serialize a
// deliberate 0 (the struct fields use omitempty, which would otherwise drop a
// zero value).
func uint64Ptr(v uint64) *uint64    { return &v }
func float64Ptr(v float64) *float64 { return &v }

// unsignedMinimum returns an annotated unsigned minimum raised to the natural
// lower bound of 0, or emits 0 by default when no annotation is present.
func unsignedMinimum(v *float64) *float64 {
	if v == nil || *v < 0 {
		return float64Ptr(0)
	}
	return float64Ptr(*v)
}

// applyValueSchema merges map field value_schema overrides into the generated
// schema for additionalProperties. The protobuf map value type still determines
// the base OpenAPI type and structural shape; value_schema supplies compatible
// constraints and metadata.
func applyValueSchemaForMapValue(additionalPropertiesSchema *OpenAPIV3SchemaRef, valueSchema *options.JSONSchema, valueField *descriptor.Field, schemaType string) *OpenAPIV3SchemaRef {
	if valueSchema == nil {
		return additionalPropertiesSchema
	}

	schema, ref := valueSchemaMergeTarget(additionalPropertiesSchema, schemaType)
	if schema.Type != "" {
		schemaType = schema.Type
	}

	if valueSchema.Title != "" {
		schema.Title = valueSchema.Title
	}
	if valueSchema.Description != "" {
		schema.Description = valueSchema.Description
	}

	switch schemaType {
	case "integer", "number":
		if valueSchema.MultipleOf != 0 {
			schema.MultipleOf = valueSchema.MultipleOf
		}
		if valueSchema.Maximum != 0 {
			schema.Maximum = valueSchema.Maximum
		}
		if valueSchema.Minimum != nil {
			if schema.Minimum == nil || *valueSchema.Minimum >= *schema.Minimum {
				schema.Minimum = valueSchema.Minimum
			}
		}
		if valueSchema.ExclusiveMaximum {
			schema.ExclusiveMaximum = true
		}
		if valueSchema.ExclusiveMinimum {
			schema.ExclusiveMinimum = true
		}
	case "string":
		if valueSchema.MaxLength != 0 {
			schema.MaxLength = valueSchema.MaxLength
		}
		if valueSchema.MinLength != 0 {
			schema.MinLength = uint64Ptr(valueSchema.MinLength)
		}
		if valueSchema.Pattern != "" {
			schema.Pattern = valueSchema.Pattern
		}
		if mapValueSchemaAllowsFormatOverride(valueField) && valueSchema.Format != "" {
			schema.Format = valueSchema.Format
		}
		if len(valueSchema.Enum) > 0 {
			schema.Enum = slices.Clone(valueSchema.Enum)
		}
	case "array":
		if valueSchema.MaxItems != 0 {
			schema.MaxItems = valueSchema.MaxItems
		}
		if valueSchema.MinItems != 0 {
			schema.MinItems = uint64Ptr(valueSchema.MinItems)
		}
		if valueSchema.UniqueItems {
			schema.UniqueItems = true
		}
	case "object":
		if valueSchema.MaxProperties != 0 {
			schema.MaxProperties = valueSchema.MaxProperties
		}
		if valueSchema.MinProperties != 0 {
			schema.MinProperties = valueSchema.MinProperties
		}
	}
	if valueSchema.ReadOnly {
		schema.ReadOnly = true
	}
	if valueSchema.Example != "" {
		constrainedExample, err := validateAndCoerceJsonExample(valueSchema.Example, schemaType)
		if err == nil && constrainedExample != "" && json.Valid([]byte(constrainedExample)) {
			schema.Example = RawExample(constrainedExample)
		}
	}

	return &OpenAPIV3SchemaRef{
		Ref:             ref,
		OpenAPIV3Schema: &schema,
	}
}

// valueSchemaMergeTarget returns a mutable schema for additionalProperties.
// Ref-only schemas are wrapped with allOf so value_schema fields can be emitted
// alongside the reference.
func valueSchemaMergeTarget(additionalPropertiesSchema *OpenAPIV3SchemaRef, schemaType string) (OpenAPIV3Schema, string) {
	if additionalPropertiesSchema.OpenAPIV3Schema != nil {
		return *additionalPropertiesSchema.OpenAPIV3Schema, additionalPropertiesSchema.Ref
	}
	return OpenAPIV3Schema{
		Type:  schemaType,
		AllOf: []*OpenAPIV3SchemaRef{{Ref: additionalPropertiesSchema.Ref}},
	}, ""
}

func mapValueSchemaAllowsFormatOverride(field *descriptor.Field) bool {
	return *field.Type == descriptorpb.FieldDescriptorProto_TYPE_STRING
}

func mapValueSchemaType(field *descriptor.Field) string {
	switch *field.Type {
	case descriptorpb.FieldDescriptorProto_TYPE_DOUBLE,
		descriptorpb.FieldDescriptorProto_TYPE_FLOAT:
		return "number"
	case descriptorpb.FieldDescriptorProto_TYPE_INT32,
		descriptorpb.FieldDescriptorProto_TYPE_SFIXED32,
		descriptorpb.FieldDescriptorProto_TYPE_SINT32,
		descriptorpb.FieldDescriptorProto_TYPE_UINT32,
		descriptorpb.FieldDescriptorProto_TYPE_FIXED32:
		return "integer"
	case descriptorpb.FieldDescriptorProto_TYPE_INT64,
		descriptorpb.FieldDescriptorProto_TYPE_SFIXED64,
		descriptorpb.FieldDescriptorProto_TYPE_SINT64,
		descriptorpb.FieldDescriptorProto_TYPE_UINT64,
		descriptorpb.FieldDescriptorProto_TYPE_FIXED64:
		return "string"
	case descriptorpb.FieldDescriptorProto_TYPE_BOOL:
		return "boolean"
	case descriptorpb.FieldDescriptorProto_TYPE_STRING,
		descriptorpb.FieldDescriptorProto_TYPE_BYTES,
		descriptorpb.FieldDescriptorProto_TYPE_ENUM:
		return "string"
	case descriptorpb.FieldDescriptorProto_TYPE_MESSAGE,
		descriptorpb.FieldDescriptorProto_TYPE_GROUP:
		if field.TypeName != nil {
			if schema, ok := wellKnownTypesToOpenAPIV3SchemaMapping[*field.TypeName]; ok && schema != nil {
				return schema.Type
			}
		}
		return "object"
	default:
		return ""
	}
}

func applyTemplateV3(param param) (OpenAPIV3Document, error) {
	resolvedNames := resolveNames(param)
	enumSchemas := buildEnumSchemas(param, resolvedNames)
	schemas := buildMessageSchemasWithReferences(param, resolvedNames)
	maps.Copy(schemas, enumSchemas)
	tags, err := buildTags(param)

	if err != nil {
		return OpenAPIV3Document{}, err
	}
	for _, schema := range schemas {
		schema.OpenAPIV3Schema.CamelCase()
	}
	paths, schemasToAddToComponents, err := buildOpenAPIV3Paths(param, resolvedNames)
	maps.Copy(schemas, schemasToAddToComponents)
	if err != nil {
		return OpenAPIV3Document{}, err
	}
	hoistSharedPathParameters(paths)
	openapiDocument := OpenAPIV3Document{
		OpenAPI: "3.0.0",
		Info: &OpenAPIV3Info{
			Version: "1.0.0", // This should be set to the actual version of your API
		},
		Paths: paths,
		Components: &OpenAPIV3Components{
			Schemas: schemas,
		},
		Tags: tags,
	}

	return openapiDocument, nil
}

func resolveNames(param param) map[string]string {
	typeNamesSet := map[string]struct{}{}
	for _, message := range param.Messages {
		typeNamesSet[message.FQMN()] = struct{}{}
	}
	for _, enum := range param.Enums {
		typeNamesSet[enum.FQEN()] = struct{}{}
	}
	statusType, err := param.reg.LookupMsg("google.rpc", "Status")
	if err == nil && statusType != nil {
		typeNamesSet[statusType.FQMN()] = struct{}{}
	}
	statusCodeType, err := param.reg.LookupEnum("google.rpc", "Code")
	if err == nil && statusCodeType != nil {
		typeNamesSet[statusCodeType.FQEN()] = struct{}{}
	}
	typeNames := []string{}
	for typeName := range typeNamesSet {
		typeNames = append(typeNames, typeName)
	}
	if param.reg.GetOpenAPINamingStrategy() == "fqn" {
		return resolveNamesFQN(typeNames)
	} else {
		return resolveNamesSimple(typeNames)
	}
}

// pathMethodKey represents a unique combination of HTTP path and method for duplicate detection.
type pathMethodKey struct {
	path   string
	method string
}

// pathMethodSource tracks the source RPC that registered a particular path + method combination.
type pathMethodSource struct {
	serviceName string
	methodName  string
	bindingIdx  int
}

// checkDuplicatePath checks if a path + method combination has already been registered.
// Returns an error if a duplicate is found.
func checkDuplicatePath(
	registeredPaths map[pathMethodKey]pathMethodSource,
	path, httpMethod, serviceName, methodName string,
	bindingIdx int,
) error {
	key := pathMethodKey{path: path, method: httpMethod}
	if existing, found := registeredPaths[key]; found {
		return fmt.Errorf(
			"duplicate HTTP path and method: %s %s is defined by both %s.%s (binding %d) and %s.%s (binding %d)",
			httpMethod, path,
			existing.serviceName, existing.methodName, existing.bindingIdx,
			serviceName, methodName, bindingIdx,
		)
	}
	registeredPaths[key] = pathMethodSource{
		serviceName: serviceName,
		methodName:  methodName,
		bindingIdx:  bindingIdx,
	}
	return nil
}

func buildOpenAPIV3Paths(param param, resolvedNames map[string]string) (OpenAPIV3Paths, map[string]*OpenAPIV3SchemaRef, error) {
	paths := OpenAPIV3Paths{}
	schemasToAddToComponents := map[string]*OpenAPIV3SchemaRef{}

	// Reference for the built-in default error body schema (defaultErrorSchema),
	// attached to description-only error responses so they carry a documented
	// JSON body — satisfying both ibm-content-contains-schema (content must have
	// a schema) and ibm-request-and-response-content (non-204 responses should
	// have content). Skipped when disable_default_errors is set, so a service
	// that opted out keeps those responses bodyless; also skipped (with a
	// warning) if a proto type already claims the "Error" component name.
	var errorSchemaRef string
	if !param.reg.GetDisableDefaultErrors() {
		if errorComponentReserved(resolvedNames) {
			log.Printf("Warning: a proto type already uses the %q schema name; skipping the built-in error schema for description-only error responses", defaultErrorSchemaName)
		} else {
			errorSchemaRef = "#/components/schemas/" + defaultErrorSchemaName
		}
	}

	// Track registered path + method combinations to detect duplicates
	registeredPaths := make(map[pathMethodKey]pathMethodSource)

	for _, svc := range param.Services {
		if !isVisible(getServiceVisibilityOption(svc), param.reg) {
			continue
		}
		for _, m := range svc.Methods {
			if !isVisible(getMethodVisibilityOption(m), param.reg) {
				continue
			}
			var mainBinding *descriptor.Binding
			var bindings []*descriptor.Binding

			for _, b := range m.Bindings {
				if b.Index == 0 {
					mainBinding = b
				}
			}

			if param.reg.IsIgnoreAdditionalBindings() && mainBinding != nil {
				bindings = []*descriptor.Binding{mainBinding}

			} else {
				bindings = m.Bindings
			}
			for _, b := range bindings {
				tags := []string{}
				summary := m.GetName()
				operationID := fmt.Sprintf("%s_%s", svc.GetName(), m.GetName())
				deprecated := false
				responses := OpenAPIV3Responses{}
				externalDocs := &OpenAPIV3ExternalDocs{}
				extensions := OpenAPIV3Extensions{}
				var description string
				var successResponseExamples map[string]string
				if proto.HasExtension(m.Options, options.E_Openapiv3Operation) {
					operation, ok := proto.GetExtension(m.Options, options.E_Openapiv3Operation).(*options.Operation)
					if ok {
						tags = operation.Tags
						if operation.Summary != "" {
							summary = operation.Summary
						}
						if operation.OperationId != "" {
							operationID = operation.OperationId
						}
						if operation.Description != "" {
							description = operation.Description
						}
						if operation.Deprecated {
							deprecated = true
						}
						for k, v := range operation.Extensions {
							extensions[k] = v
						}
						responses = extractOpenAPIV3ResponsesFromProtoExtension(operation, errorSchemaRef)
						if successResp, ok := operation.GetResponses()[successStatusCode]; ok && successResp != nil {
							successResponseExamples = successResp.GetExamples()
						}
						if operation.ExternalDocs != nil && operation.ExternalDocs.Description != "" && operation.ExternalDocs.Url != "" {
							externalDocs = &OpenAPIV3ExternalDocs{
								Description: operation.ExternalDocs.Description,
								URL:         operation.ExternalDocs.Url,
							}
						}
					}
				}
				path := applyPathParamRenames(sanitizeURLPath(b.PathTmpl.Template), buildPathParamRenames(b, param.reg))
				httpMethod := b.HTTPMethod

				// Check for duplicate path + method combination
				if err := checkDuplicatePath(registeredPaths, path, httpMethod, svc.GetName(), m.GetName(), b.Index); err != nil {
					return nil, nil, err
				}

				// Ensure the path item exists
				pathItem, ok := paths[path]
				if !ok {
					pathItem = &OpenAPIV3PathItem{}
					paths[path] = pathItem
				}

				schemaMap, messageOneOfSchemas := buildMessageSchemas(param, resolvedNames)
				requestBody, bodyOneOfSchemas := buildRequestBody(b, schemaMap, param.reg, resolvedNames)
				pathParameters := buildPathParameters(b, param.reg, resolvedNames)
				queryParameters := buildQueryParameters(b, schemaMap, resolvedNames, param.reg)
				parameters := append(pathParameters, queryParameters...)
				if requestBody != nil {
					requestBody.OpenAPIV3RequestBody.Content["application/json"].Schema.OpenAPIV3Schema.CamelCase()
				}
				responseBody := buildResponseBody(b, param.reg, resolvedNames)
				if responseBody != nil {
					responseBody.OpenAPIV3Response.Content["application/json"].Schema.OpenAPIV3Schema.CamelCase()
					applyResponseExamples(responseBody.OpenAPIV3Response, successResponseExamples)
				}
				responses[successStatusCode] = *responseBody
				op := &OpenAPIV3Operation{
					Summary:             summary,
					OperationID:         operationID,
					Description:         description,
					Parameters:          parameters,
					RequestBody:         requestBody,
					Deprecated:          deprecated,
					Tags:                tags,
					Responses:           responses,
					OpenAPIV3Extensions: extensions,
					ExternalDocs:        externalDocs,
				}

				switch httpMethod {
				case "GET":
					pathItem.Get = op
				case "POST":
					pathItem.Post = op
				case "PUT":
					pathItem.Put = op
				case "DELETE":
					pathItem.Delete = op
				case "PATCH":
					pathItem.Patch = op
				case "OPTIONS":
					pathItem.Options = op
				case "HEAD":
					pathItem.Head = op
				case "TRACE":
					pathItem.Trace = op
				}
				maps.Copy(schemasToAddToComponents, bodyOneOfSchemas)
				maps.Copy(schemasToAddToComponents, messageOneOfSchemas)
			}
		}
	}
	return paths, schemasToAddToComponents, nil
}

// hoistSharedPathParameters moves a path parameter to the path-item level when
// it is defined identically across every operation on a path that has more than
// one operation, then removes it from each operation. This avoids repeating the
// same path parameter on each operation (ibm-avoid-repeating-path-parameters).
// Differing definitions and query parameters are left untouched.
func hoistSharedPathParameters(paths OpenAPIV3Paths) {
	for _, pathItem := range paths {
		ops := pathItem.operations()
		if len(ops) < 2 {
			continue
		}
		// A path parameter is hoistable iff every operation defines it and all
		// copies are identical. Iterate the first operation's path params so the
		// hoisted order is deterministic.
		for _, candidate := range ops[0].Parameters {
			if candidate.OpenAPIV3Parameter == nil || candidate.In != "path" {
				continue
			}
			name := candidate.Name
			shared := true
			for _, op := range ops[1:] {
				if match := findPathParameter(op.Parameters, name); match == nil || !reflect.DeepEqual(*match, candidate) {
					shared = false
					break
				}
			}
			if !shared {
				continue
			}
			pathItem.Parameters = append(pathItem.Parameters, candidate)
			for _, op := range ops {
				op.Parameters = removePathParameter(op.Parameters, name)
			}
		}
	}
}

// findPathParameter returns the path parameter with the given name, or nil.
func findPathParameter(params []OpenAPIV3ParameterRef, name string) *OpenAPIV3ParameterRef {
	for i := range params {
		if params[i].OpenAPIV3Parameter != nil && params[i].In == "path" && params[i].Name == name {
			return &params[i]
		}
	}
	return nil
}

// removePathParameter returns params without the named path parameter, keeping
// the order of the remaining entries.
func removePathParameter(params []OpenAPIV3ParameterRef, name string) []OpenAPIV3ParameterRef {
	filtered := make([]OpenAPIV3ParameterRef, 0, len(params))
	for _, p := range params {
		if p.OpenAPIV3Parameter != nil && p.In == "path" && p.Name == name {
			continue
		}
		filtered = append(filtered, p)
	}
	return filtered
}

func sanitizeURLPath(urlPath string) string {
	segments := strings.Split(urlPath, "/")

	var sanitizedSegments []string

	for _, segment := range segments {
		if segment == "" {
			sanitizedSegments = append(sanitizedSegments, segment)
			continue
		}

		parts := strings.Split(segment, ".")
		partPrefix := ""
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") && len(parts) > 1 {
			partPrefix = "{"
		}

		if len(parts) > 0 {
			lastPart := parts[len(parts)-1]
			sanitizedSegments = append(sanitizedSegments, partPrefix+lastPart)
		} else {
			sanitizedSegments = append(sanitizedSegments, "")
		}
	}
	return strings.Join(sanitizedSegments, "/")
}

func buildPathParamRenames(binding *descriptor.Binding, registry *descriptor.Registry) map[string]string {
	renames := make(map[string]string)
	for _, param := range binding.PathParams {
		paramName := param.FieldPath[len(param.FieldPath)-1].Target.Name
		field := param.Target
		if fc := getFieldConfiguration(registry, field); fc != nil {
			if name := fc.GetPathParamName(); name != "" && name != *paramName {
				renames["{"+*paramName+"}"] = "{" + name + "}"
			}
		}
	}
	return renames
}

func applyPathParamRenames(path string, renames map[string]string) string {
	for original, renamed := range renames {
		path = strings.ReplaceAll(path, original, renamed)
	}
	return path
}

var jsonSchemaSimpleTypeToString = map[options.JSONSchema_JSONSchemaSimpleTypes]string{
	options.JSONSchema_ARRAY:   "array",
	options.JSONSchema_BOOLEAN: "boolean",
	options.JSONSchema_INTEGER: "integer",
	options.JSONSchema_NULL:    "null",
	options.JSONSchema_NUMBER:  "number",
	options.JSONSchema_OBJECT:  "object",
	options.JSONSchema_STRING:  "string",
}

// inlineResponseSchema renders an annotated non-$ref response schema (e.g.
// json_schema: {type: STRING}) so the body contract is preserved.
func inlineResponseSchema(js *options.JSONSchema) *OpenAPIV3Schema {
	s := &OpenAPIV3Schema{
		Title:            js.Title,
		Description:      js.Description,
		Format:           js.Format,
		Pattern:          js.Pattern,
		Enum:             js.Enum,
		Required:         js.Required,
		Maximum:          js.Maximum,
		Minimum:          js.Minimum,
		ExclusiveMaximum: js.ExclusiveMaximum,
		ExclusiveMinimum: js.ExclusiveMinimum,
		MultipleOf:       js.MultipleOf,
		MaxLength:        js.MaxLength,
		MaxItems:         js.MaxItems,
		UniqueItems:      js.UniqueItems,
		MaxProperties:    js.MaxProperties,
		MinProperties:    js.MinProperties,
		ReadOnly:         js.ReadOnly,
	}
	if len(js.Type) > 0 {
		s.Type = jsonSchemaSimpleTypeToString[js.Type[0]]
	}
	// MinLength/MinItems serialize as pointers so a deliberate 0 is kept; the
	// proto scalar can't distinguish unset from 0, so emit only when non-zero.
	if js.MinLength != 0 {
		s.MinLength = uint64Ptr(js.MinLength)
	}
	if js.MinItems != 0 {
		s.MinItems = uint64Ptr(js.MinItems)
	}
	if js.Default != "" {
		s.Default = RawExample(js.Default)
	}
	if js.Example != "" {
		s.Example = RawExample(js.Example)
	}
	return s
}

// defaultErrorSchemaName is the components/schemas key for the built-in error
// body schema referenced by description-only error responses.
const defaultErrorSchemaName = "Error"

// errorComponentReserved reports whether a proto-derived type already resolved
// to the built-in error schema's component name. When true, the built-in must
// not be emitted or referenced, so a real type with that name is never
// clobbered and error responses never point at an unrelated model.
func errorComponentReserved(resolvedNames map[string]string) bool {
	for _, name := range resolvedNames {
		if name == defaultErrorSchemaName {
			return true
		}
	}
	return false
}

// defaultErrorSchema is the built-in error body schema ({ code, message })
// attached to description-only 4xx/5xx responses. It is self-contained and
// fully annotated so it documents a JSON contract and passes the IBM
// schema-quality rules without depending on google.rpc.Status.
func defaultErrorSchema() *OpenAPIV3Schema {
	return &OpenAPIV3Schema{
		Type:        "object",
		Description: "Standard error response body returned for a failed request.",
		Properties: map[string]*OpenAPIV3SchemaRef{
			"code": {OpenAPIV3Schema: &OpenAPIV3Schema{
				Type:        "integer",
				Format:      "int32",
				Description: "HTTP status code of the error (for example 400, 404, 500).",
				Minimum:     float64Ptr(100),
				Maximum:     599,
			}},
			"message": {OpenAPIV3Schema: &OpenAPIV3Schema{
				Type:        "string",
				Description: "Human-readable description of the error.",
				MinLength:   uint64Ptr(0),
				MaxLength:   4096,
				Pattern:     "^[\\s\\S]*$",
			}},
		},
	}
}

// isErrorStatusCode reports whether an OpenAPI response status code key denotes
// an error (4xx/5xx). Non-numeric keys (e.g. "default", "4XX") are treated as
// non-errors so they keep their explicit shape.
func isErrorStatusCode(statusCode string) bool {
	if code, err := strconv.Atoi(statusCode); err == nil {
		return code >= 400
	}
	// OpenAPI also allows class-level range keys like "4XX"/"5XX".
	switch strings.ToUpper(statusCode) {
	case "4XX", "5XX":
		return true
	}
	return false
}

func extractOpenAPIV3ResponsesFromProtoExtension(operation *options.Operation, errorSchemaRef string) OpenAPIV3Responses {
	responses := OpenAPIV3Responses{}
	for statusCode, response := range operation.Responses {
		if response != nil {
			if statusCode != successStatusCode {
				var content map[string]OpenAPIV3MediaType
				// Pick the response body schema:
				//   - an explicit annotation schema ($ref or inline) wins;
				//   - otherwise a description-only error response (4xx/5xx) uses
				//     the built-in default error schema (errorSchemaRef) so it
				//     still documents a JSON body. Without this, an empty media
				//     type trips ibm-content-contains-schema while no content at
				//     all trips ibm-request-and-response-content.
				// Non-error description-only responses (e.g. 201) and 204 stay
				// bodyless. Examples, if any, are added by applyResponseExamples.
				if js := response.Schema.GetJsonSchema(); js != nil {
					var schemaRef *OpenAPIV3SchemaRef
					if js.Ref != "" {
						schemaRef = &OpenAPIV3SchemaRef{Ref: "#/components/schemas/" + js.Ref}
					} else {
						schemaRef = &OpenAPIV3SchemaRef{OpenAPIV3Schema: inlineResponseSchema(js)}
					}
					content = map[string]OpenAPIV3MediaType{"application/json": {Schema: schemaRef}}
				} else if errorSchemaRef != "" && isErrorStatusCode(statusCode) {
					content = map[string]OpenAPIV3MediaType{"application/json": {Schema: &OpenAPIV3SchemaRef{Ref: errorSchemaRef}}}
				}
				headers := make(map[string]OpenAPIV3HeaderRef)
				for headerName, header := range response.Headers {
					if header == nil {
						continue
					}
					headers[headerName] = OpenAPIV3HeaderRef{
						Header: &OpenAPIV3Header{
							Description: header.Description,
							Style:       "simple",
							Schema: &OpenAPIV3SchemaRef{
								OpenAPIV3Schema: &OpenAPIV3Schema{
									Type: header.Type,
								},
							},
						},
					}
				}
				respObj := &OpenAPIV3Response{
					Description: response.Description,
					Headers:     headers,
					Content:     content,
				}
				applyResponseExamples(respObj, response.GetExamples())
				responses[statusCode] = OpenAPIV3ResponseRef{
					OpenAPIV3Response: respObj,
				}
			} else {
				// The 200 response is reserved for the main response body
				continue
			}
		}
	}
	return responses
}

// mediaTypeExampleValue converts a per-mime-type example string from the
// openapiv3 Response.examples annotation into a value suitable for OpenAPI v3
// MediaType.example. JSON-flavored mime types preserve the original JSON shape
// via RawExample so generated specs round-trip the dev's annotated structure
// (objects stay objects, numbers stay numbers, etc.). All other mime types are
// emitted as plain strings. Behaves like protoc-gen-openapiv2's
// openapiExamplesFromProtoExamples so v2 and v3 surface the same example data
// for the same proto annotations.
func mediaTypeExampleValue(mimeType, exampleStr string) interface{} {
	if isJSONMediaType(mimeType) {
		return RawExample(exampleStr)
	}
	return exampleStr
}

// isJSONMediaType returns true for application/json and any structured-syntax
// suffix variant (RFC 6838 §4.2.8, e.g. application/problem+json,
// application/cloudevents+json). Uses mime.ParseMediaType so casing and
// charset/parameter suffixes don't matter — "Application/JSON" and
// "application/json; charset=utf-8" both qualify, matching RFC 9110's
// case-insensitive media-type semantics. Falls back to a literal lowercased
// comparison on parse failure so a malformed-but-recognizable annotation
// doesn't silently regress to "always string".
func isJSONMediaType(mediaType string) bool {
	parsed, _, err := mime.ParseMediaType(mediaType)
	if err != nil {
		parsed = strings.ToLower(strings.TrimSpace(mediaType))
	}
	return parsed == "application/json" || strings.HasSuffix(parsed, "+json")
}

// applyResponseExamples merges per-mime-type examples from a proto Response.examples
// annotation onto an OpenAPI v3 response's content map. Existing content entries
// keep their schema and get their Example set; mime types without an entry yet
// (e.g. an "application/xml" annotation on a JSON-only response) get a fresh
// entry created so the example is still surfaced.
func applyResponseExamples(resp *OpenAPIV3Response, examples map[string]string) {
	if resp == nil || len(examples) == 0 {
		return
	}
	if resp.Content == nil {
		resp.Content = make(map[string]OpenAPIV3MediaType)
	}
	for mimeType, exampleStr := range examples {
		mt := resp.Content[mimeType]
		mt.Example = mediaTypeExampleValue(mimeType, exampleStr)
		resp.Content[mimeType] = mt
	}
}

func buildTags(param param) ([]OpenAPIV3Tag, error) {
	openApiV3TagSet := map[string]OpenAPIV3Tag{}
	for _, svc := range param.Services {
		if !proto.HasExtension(svc.Options, options.E_Openapiv3Tag) {
			continue
		} else {
			tag_extension := proto.GetExtension(svc.Options, options.E_Openapiv3Tag)
			tag, ok := tag_extension.(*options.Tag)
			if !ok {
				return nil, fmt.Errorf("you have added an extension of type %T to rpc service %s, but only extensions of type Tag are allowed", tag_extension, svc.GetName())
			}
			openapiV3Tag := OpenAPIV3Tag{
				Name:        tag.GetName(),
				Description: tag.GetDescription(),
				ExternalDocs: &OpenAPIV3ExternalDocs{
					Description: tag.GetExternalDocs().GetDescription(),
					URL:         tag.GetExternalDocs().GetUrl(),
				},
			}
			openApiV3TagSet[tag.GetName()] = openapiV3Tag
		}
	}
	openapiV3Tags := make([]OpenAPIV3Tag, 0, len(openApiV3TagSet))
	for _, tag := range openApiV3TagSet {
		openapiV3Tags = append(openapiV3Tags, tag)
	}
	// Sort tags by name so the generated spec is deterministic: the tags are
	// collected into a map above, and Go randomizes map iteration order, which
	// would otherwise shuffle the top-level `tags` array on every generation.
	slices.SortFunc(openapiV3Tags, func(a, b OpenAPIV3Tag) int {
		return strings.Compare(a.Name, b.Name)
	})
	return openapiV3Tags, nil
}

func buildResponseBody(binding *descriptor.Binding, registry *descriptor.Registry, resolvedNames map[string]string) *OpenAPIV3ResponseRef {
	if binding.Method.ResponseType == nil {
		return nil
	}
	var targetField *descriptor.Field
	if binding.ResponseBody != nil && len(binding.ResponseBody.FieldPath) > 0 {
		targetField = binding.ResponseBody.FieldPath[len(binding.ResponseBody.FieldPath)-1].Target
	}
	responseContent := make(map[string]OpenAPIV3MediaType)
	if targetField == nil {
		if schema, ok := wellKnownTypesToOpenAPIV3SchemaMapping[binding.Method.ResponseType.FQMN()]; ok {
			// A top-level wrapper response bypasses the field-level switch, so
			// apply the same cleanups here (string-int format/pattern/length and
			// the UInt32Value minimum: 0).
			responseContent["application/json"] = OpenAPIV3MediaType{
				Schema: &OpenAPIV3SchemaRef{
					OpenAPIV3Schema: cleanWellKnownResponseSchema(schema, binding.Method.ResponseType.FQMN()),
				},
			}
		} else {
			responseContent["application/json"] = OpenAPIV3MediaType{
				Schema: &OpenAPIV3SchemaRef{
					Ref: "#/components/schemas/" + resolvedNames[binding.Method.ResponseType.FQMN()],
				},
			}
		}
	} else {
		schema := buildPropertySchemaWithReferencesFromField(targetField, registry, resolvedNames)
		if schema == nil {
			responseContent["application/json"] = OpenAPIV3MediaType{}
		}
		responseContent["application/json"] = OpenAPIV3MediaType{
			Schema: schema,
		}
	}
	return &OpenAPIV3ResponseRef{
		OpenAPIV3Response: &OpenAPIV3Response{
			Content: responseContent,
		},
	}
}

// fieldDescription reads openapiv3_field.description from a proto field so it
// can be surfaced on the OpenAPI parameter (not only on the schema).
func fieldDescription(field *descriptor.Field) string {
	if field == nil || field.Options == nil {
		return ""
	}
	if !proto.HasExtension(field.Options, options.E_Openapiv3Field) {
		return ""
	}
	ext, ok := proto.GetExtension(field.Options, options.E_Openapiv3Field).(*options.JSONSchema)
	if !ok || ext == nil {
		return ""
	}
	return ext.Description
}

func buildPathParameters(binding *descriptor.Binding, registry *descriptor.Registry, resolvedNames map[string]string) []OpenAPIV3ParameterRef {
	parameterRefs := []OpenAPIV3ParameterRef{}
	for _, param := range binding.PathParams {
		paramName := param.FieldPath[len(param.FieldPath)-1].Target.Name
		field := param.Target
		if !isVisible(getFieldVisibilityOption(field), registry) {
			continue
		}
		pathParamName := *paramName
		if fc := getFieldConfiguration(registry, field); fc != nil {
			if name := fc.GetPathParamName(); name != "" {
				pathParamName = name
			}
		}
		fieldOpenApiV3Schema := buildPropertySchemaWithReferencesFromField(field, registry, resolvedNames)
		if fieldOpenApiV3Schema != nil {
			parameterRef := OpenAPIV3ParameterRef{
				OpenAPIV3Parameter: &OpenAPIV3Parameter{
					Name:        pathParamName,
					In:          "path",
					Required:    true,
					Description: fieldDescription(field),
					Schema:      fieldOpenApiV3Schema,
				},
			}
			parameterRefs = append(parameterRefs, parameterRef)
		}
	}
	return parameterRefs
}

func buildQueryParameters(binding *descriptor.Binding, schemaMap map[string]*OpenAPIV3SchemaRef, resolvedNames map[string]string, registry *descriptor.Registry) []OpenAPIV3ParameterRef {
	if binding.Body != nil && len(binding.Body.FieldPath) == 0 {
		return []OpenAPIV3ParameterRef{}
	}
	parameterRefs := []OpenAPIV3ParameterRef{}
	message, err := registry.LookupMsg(*binding.Method.InputType, *binding.Method.InputType)
	if err != nil {
	}
	for _, field := range message.Fields {
		if !isVisible(getFieldVisibilityOption(field), registry) {
			continue
		}
		shouldSkipField := false
		fieldPathsAlreadyIncludedInBodyOrPathParameters := [][]string{}
		for _, pathParameter := range binding.PathParams {
			if *field.Name == pathParameter.FieldPath[0].Name {
				shouldSkipField = len(pathParameter.FieldPath) == 1
				if !shouldSkipField {
					fieldPathToRemove := []string{}
					for index, pathParameterFieldPathComponent := range pathParameter.FieldPath {
						if index > 0 {
							fieldPathToRemove = append(fieldPathToRemove, pathParameterFieldPathComponent.Name)
						}
					}
					fieldPathsAlreadyIncludedInBodyOrPathParameters = append(fieldPathsAlreadyIncludedInBodyOrPathParameters, fieldPathToRemove)
				}
			}
		}
		if binding.Body != nil {
			fieldPathToRemove := []string{}
			if *field.Name == binding.Body.FieldPath[0].Name {
				shouldSkipField = len(binding.Body.FieldPath) == 1
				if !shouldSkipField {
					for index, pathParameterFieldPathComponent := range binding.Body.FieldPath {
						if index > 0 {
							fieldPathToRemove = append(fieldPathToRemove, pathParameterFieldPathComponent.Name)
						}
					}
					fieldPathsAlreadyIncludedInBodyOrPathParameters = append(fieldPathsAlreadyIncludedInBodyOrPathParameters, fieldPathToRemove)
				}
			}
		}
		if shouldSkipField {
			continue
		}

		queryParameterSchema := buildPropertySchemaFromField(field, schemaMap, resolvedNames, registry)
		if queryParameterSchema == nil {
			continue
		}
		// This means we're dealing with an enum, so we can just create a reference parameter.
		if queryParameterSchema.Ref != "" {
			parameterRef := OpenAPIV3ParameterRef{
				OpenAPIV3Parameter: &OpenAPIV3Parameter{
					Name:        *field.Name,
					In:          "query",
					Required:    false,
					Description: fieldDescription(field),
					Schema:      queryParameterSchema,
				},
			}
			parameterRefs = append(parameterRefs, parameterRef)
			continue
		}
		// Follow the path of the field to remove, and remove it from the body schema
		if len(queryParameterSchema.Properties) > 0 {
			properties := &queryParameterSchema.Properties
			fieldSchemaRequiredFields := &queryParameterSchema.Required
			for _, fieldPathToRemove := range fieldPathsAlreadyIncludedInBodyOrPathParameters {
				pathMinusField := fieldPathToRemove[:len(fieldPathToRemove)-1]

				for _, pathComponent := range pathMinusField {
					if properties == nil || (*properties)[pathComponent] == nil || (*properties)[pathComponent].Properties == nil {
						continue
					}
					fieldSchemaRequiredFields = &(*properties)[pathComponent].Required
					properties = &(*properties)[pathComponent].Properties
				}
				for requiredFieldIndex, requiredField := range *fieldSchemaRequiredFields {
					if requiredField == fieldPathToRemove[len(fieldPathToRemove)-1] {
						// If the field to remove is required, we need to remove it from the required fields list.
						*fieldSchemaRequiredFields = slices.Delete((*fieldSchemaRequiredFields), requiredFieldIndex, requiredFieldIndex+1)
						break
					}
				}
				delete(*properties, fieldPathToRemove[len(fieldPathToRemove)-1])
			}
			// It is possible that the field schema has no properties left after removing the fields,
			if len(queryParameterSchema.Properties) == 0 {
				continue
			}
		}
		parameterRef := OpenAPIV3ParameterRef{
			OpenAPIV3Parameter: &OpenAPIV3Parameter{
				Name:        *field.Name,
				In:          "query",
				Required:    false,
				Description: fieldDescription(field),
				Schema:      queryParameterSchema,
			},
		}
		parameterRefs = append(parameterRefs, parameterRef)
	}
	return parameterRefs
}

func buildRequestBody(binding *descriptor.Binding, schemaMap map[string]*OpenAPIV3SchemaRef, registry *descriptor.Registry, resolvedNames map[string]string) (*OpenAPIV3RequestBodyRef, map[string]*OpenAPIV3SchemaRef) {
	if binding.Body == nil {
		return nil, map[string]*OpenAPIV3SchemaRef{}
	}
	schemasToAddToComponents := map[string]*OpenAPIV3SchemaRef{}
	bodyRepresentation := extractRequestBodyFieldCombinations(binding, registry, resolvedNames)
	parameterFields := extractParameterFields(binding)
	oneOfSchemas := map[string]*OpenAPIV3SchemaRef{}
	for combinationName, bodyFields := range bodyRepresentation.fieldCombinations {
		bodyProperties := make(map[string]*OpenAPIV3SchemaRef)
		for _, bodyField := range bodyFields {
			if !isVisible(getFieldVisibilityOption(bodyField.Field), registry) {
				continue
			}
			fieldsToRemoveFromBody := []protoField{}
			for _, parameterField := range parameterFields {
				if bodyField.isParentOf(parameterField) {
					fieldsToRemoveFromBody = append(fieldsToRemoveFromBody, parameterField)
				}
			}

			if len(fieldsToRemoveFromBody) > 0 {
				if *bodyField.Field.Type != descriptorpb.FieldDescriptorProto_TYPE_MESSAGE || wellKnownTypesToOpenAPIV3SchemaMapping[*bodyField.Field.TypeName] != nil {
					// The field is of a primitive type, and it's already passed through
					// a url parameter, so we can skip it.
					continue
				}
				fieldMessage, err := registry.LookupMsg(*bodyField.Field.TypeName, *bodyField.Field.TypeName)
				if err != nil || fieldMessage == nil {
					log.Printf("Warning: field %s has no message type", *bodyField.Field.Name)
					return nil, map[string]*OpenAPIV3SchemaRef{}
				}
				fieldSchema, messageOneOfSchemas := buildOpenAPIV3SchemaFromMessage(fieldMessage, schemaMap, resolvedNames, registry)
				maps.Copy(schemasToAddToComponents, messageOneOfSchemas)
				// Follow the path of the field to remove, and remove it from the body schema
				if len(fieldSchema.Properties) > 0 {
					properties := &fieldSchema.Properties
					fieldSchemaRequiredFields := &fieldSchema.Required
					for _, fieldToRemove := range fieldsToRemoveFromBody {
						pathMinusField := fieldToRemove.FullPathToField[:len(fieldToRemove.FullPathToField)-1]
						for _, pathComponent := range pathMinusField {
							if (*properties)[pathComponent] == nil || (*properties)[pathComponent].Properties == nil {
								continue
							}
							fieldSchemaRequiredFields = &(*properties)[pathComponent].Required
							properties = &(*properties)[pathComponent].Properties
						}
						for requiredFieldIndex, requiredField := range *fieldSchemaRequiredFields {
							if requiredField == *fieldToRemove.Field.Name {
								// If the field to remove is required, we need to remove it from the required fields list.
								*fieldSchemaRequiredFields = append((*fieldSchemaRequiredFields)[:requiredFieldIndex], (*fieldSchemaRequiredFields)[requiredFieldIndex+1:]...)
								break
							}
						}
						delete(*properties, *fieldToRemove.Field.Name)
					}
					// It is possible that the field schema has no properties left after removing the fields,
					if len(fieldSchema.Properties) == 0 {
						continue
					}
					bodyProperties[*bodyField.Field.Name] = &OpenAPIV3SchemaRef{
						OpenAPIV3Schema: fieldSchema,
					}
				}
			} else {
				propertySchema := buildPropertySchemaWithReferencesFromField(bodyField.Field, registry, resolvedNames)
				if propertySchema != nil {
					bodyProperties[*bodyField.Field.Name] = propertySchema
				}
			}
		}
		if len(bodyProperties) > 0 {
			schema := OpenAPIV3Schema{
				Type:                "object",
				Properties:          bodyProperties,
				Required:            filterRequired(bodyRepresentation.requiredFields, bodyProperties),
				Title:               bodyRepresentation.title,
				Description:         bodyRepresentation.description,
				OpenAPIV3Extensions: bodyRepresentation.extensions,
			}
			oneOfSchemas[combinationName] = &OpenAPIV3SchemaRef{
				OpenAPIV3Schema: &schema,
			}
		}
	}
	applyInferredDiscriminatorFields(oneOfSchemas)

	sortedBodyOneOfNames := make([]string, 0, len(oneOfSchemas))
	for name := range oneOfSchemas {
		sortedBodyOneOfNames = append(sortedBodyOneOfNames, name)
	}
	sort.Strings(sortedBodyOneOfNames)

	oneOfSchemaRefs := []*OpenAPIV3SchemaRef{}
	for _, combinationName := range sortedBodyOneOfNames {
		schemaRef := OpenAPIV3SchemaRef{
			Ref: "#/components/schemas/" + combinationName,
		}
		oneOfSchemaRefs = append(oneOfSchemaRefs, &schemaRef)
	}
	var bodySchema *OpenAPIV3Schema
	if len(oneOfSchemas) == 0 {
		return nil, map[string]*OpenAPIV3SchemaRef{}
	}
	if len(oneOfSchemas) > 1 {
		bodySchema = &OpenAPIV3Schema{
			Type:  "object",
			OneOf: oneOfSchemaRefs,
		}
		schemasToAddToComponents = oneOfSchemas
	} else {
		for _, schema := range oneOfSchemas {
			bodySchema = schema.OpenAPIV3Schema
			break
		}
	}

	bodyContent := make(map[string]OpenAPIV3MediaType)
	bodyContent["application/json"] = OpenAPIV3MediaType{
		Schema: &OpenAPIV3SchemaRef{
			OpenAPIV3Schema: bodySchema,
		},
	}
	if len(oneOfSchemas) > 1 {
		schemasToAddToComponents = oneOfSchemas
	}

	// If any variant of the request body has required properties, the body
	// itself must be required — an optional body cannot have required fields.
	bodyRequired := false
	for _, schema := range oneOfSchemas {
		if schema.OpenAPIV3Schema != nil && len(schema.OpenAPIV3Schema.Required) > 0 {
			bodyRequired = true
			break
		}
	}

	return &OpenAPIV3RequestBodyRef{
		OpenAPIV3RequestBody: &OpenAPIV3RequestBody{
			Content:  bodyContent,
			Required: bodyRequired,
		},
	}, schemasToAddToComponents
}

type openAPIV3BodyRepresentation struct {
	fieldCombinations map[string][]protoField
	requiredFields    []string
	title             string
	description       string
	extensions        OpenAPIV3Extensions
	externaDocs       *OpenAPIV3ExternalDocs
}

func extractRequestBodyFieldCombinations(binding *descriptor.Binding, registry *descriptor.Registry, resolvedNames map[string]string) openAPIV3BodyRepresentation {
	var fieldMessage *descriptor.Message
	bodyFields := []protoField{}
	prefix := []string{}
	requiredFields := []string{}
	var title string
	var description string
	var externalDocs *OpenAPIV3ExternalDocs
	var extensions OpenAPIV3Extensions
	for _, fieldPathComponent := range binding.Body.FieldPath {
		prefix = append(prefix, fieldPathComponent.Name)
		// If the field is not a message type, it means the body is of a primitive type
		// and therefore we just return the field as is.
		if *fieldPathComponent.Target.Type != descriptorpb.FieldDescriptorProto_TYPE_MESSAGE {
			return openAPIV3BodyRepresentation{
				fieldCombinations: map[string][]protoField{"": {
					{
						FullPathToField: prefix,
						Field:           fieldPathComponent.Target,
					},
				}},
			}
		}
		fm, err := registry.LookupMsg(*fieldPathComponent.Target.TypeName, *fieldPathComponent.Target.TypeName)
		if err != nil {
			return openAPIV3BodyRepresentation{}
		}
		if fm == nil {
			return openAPIV3BodyRepresentation{}
		}
		fieldMessage = fm
	}
	if fieldMessage == nil {
		fieldMessage = binding.Method.RequestType
	}

	if proto.HasExtension(fieldMessage.Options, options.E_Openapiv3Schema) {
		schemaExtension, ok := proto.GetExtension(fieldMessage.Options, options.E_Openapiv3Schema).(*options.Schema)
		if ok && schemaExtension != nil {
			title = schemaExtension.GetJsonSchema().GetTitle()
			description = schemaExtension.GetJsonSchema().GetDescription()
			externalDocs = &OpenAPIV3ExternalDocs{
				Description: schemaExtension.GetExternalDocs().GetDescription(),
				URL:         schemaExtension.GetExternalDocs().GetUrl(),
			}
			for k, v := range schemaExtension.GetJsonSchema().GetExtensions() {
				if extensions == nil {
					extensions = make(OpenAPIV3Extensions)
				}
				extensions[k] = v
			}
			requiredFields = schemaExtension.GetJsonSchema().GetRequired()
		}
	}

	var fieldsNotPartOfOneofGroup []*descriptor.Field
	oneofGroups := make(map[string][]*descriptor.Field)
	for _, field := range fieldMessage.Fields {
		if field.OneofIndex == nil {
			fieldsNotPartOfOneofGroup = append(fieldsNotPartOfOneofGroup, field)
			continue
		}
		oneofDecl := fieldMessage.OneofDecl[*field.OneofIndex]
		if _, exists := oneofGroups[*oneofDecl.Name]; !exists {
			oneofGroups[*oneofDecl.Name] = []*descriptor.Field{}
		}
		oneofGroups[*oneofDecl.Name] = append(oneofGroups[*oneofDecl.Name], field)
	}
	for group := range oneofGroups {
		numberOfFieldsInGroup := len(oneofGroups[group])
		if numberOfFieldsInGroup <= 1 {
			fieldsNotPartOfOneofGroup = append(fieldsNotPartOfOneofGroup, oneofGroups[group]...)
			delete(oneofGroups, group)
		}
	}

	if len(oneofGroups) == 0 {
		for _, field := range fieldsNotPartOfOneofGroup {
			bodyField := protoField{
				FullPathToField: append(prefix, *field.Name),
				Field:           field,
			}
			bodyFields = append(bodyFields, bodyField)
		}
		return openAPIV3BodyRepresentation{
			fieldCombinations: map[string][]protoField{*fieldMessage.Name: bodyFields},
			requiredFields:    requiredFields,
			title:             title,
			description:       description,
			extensions:        extensions,
			externaDocs:       externalDocs,
		}
	}

	combinationsOfFieldsPartOfOneofGroups := generateOneOfCombinationsWithResolvedNames(oneofGroups, *fieldMessage.Name, resolvedNames)
	protoFields := make(map[string][]protoField)
	for combinationName, combination := range combinationsOfFieldsPartOfOneofGroups {
		fields := make([]protoField, 0, len(combination)+len(fieldsNotPartOfOneofGroup))
		for _, field := range fieldsNotPartOfOneofGroup {
			bodyField := protoField{
				FullPathToField: append(prefix, *field.Name),
				Field:           field,
			}
			fields = append(fields, bodyField)
		}

		for _, field := range combination {
			bodyField := protoField{
				FullPathToField: append(prefix, *field.Name),
				Field:           field,
			}
			fields = append(fields, bodyField)
		}
		protoFields[combinationName] = fields
	}

	return openAPIV3BodyRepresentation{
		fieldCombinations: protoFields,
		requiredFields:    requiredFields,
		title:             title,
		description:       description,
		extensions:        extensions,
		externaDocs:       externalDocs,
	}
}

func filterRequired(required []string, bodyProperties map[string]*OpenAPIV3SchemaRef) []string {
	result := make([]string, 0, len(required))
	for _, r := range required {
		if _, exists := bodyProperties[r]; exists {
			result = append(result, r)
		}
	}
	return result
}

func extractParameterFields(binding *descriptor.Binding) []protoField {
	protoFields := []protoField{}
	for _, pathParameter := range binding.PathParams {
		fullPathToField := []string{}
		for _, fieldPathComponent := range pathParameter.FieldPath {
			fullPathToField = append(fullPathToField, fieldPathComponent.Name)
		}
		protoField := protoField{
			FullPathToField: fullPathToField,
			Field:           pathParameter.Target,
		}
		protoFields = append(protoFields, protoField)
	}
	return protoFields
}

func buildMessageSchemasWithReferences(param param, resolvedNames map[string]string) map[string]*OpenAPIV3SchemaRef {
	schemas := make(map[string]*OpenAPIV3SchemaRef)
	statusMessage, err := param.reg.LookupMsg("google.rpc", "Status")
	statusMessageName := resolvedNames[statusMessage.FQMN()]
	if err != nil {
		log.Printf("Warning: could not lookup google.rpc.Status message: %v", err)
	}
	for _, message := range param.Messages {
		if !strings.HasPrefix(message.FQMN(), ".google.api") && !strings.HasPrefix(message.FQMN(), ".grpc.gateway.protoc_gen_openapi") && !strings.HasPrefix(message.FQMN(), ".google.rpc") {
			schema := buildOpenAPIV3SchemaFromMessageWithReferences(message, param.reg, resolvedNames)
			schemaRef := &OpenAPIV3SchemaRef{
				OpenAPIV3Schema: schema,
			}
			typeName := resolvedNames[message.FQMN()]
			schemas[typeName] = schemaRef
		}
	}

	statusSchema := buildOpenAPIV3SchemaFromMessageWithReferences(statusMessage, param.reg, resolvedNames)
	statusSchemaRef := &OpenAPIV3SchemaRef{
		OpenAPIV3Schema: statusSchema,
	}
	schemas[statusMessageName] = statusSchemaRef

	// Register the built-in default error schema referenced by description-only
	// error responses (unless default errors are disabled). It is self-contained,
	// so there are no extra dependencies to register. Skip if a proto type
	// already resolved to the same name — buildOpenAPIV3Paths applies the same
	// guard, so those responses stay bodyless rather than pointing at it.
	if !param.reg.GetDisableDefaultErrors() && !errorComponentReserved(resolvedNames) {
		schemas[defaultErrorSchemaName] = &OpenAPIV3SchemaRef{OpenAPIV3Schema: defaultErrorSchema()}
	}

	return schemas
}

func buildMessageSchemas(param param, resolvedNames map[string]string) (map[string]*OpenAPIV3SchemaRef, map[string]*OpenAPIV3SchemaRef) {
	schemaMap := make(map[string]*OpenAPIV3SchemaRef)
	oneOfSchemas := make(map[string]*OpenAPIV3SchemaRef)

	for _, message := range param.Messages {
		schemaMap[message.FQMN()] = &OpenAPIV3SchemaRef{
			OpenAPIV3Schema: &OpenAPIV3Schema{},
		}
	}

	for _, message := range param.Messages {
		schemaRefPtr := schemaMap[message.FQMN()]
		schema, messageOneOfSchemas := buildOpenAPIV3SchemaFromMessage(message, schemaMap, resolvedNames, param.reg)
		schemaRefPtr.OpenAPIV3Schema = schema
		maps.Copy(oneOfSchemas, messageOneOfSchemas)
	}

	return schemaMap, oneOfSchemas
}

func buildEnumSchemas(param param, resolvedNames map[string]string) map[string]*OpenAPIV3SchemaRef {
	schemas := make(map[string]*OpenAPIV3SchemaRef)
	for _, enum := range param.Enums {
		if strings.HasPrefix(enum.FQEN(), ".google.api") || strings.HasPrefix(enum.FQEN(), ".grpc.gateway.protoc_gen_openapi") || strings.HasPrefix(enum.FQEN(), ".google.rpc") {
			continue
		}
		var enumDefaultValue interface{}
		var title string
		var description string
		var deprecated bool
		var readOnly bool
		var example RawExample
		var extensions OpenAPIV3Extensions = make(OpenAPIV3Extensions)
		var enumVariants []string
		enumExtension, ok := proto.GetExtension(enum.Options, options.E_Openapiv3Enum).(*options.EnumSchema)
		openApiV3EnumExtensions := &OpenAPIV3Extensions{}
		if ok && enumExtension != nil {
			for k, v := range enumExtension.Extensions {
				(*openApiV3EnumExtensions)[k] = v
			}
			example = RawExample(enumExtension.Example)
			if enumExtension.GetDefault() != "" {
				enumDefaultValue = enumExtension.GetDefault()
			} else {
				enumDefaultValue = nil
			}
			title = enumExtension.Title
			description = enumExtension.Description
			readOnly = enumExtension.ReadOnly
			extensions = *openApiV3EnumExtensions
		}
		for _, enumValue := range enum.Value {
			if !isVisible(getEnumValueVisibilityOption(enumValue), param.reg) {
				continue
			}
			enumVariants = append(enumVariants, *enumValue.Name)
		}
		enumSchema := &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "string",
			Enum:                enumVariants,
			Default:             enumDefaultValue,
			Title:               title,
			Description:         description,
			Deprecated:          deprecated,
			ReadOnly:            readOnly,
			Example:             example,
			OpenAPIV3Extensions: extensions,
		}}
		schemas[resolvedNames[enum.FQEN()]] = enumSchema
	}
	codeEnumVariants := []string{
		"OK",
		"CANCELLED",
		"UNKNOWN",
		"INVALID_ARGUMENT",
		"DEADLINE_EXCEEDED",
		"NOT_FOUND",
		"ALREADY_EXISTS",
		"PERMISSION_DENIED",
		"UNAUTHENTICATED",
		"RESOURCE_EXHAUSTED",
		"FAILED_PRECONDITION",
		"ABORTED",
		"OUT_OF_RANGE",
		"UNIMPLEMENTED",
		"INTERNAL",
		"UNAVAILABLE",
		"DATA_LOSS",
	}
	codeSchema := &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
		Type: "string",
		Enum: codeEnumVariants,
	}}
	schemas[resolvedNames[".google.rpc.Code"]] = codeSchema
	return schemas
}

func buildOpenAPIV3SchemaFromMessageWithReferences(message *descriptor.Message, registry *descriptor.Registry, resolvedNames map[string]string) *OpenAPIV3Schema {
	var fieldsNotPartOfOneofGroup []*descriptor.Field
	var requiredFields []string
	var title string
	var description string
	var externalDocs *OpenAPIV3ExternalDocs
	var extensions OpenAPIV3Extensions
	var discriminator *OpenAPIV3Discriminator
	oneofGroups := make(map[string][]*descriptor.Field)
	if proto.HasExtension(message.Options, options.E_Openapiv3Schema) {
		schemaExtension, ok := proto.GetExtension(message.Options, options.E_Openapiv3Schema).(*options.Schema)
		if ok && schemaExtension != nil {
			title = schemaExtension.GetJsonSchema().GetTitle()
			description = schemaExtension.GetJsonSchema().GetDescription()
			externalDocs = &OpenAPIV3ExternalDocs{
				Description: schemaExtension.GetExternalDocs().GetDescription(),
				URL:         schemaExtension.GetExternalDocs().GetUrl(),
			}
			if schemaExtension.Discriminator != nil {
				discriminator = &OpenAPIV3Discriminator{
					PropertyName: schemaExtension.GetDiscriminator().GetPropertyName(),
					Mapping:      schemaExtension.GetDiscriminator().GetMapping(),
				}
			}
			for k, v := range schemaExtension.GetJsonSchema().GetExtensions() {
				if extensions == nil {
					extensions = make(OpenAPIV3Extensions)
				}
				extensions[k] = v
			}
			requiredFields = schemaExtension.GetJsonSchema().GetRequired()
		}
	}

	for _, field := range message.Fields {
		if field.OneofIndex == nil {
			fieldsNotPartOfOneofGroup = append(fieldsNotPartOfOneofGroup, field)
			continue
		}
		oneofDecl := message.OneofDecl[*field.OneofIndex]
		if _, exists := oneofGroups[*oneofDecl.Name]; !exists {
			oneofGroups[*oneofDecl.Name] = []*descriptor.Field{}
		}
		oneofGroups[*oneofDecl.Name] = append(oneofGroups[*oneofDecl.Name], field)
	}

	for group := range oneofGroups {
		numberOfFieldsInGroup := len(oneofGroups[group])
		if numberOfFieldsInGroup <= 1 {
			fieldsNotPartOfOneofGroup = append(fieldsNotPartOfOneofGroup, oneofGroups[group]...)
			delete(oneofGroups, group)
		}
	}

	if len(oneofGroups) == 0 {
		return buildSchemaFromFieldsWithReferences(fieldsNotPartOfOneofGroup, registry, requiredFields, title, description, externalDocs, extensions, resolvedNames)
	}

	combinationsOfFieldsPartOfOneofGroups := generateOneOfCombinationsWithResolvedNames(oneofGroups, resolvedNames[message.FQMN()], resolvedNames)

	combinationNames := make([]string, 0, len(combinationsOfFieldsPartOfOneofGroups))
	for name := range combinationsOfFieldsPartOfOneofGroups {
		combinationNames = append(combinationNames, name)
	}
	sort.Strings(combinationNames)

	oneOfSchemas := []*OpenAPIV3SchemaRef{}
	for _, combinationName := range combinationNames {
		oneOfSchemas = append(oneOfSchemas, &OpenAPIV3SchemaRef{
			Ref: "#/components/schemas/" + combinationName,
		})
	}
	if len(oneOfSchemas) == 1 {
		for _, schema := range oneOfSchemas {
			return schema.OpenAPIV3Schema
		}
	}

	return &OpenAPIV3Schema{
		OneOf:         oneOfSchemas,
		Discriminator: discriminator,
	}
}

func buildOpenAPIV3SchemaFromMessage(message *descriptor.Message, schemaMap map[string]*OpenAPIV3SchemaRef, resolvedNames map[string]string, registry *descriptor.Registry) (*OpenAPIV3Schema, map[string]*OpenAPIV3SchemaRef) {
	var fieldsNotPartOfOneofGroup []*descriptor.Field
	oneofGroups := make(map[string][]*descriptor.Field)
	var title string
	var description string
	var externalDocs *OpenAPIV3ExternalDocs
	var extensions OpenAPIV3Extensions
	var discriminator *OpenAPIV3Discriminator
	var requiredFields []string

	if proto.HasExtension(message.Options, options.E_Openapiv3Schema) {
		schemaExtension, ok := proto.GetExtension(message.Options, options.E_Openapiv3Schema).(*options.Schema)
		if ok && schemaExtension != nil {
			title = schemaExtension.GetJsonSchema().GetTitle()
			description = schemaExtension.GetJsonSchema().GetDescription()
			if schemaExtension.Discriminator != nil {
				discriminator = &OpenAPIV3Discriminator{
					PropertyName: schemaExtension.GetDiscriminator().GetPropertyName(),
					Mapping:      schemaExtension.GetDiscriminator().GetMapping(),
				}
			}
			for k, v := range schemaExtension.GetJsonSchema().GetExtensions() {
				if extensions == nil {
					extensions = make(OpenAPIV3Extensions)
				}
				extensions[k] = v
			}
			externalDocs = &OpenAPIV3ExternalDocs{
				Description: schemaExtension.GetExternalDocs().GetDescription(),
				URL:         schemaExtension.GetExternalDocs().GetUrl(),
			}
			requiredFields = schemaExtension.GetJsonSchema().GetRequired()
		}
	}

	for _, field := range message.Fields {
		if field.OneofIndex == nil {
			fieldsNotPartOfOneofGroup = append(fieldsNotPartOfOneofGroup, field)
			continue
		}
		oneofDecl := message.OneofDecl[*field.OneofIndex]
		if _, exists := oneofGroups[*oneofDecl.Name]; !exists {
			oneofGroups[*oneofDecl.Name] = []*descriptor.Field{}
		}
		oneofGroups[*oneofDecl.Name] = append(oneofGroups[*oneofDecl.Name], field)
	}
	for group := range oneofGroups {
		numberOfFieldsInGroup := len(oneofGroups[group])
		if numberOfFieldsInGroup <= 1 {
			fieldsNotPartOfOneofGroup = append(fieldsNotPartOfOneofGroup, oneofGroups[group]...)
			delete(oneofGroups, group)
		}
	}

	if len(oneofGroups) == 0 {
		return buildSchemaFromFields(fieldsNotPartOfOneofGroup, schemaMap, requiredFields, title, description, externalDocs, extensions, resolvedNames, registry), map[string]*OpenAPIV3SchemaRef{}
	}

	combinationsOfFieldsPartOfOneofGroups := generateOneOfCombinationsWithResolvedNames(oneofGroups, resolvedNames[message.FQMN()], resolvedNames)

	oneOfSchemas := map[string]*OpenAPIV3SchemaRef{}
	for combinationName, combination := range combinationsOfFieldsPartOfOneofGroups {
		combinationFields := []*descriptor.Field{}
		for _, field := range combination {
			combinationFields = append(combinationFields, field)
		}
		allSchemaFields := append(fieldsNotPartOfOneofGroup, combinationFields...)
		schema := buildSchemaFromFieldsWithReferences(allSchemaFields, registry, requiredFields, title, description, externalDocs, extensions, resolvedNames)
		oneOfSchemas[combinationName] = &OpenAPIV3SchemaRef{
			OpenAPIV3Schema: schema,
		}
	}
	applyInferredDiscriminatorFields(oneOfSchemas)

	if len(oneOfSchemas) == 1 {
		for _, schema := range oneOfSchemas {
			return schema.OpenAPIV3Schema, map[string]*OpenAPIV3SchemaRef{}
		}
	}

	sortedOneOfNames := make([]string, 0, len(oneOfSchemas))
	for name := range oneOfSchemas {
		sortedOneOfNames = append(sortedOneOfNames, name)
	}
	sort.Strings(sortedOneOfNames)

	oneOfSchemaRefs := []*OpenAPIV3SchemaRef{}
	for _, combinationName := range sortedOneOfNames {
		schemaRef := OpenAPIV3SchemaRef{
			Ref: "#/components/schemas/" + combinationName,
		}
		oneOfSchemaRefs = append(oneOfSchemaRefs, &schemaRef)
	}

	return &OpenAPIV3Schema{
		OneOf:         oneOfSchemaRefs,
		Discriminator: discriminator,
	}, oneOfSchemas
}

func generateOneOfCombinations(oneofGroups map[string][]*descriptor.Field, messageName string) map[string]map[string]*descriptor.Field {
	return generateOneOfCombinationsWithResolvedNames(oneofGroups, messageName, nil)
}

func generateOneOfCombinationsWithResolvedNames(oneofGroups map[string][]*descriptor.Field, messageName string, resolvedNames map[string]string) map[string]map[string]*descriptor.Field {
	allCombinations := []map[string]*descriptor.Field{{}}

	oneofGroupNames := make([]string, 0, len(oneofGroups))
	for name := range oneofGroups {
		oneofGroupNames = append(oneofGroupNames, name)
	}
	sort.Strings(oneofGroupNames)

	for _, groupName := range oneofGroupNames {
		variants := oneofGroups[groupName]
		newCombinations := []map[string]*descriptor.Field{}

		for _, existingCombination := range allCombinations {
			for _, variant := range variants {
				newCombination := make(map[string]*descriptor.Field)
				maps.Copy(newCombination, existingCombination)

				newCombination[groupName] = variant

				newCombinations = append(newCombinations, newCombination)
			}
		}
		allCombinations = newCombinations
	}

	// Build a set of existing type names for collision detection.
	// We store both the original name and the PascalCase version (dots removed)
	// because some code generators ignore dots when comparing names.
	existingTypeNames := make(map[string]struct{})
	for _, name := range resolvedNames {
		existingTypeNames[name] = struct{}{}
		// Also add the PascalCase version (dots stripped) to catch collisions
		// where the generated name would match after dot removal
		pascalName := toPascalCase(name)
		existingTypeNames[pascalName] = struct{}{}
	}

	namedCombinations := make(map[string]map[string]*descriptor.Field, len(allCombinations))

	for _, combination := range allCombinations {
		keyParts := make([]string, 0, len(oneofGroupNames))

		for _, groupName := range oneofGroupNames {
			variant, ok := combination[groupName]
			if !ok {
				continue
			}
			keyPart := fmt.Sprintf("%v", variant.GetName())
			keyParts = append(keyParts, keyPart)
		}

		combinationName := strings.Join(keyParts, "_")
		combinationName = messageName + "_" + combinationName
		combinationName = toPascalCase(combinationName)

		// Check for collision with existing type names and add suffix if needed
		if _, exists := existingTypeNames[combinationName]; exists {
			combinationName = combinationName + "Variant"
		}

		namedCombinations[combinationName] = combination
	}

	return namedCombinations
}

func applyInferredDiscriminatorFields(oneOfSchemas map[string]*OpenAPIV3SchemaRef) {
	discriminatorFieldsBySchema, ok := inferDiscriminatorFields(oneOfSchemas)
	if !ok {
		return
	}
	for schemaName, discriminatorFields := range discriminatorFieldsBySchema {
		schemaRef := oneOfSchemas[schemaName]
		if schemaRef == nil || schemaRef.OpenAPIV3Schema == nil {
			continue
		}
		schemaRef.Required = mergeRequiredFields(schemaRef.Required, discriminatorFields)
	}
}

func inferDiscriminatorFields(oneOfSchemas map[string]*OpenAPIV3SchemaRef) (map[string][]string, bool) {
	if len(oneOfSchemas) <= 1 {
		return map[string][]string{}, true
	}

	schemaNames := make([]string, 0, len(oneOfSchemas))
	propertySets := make(map[string][]string, len(oneOfSchemas))
	for schemaName, schemaRef := range oneOfSchemas {
		schemaNames = append(schemaNames, schemaName)
		if schemaRef == nil || schemaRef.OpenAPIV3Schema == nil {
			continue
		}

		properties := make([]string, 0, len(schemaRef.Properties))
		for propertyName := range schemaRef.Properties {
			properties = append(properties, propertyName)
		}
		sort.Strings(properties)
		propertySets[schemaName] = properties
	}
	sort.Strings(schemaNames)

	discriminatorFieldsBySchema := make(map[string][]string, len(oneOfSchemas))
	for _, schemaName := range schemaNames {
		discriminatorFields := minimalUniqueFieldSet(schemaName, propertySets)
		if len(discriminatorFields) == 0 {
			log.Printf("Warning: unable to infer discriminator fields for cartesian oneOf branch %q; sibling property sets: %v", schemaName, propertySets)
			return nil, false
		}
		discriminatorFieldsBySchema[schemaName] = discriminatorFields
	}

	return discriminatorFieldsBySchema, true
}

func minimalUniqueFieldSet(targetSchema string, propertySets map[string][]string) []string {
	targetProperties := propertySets[targetSchema]
	if len(targetProperties) == 0 {
		return nil
	}

	for subsetSize := 1; subsetSize <= len(targetProperties); subsetSize++ {
		for _, subset := range combinationsOfStrings(targetProperties, subsetSize) {
			isUnique := true
			for schemaName, properties := range propertySets {
				if schemaName == targetSchema {
					continue
				}
				if containsAllStrings(properties, subset) {
					isUnique = false
					break
				}
			}
			if isUnique {
				return subset
			}
		}
	}

	return nil
}

func combinationsOfStrings(values []string, size int) [][]string {
	if size <= 0 || size > len(values) {
		return nil
	}

	var results [][]string
	var current []string
	var visit func(start int)
	visit = func(start int) {
		if len(current) == size {
			results = append(results, slices.Clone(current))
			return
		}
		for index := start; index <= len(values)-(size-len(current)); index++ {
			current = append(current, values[index])
			visit(index + 1)
			current = current[:len(current)-1]
		}
	}
	visit(0)

	return results
}

func containsAllStrings(values []string, subset []string) bool {
	valueSet := make(map[string]struct{}, len(values))
	for _, value := range values {
		valueSet[value] = struct{}{}
	}
	for _, value := range subset {
		if _, ok := valueSet[value]; !ok {
			return false
		}
	}
	return true
}

func mergeRequiredFields(existing []string, additions []string) []string {
	if len(additions) == 0 {
		return existing
	}

	merged := make([]string, 0, len(existing)+len(additions))
	seen := make(map[string]struct{}, len(existing)+len(additions))
	for _, field := range existing {
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		merged = append(merged, field)
	}
	for _, field := range additions {
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		merged = append(merged, field)
	}
	return merged
}

// Helper function to build a single OpenAPI schema from a list of fields.
// This is used for both the no-oneof case and for individual oneOf variants.
func buildSchemaFromFieldsWithReferences(
	fields []*descriptor.Field,
	registry *descriptor.Registry,
	requiredFields []string,
	title string,
	description string,
	externalDocs *OpenAPIV3ExternalDocs,
	extensions OpenAPIV3Extensions,
	resolvedNames map[string]string,
) *OpenAPIV3Schema {
	properties := make(map[string]*OpenAPIV3SchemaRef)
	for _, field := range fields {
		propertySchema := buildPropertySchemaWithReferencesFromField(field, registry, resolvedNames)
		if propertySchema == nil {
			continue
		}
		properties[*field.Name] = propertySchema
	}
	schema := &OpenAPIV3Schema{
		Type:                "object",
		Title:               title,
		Description:         description,
		ExternalDocs:        externalDocs,
		OpenAPIV3Extensions: extensions,
		Properties:          properties,
		Required:            requiredFields,
	}
	if len(properties) == 0 {
		schema.AdditionalProperties = false
	}
	return schema
}

func buildSchemaFromFields(
	fields []*descriptor.Field,
	schemaMap map[string]*OpenAPIV3SchemaRef,
	requiredFields []string,
	title string,
	description string,
	externalDocs *OpenAPIV3ExternalDocs,
	extensions OpenAPIV3Extensions,
	resolvedNames map[string]string,
	registry *descriptor.Registry,
) *OpenAPIV3Schema {
	properties := make(map[string]*OpenAPIV3SchemaRef)
	for _, field := range fields {
		propertySchema := buildPropertySchemaFromField(field, schemaMap, resolvedNames, registry)
		if propertySchema == nil {
			continue
		}
		properties[*field.Name] = propertySchema
	}
	schema := &OpenAPIV3Schema{
		Type:                "object",
		Title:               title,
		Description:         description,
		ExternalDocs:        externalDocs,
		OpenAPIV3Extensions: extensions,
		Properties:          properties,
		Required:            requiredFields,
	}
	if len(properties) == 0 {
		schema.AdditionalProperties = false
	}
	return schema
}

// Helper function to convert a protobuf field descriptor into an OpenAPI schema reference.
// This function will use references for message types, and will build the schema inline for primitive types.
func buildPropertySchemaWithReferencesFromField(field *descriptor.Field, registry *descriptor.Registry, resolvedNames map[string]string) *OpenAPIV3SchemaRef {
	// This function handles the logic from your original code, mapping protobuf types to OpenAPI types.
	if !isVisible(getFieldVisibilityOption(field), registry) {
		return nil
	}
	var fieldMessage *descriptor.Message
	if field.TypeName != nil {
		fieldMessage, _ = registry.LookupMsg(*field.TypeName, *field.TypeName)
	}
	var opts *descriptorpb.MessageOptions
	if fieldMessage != nil {
		opts = fieldMessage.Options
	}

	if field.Label != nil && *field.Label == descriptorpb.FieldDescriptorProto_LABEL_REPEATED && (opts == nil || opts.MapEntry == nil || !*opts.MapEntry) {
		itemSchema, example := buildPropertySchemaWithReferencesFromFieldType(field, registry, resolvedNames)

		schema := &OpenAPIV3Schema{
			Type:  "array",
			Items: itemSchema,
		}
		if example != nil {
			schema.Example = example
		}
		// Always emit minItems on arrays (default 0). proto3 cannot express an
		// explicit 0, so an array with no min_items annotation still gets
		// minItems: 0, satisfying ibm-array-attributes. maxItems has no natural
		// default and is only emitted when annotated.
		var minItems uint64
		if proto.HasExtension(field.Options, options.E_Openapiv3Field) {
			if fieldExtension, ok := proto.GetExtension(field.Options, options.E_Openapiv3Field).(*options.JSONSchema); ok {
				schema.Description = fieldExtension.Description
				minItems = fieldExtension.MinItems
				schema.MaxItems = fieldExtension.MaxItems
			}
		}
		schema.MinItems = uint64Ptr(minItems)
		return &OpenAPIV3SchemaRef{
			OpenAPIV3Schema: schema,
		}
	}
	propertySchema, _ := buildPropertySchemaWithReferencesFromFieldType(field, registry, resolvedNames)
	return propertySchema
}

func buildPropertySchemaWithReferencesFromFieldType(field *descriptor.Field, registry *descriptor.Registry, resolvedNames map[string]string) (*OpenAPIV3SchemaRef, RawExample) {
	var title string
	var maximum float64
	var minimum *float64
	var exclusiveMaximum bool
	var exclusiveMinimum bool
	var pattern string
	var format string
	var maxLength uint64
	var minLength uint64
	var multipleOf float64
	var description string
	var readOnly bool
	var deprecated bool
	var arrayExample RawExample = nil
	var fieldExample RawExample = nil
	var jsonExample string
	var rawExample RawExample = nil
	var extensions OpenAPIV3Extensions
	var valueSchema *options.JSONSchema
	if field.Options != nil && field.Options.Deprecated != nil {
		deprecated = *field.Options.Deprecated
	}
	if proto.HasExtension(field.Options, options.E_Openapiv3Field) {
		fieldExtension, ok := proto.GetExtension(field.Options, options.E_Openapiv3Field).(*options.JSONSchema)
		if ok {
			for k, v := range fieldExtension.Extensions {
				if extensions == nil {
					extensions = make(OpenAPIV3Extensions)
				}
				extensions[k] = v
			}
			title = fieldExtension.Title
			maximum = fieldExtension.Maximum
			minimum = fieldExtension.Minimum
			exclusiveMaximum = fieldExtension.ExclusiveMaximum
			exclusiveMinimum = fieldExtension.ExclusiveMinimum
			pattern = fieldExtension.Pattern
			format = fieldExtension.Format
			maxLength = fieldExtension.MaxLength
			minLength = fieldExtension.MinLength
			multipleOf = fieldExtension.MultipleOf
			description = fieldExtension.Description
			readOnly = fieldExtension.ReadOnly
			jsonExample = fieldExtension.Example
			valueSchema = fieldExtension.ValueSchema
		}
	}
	isArrayOrMapElement := false
	if jsonExample != "" {
		isArrayOrMapElement = jsonExample[0:1] == "[" || jsonExample[0:1] == "{"
		rawExample = RawExample(jsonExample)
	}
	if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_BOOL {
		constrainedExample, err := validateAndCoerceJsonExample(jsonExample, "boolean")
		if err == nil && constrainedExample != "" {
			rawExample = RawExample(constrainedExample)
		}
		if isArrayOrMapElement {
			arrayExample = rawExample
		} else {
			fieldExample = rawExample
		}
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "boolean",
			Title:               title,
			Description:         description,
			Deprecated:          deprecated,
			ReadOnly:            readOnly,
			Example:             fieldExample,
			OpenAPIV3Extensions: extensions,
		}}, arrayExample
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_DOUBLE {
		constrainedExample, err := validateAndCoerceJsonExample(jsonExample, "double")
		if err == nil && constrainedExample != "" {
			rawExample = RawExample(constrainedExample)
		}
		if isArrayOrMapElement {
			arrayExample = rawExample
		} else {
			fieldExample = rawExample
		}
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "number",
			Format:              "double",
			Title:               title,
			Maximum:             maximum,
			Minimum:             minimum,
			ExclusiveMaximum:    exclusiveMaximum,
			ExclusiveMinimum:    exclusiveMinimum,
			MultipleOf:          multipleOf,
			Description:         description,
			Deprecated:          deprecated,
			ReadOnly:            readOnly,
			Example:             fieldExample,
			OpenAPIV3Extensions: extensions,
		}}, arrayExample
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_FLOAT {
		constrainedExample, err := validateAndCoerceJsonExample(jsonExample, "float")
		if err == nil && constrainedExample != "" {
			rawExample = RawExample(constrainedExample)
		}
		if isArrayOrMapElement {
			arrayExample = rawExample
		} else {
			fieldExample = rawExample
		}
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "number",
			Format:              "float",
			Title:               title,
			Maximum:             maximum,
			Minimum:             minimum,
			ExclusiveMaximum:    exclusiveMaximum,
			ExclusiveMinimum:    exclusiveMinimum,
			MultipleOf:          multipleOf,
			Description:         description,
			Deprecated:          deprecated,
			ReadOnly:            readOnly,
			Example:             fieldExample,
			OpenAPIV3Extensions: extensions,
		}}, arrayExample
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_UINT32 {
		constrainedExample, err := validateAndCoerceJsonExample(jsonExample, "integer")
		if err == nil && constrainedExample != "" {
			rawExample = RawExample(constrainedExample)
		}
		if isArrayOrMapElement {
			arrayExample = rawExample
		} else {
			fieldExample = rawExample
		}
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:    "integer",
			Format:  "int64",
			Title:   title,
			Maximum: maximum,
			// Unsigned integer: natural lower bound is 0, so emit minimum: 0 by
			// default (ibm-integer-attributes). An override raises it.
			Minimum:             unsignedMinimum(minimum),
			ExclusiveMaximum:    exclusiveMaximum,
			ExclusiveMinimum:    exclusiveMinimum,
			MultipleOf:          multipleOf,
			Description:         description,
			Deprecated:          deprecated,
			ReadOnly:            readOnly,
			Example:             fieldExample,
			OpenAPIV3Extensions: extensions,
		}}, arrayExample
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_UINT64 ||
		*field.Type == descriptorpb.FieldDescriptorProto_TYPE_FIXED64 {
		// uint64/fixed64 are JSON strings in proto3: type: string with a digit
		// pattern + length bounds, no numeric/format leaks. Overrides win.
		constrainedExample, err := stringIntExample(jsonExample, false)
		if err == nil {
			rawExample = RawExample(constrainedExample)
		} else {
			rawExample = nil // drop a non-integer 64-bit example
		}
		if isArrayOrMapElement {
			arrayExample = rawExample
		} else {
			fieldExample = rawExample
		}
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "string",
			Format:              sanitizeStringIntFormat(format),
			Title:               title,
			Pattern:             stringInt64Pattern(pattern, false),
			MaxLength:           stringIntMaxLength(maxLength),
			MinLength:           stringIntMinLength(minLength),
			Description:         description,
			Deprecated:          deprecated,
			ReadOnly:            readOnly,
			Example:             fieldExample,
			OpenAPIV3Extensions: extensions,
		}}, arrayExample
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_INT64 ||
		*field.Type == descriptorpb.FieldDescriptorProto_TYPE_SINT64 ||
		*field.Type == descriptorpb.FieldDescriptorProto_TYPE_SFIXED64 {
		// Signed 64-bit ints (int64/sint64/sfixed64): like the unsigned branch
		// above, with a sign-aware pattern.
		constrainedExample, err := stringIntExample(jsonExample, true)
		if err == nil {
			rawExample = RawExample(constrainedExample)
		} else {
			rawExample = nil // drop a non-integer 64-bit example
		}
		if isArrayOrMapElement {
			arrayExample = rawExample
		} else {
			fieldExample = rawExample
		}
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "string",
			Format:              sanitizeStringIntFormat(format),
			Title:               title,
			Pattern:             stringInt64Pattern(pattern, true),
			MaxLength:           stringIntMaxLength(maxLength),
			MinLength:           stringIntMinLength(minLength),
			Description:         description,
			Deprecated:          deprecated,
			ReadOnly:            readOnly,
			Example:             fieldExample,
			OpenAPIV3Extensions: extensions,
		}}, arrayExample
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_INT32 {
		constrainedExample, err := validateAndCoerceJsonExample(jsonExample, "integer")
		if err == nil && constrainedExample != "" {
			rawExample = RawExample(constrainedExample)
		}
		if isArrayOrMapElement {
			arrayExample = rawExample
		} else {
			fieldExample = rawExample
		}
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "integer",
			Format:              "int32",
			Title:               title,
			Maximum:             maximum,
			Minimum:             minimum,
			ExclusiveMaximum:    exclusiveMaximum,
			ExclusiveMinimum:    exclusiveMinimum,
			MultipleOf:          multipleOf,
			Description:         description,
			Deprecated:          deprecated,
			ReadOnly:            readOnly,
			Example:             fieldExample,
			OpenAPIV3Extensions: extensions,
		}}, arrayExample
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_STRING {
		constrainedExample, err := validateAndCoerceJsonExample(jsonExample, "string")
		// Guard on the raw annotation: an absent one coerces to `""` and would
		// fabricate example: "" (violating min_length), while still preserving a
		// deliberate empty-string example. Keep in sync with buildPropertySchemaFromFieldType.
		if err == nil && jsonExample != "" {
			rawExample = RawExample(constrainedExample)
		}
		if isArrayOrMapElement {
			arrayExample = rawExample
		} else {
			fieldExample = rawExample
		}
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "string",
			Title:               title,
			Description:         description,
			Deprecated:          deprecated,
			Pattern:             pattern,
			MaxLength:           maxLength,
			MinLength:           uint64Ptr(minLength),
			ReadOnly:            readOnly,
			Example:             fieldExample,
			OpenAPIV3Extensions: extensions,
		}}, arrayExample
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_BYTES {
		constrainedExample, err := validateAndCoerceJsonExample(jsonExample, "string")
		// See the TYPE_STRING note above: emit only when the annotation is present.
		if err == nil && jsonExample != "" {
			rawExample = RawExample(constrainedExample)
		}
		if isArrayOrMapElement {
			arrayExample = rawExample
		} else {
			fieldExample = rawExample
		}
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "string",
			Format:              "byte",
			Title:               title,
			Description:         description,
			Deprecated:          deprecated,
			MaxLength:           maxLength,
			MinLength:           uint64Ptr(minLength),
			ReadOnly:            readOnly,
			Example:             fieldExample,
			OpenAPIV3Extensions: extensions,
		}}, arrayExample
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_ENUM {
		constrainedExample, err := validateAndCoerceJsonExample(jsonExample, "string")
		if err == nil && constrainedExample != "\"\"" {
			rawExample = RawExample(constrainedExample)
		}
		if isArrayOrMapElement {
			arrayExample = rawExample
		}
		if field.TypeName != nil {
			return &OpenAPIV3SchemaRef{Ref: "#/components/schemas/" + resolvedNames[*field.TypeName]}, arrayExample
		}
	} else if field.TypeName != nil {
		if schema, ok := wellKnownTypesToOpenAPIV3SchemaMapping[*field.TypeName]; ok && schema != nil {
			typeCategory := openapiTypeCategory(schema)
			constrainedExample, err := wellKnownExample(schema, jsonExample, typeCategory)
			if err == nil && constrainedExample != "\"\"" && constrainedExample != "" {
				rawExample = RawExample(constrainedExample)
			}
			if isArrayOrMapElement {
				arrayExample = rawExample
			} else {
				fieldExample = rawExample
			}
			schemaCopy := *schema // Create a copy to avoid modifying the original schema
			schemaCopy.Title = title
			schemaCopy.Description = description
			schemaCopy.Deprecated = deprecated
			schemaCopy.ReadOnly = readOnly
			if isGoogleTypeDecimal(field) {
				applyDecimalStringOptions(&schemaCopy, pattern, format, maxLength, minLength)
				fieldExample, arrayExample = routeDecimalObjectExample(jsonExample, fieldExample, arrayExample)
			} else if schemaCopy.Type == "string" && (schemaCopy.Format == "int64" || schemaCopy.Format == "uint64") {
				// Int64Value/UInt64Value wrappers render as type: string: drop the
				// invalid format/bounds, emit a digit pattern + length bounds.
				signed := schemaCopy.Format == "int64"
				schemaCopy.Format = sanitizeStringIntFormat(format)
				schemaCopy.Pattern = stringInt64Pattern(pattern, signed)
				schemaCopy.MaxLength = stringIntMaxLength(maxLength)
				schemaCopy.MinLength = stringIntMinLength(minLength)
			} else {
				schemaCopy.Maximum = maximum
				if field.TypeName != nil && *field.TypeName == ".google.protobuf.UInt32Value" {
					// Unsigned 32-bit wrapper renders as type: integer; emit
					// minimum: 0 by default. An override raises the floor.
					schemaCopy.Minimum = unsignedMinimum(minimum)
				} else {
					schemaCopy.Minimum = minimum
				}
				schemaCopy.ExclusiveMaximum = exclusiveMaximum
				schemaCopy.ExclusiveMinimum = exclusiveMinimum
				schemaCopy.MultipleOf = multipleOf
				schemaCopy.Pattern = pattern
				schemaCopy.MaxLength = maxLength
				// Only string wrappers carry minLength; non-string wrappers omit it.
				if schemaCopy.Type == "string" {
					schemaCopy.MinLength = uint64Ptr(minLength)
				}
			}
			schemaCopy.OpenAPIV3Extensions = extensions
			schemaCopy.Example = fieldExample
			return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &schemaCopy}, arrayExample
		} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_MESSAGE {
			fieldMessage, err := registry.LookupMsg(*field.TypeName, *field.TypeName)
			if err != nil {
				log.Printf("Warning: could not lookup message for field %s: %v", *field.Name, err)
				return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{Type: "object"}}, arrayExample
			}
			opts := fieldMessage.GetOptions()
			// We need to check if this field is an actual message, or a message generated by the protobuf compiler
			// to represent a map. Map entry messages have the option map_entry set to true.
			if opts != nil && opts.MapEntry != nil && *opts.MapEntry {
				if len(fieldMessage.Fields) != 2 {
					log.Printf("Warning: map field %s does not have exactly 2 fields", *field.Name)
					return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{Type: "object"}}, arrayExample
				}
				valueField := fieldMessage.Fields[1]
				if valueField == nil {
					log.Printf("Warning: could not find key/value fields for map field %s", *field.Name)
					return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{Type: "object"}}, arrayExample
				}
				additionalProperties, mapExample := buildPropertySchemaWithReferencesFromFieldType(valueField, registry, resolvedNames)
				if additionalProperties != nil {
					additionalProperties = applyValueSchemaForMapValue(additionalProperties, valueSchema, valueField, mapValueSchemaType(valueField))
				}
				return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
					Type:                 "object",
					AdditionalProperties: additionalProperties,
					Title:                title,
					Description:          description,
					Deprecated:           deprecated,
					ReadOnly:             readOnly,
					Example:              mapExample,
					OpenAPIV3Extensions:  extensions,
				}}, arrayExample
			} else {
				return &OpenAPIV3SchemaRef{Ref: "#/components/schemas/" + resolvedNames[*field.TypeName]}, arrayExample
			}
		}
	}
	return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{Type: "string"}}, arrayExample
}

// stripQuotes attempts to remove a single pair of surrounding double quotes from a string.
func stripQuotes(s string) string {
	if strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"") {
		return s[1 : len(s)-1]
	}
	return s
}

func validateAndCoerceJsonExample(exampleString string, targetType string) (string, error) {
	if exampleString != "" && (exampleString[0:1] == "[" || exampleString[0:1] == "{") {
		return exampleString, nil
	}
	// Remove any surrounding whitespace for clean processing
	cleanString := strings.TrimSpace(exampleString)
	lowerType := strings.ToLower(targetType)

	if lowerType == "boolean" {
		if _, err := strconv.ParseBool(cleanString); err == nil {
			return cleanString, nil
		}

		stripped := stripQuotes(cleanString)
		if _, err := strconv.ParseBool(stripped); err == nil {
			return stripped, nil
		}

		return "", fmt.Errorf("example '%s' cannot be represented as a boolean", exampleString)
	}

	if lowerType == "integer" || lowerType == "number" || lowerType == "float" || lowerType == "double" {

		parsedValueStr := cleanString
		if _, err := strconv.ParseFloat(parsedValueStr, 64); err != nil {
			stripped := stripQuotes(cleanString)
			if stripped != cleanString {
				parsedValueStr = stripped
			} else {
				return "", fmt.Errorf("example '%s' cannot be parsed as a number", exampleString)
			}
		}

		if lowerType == "integer" {
			// Try parsing as int64 first to preserve precision for large integers
			if parsedInt, err := strconv.ParseInt(parsedValueStr, 10, 64); err == nil {
				return strconv.FormatInt(parsedInt, 10), nil
			}
			// Check if it's a float with no fractional part (e.g., "123.0")
			if parsedFloat, err := strconv.ParseFloat(parsedValueStr, 64); err == nil {
				if parsedFloat == float64(int64(parsedFloat)) {
					return strconv.FormatInt(int64(parsedFloat), 10), nil
				}
			}
			return "", fmt.Errorf("example '%s' is a float/double, not a valid integer", exampleString)
		}

		if _, err := strconv.ParseFloat(parsedValueStr, 64); err != nil {
			return "", fmt.Errorf("example '%s' cannot be parsed as a number", exampleString)
		}

		return parsedValueStr, nil
	}

	if lowerType == "string" {
		if strings.HasPrefix(cleanString, "\"") && strings.HasSuffix(cleanString, "\"") {
			return cleanString, nil
		}

		marshaled, err := json.Marshal(exampleString)
		if err != nil {
			return "", fmt.Errorf("failed to marshal string example '%s': %w", exampleString, err)
		}
		return string(marshaled), nil
	}

	return exampleString, nil
}

func buildPropertySchemaFromField(field *descriptor.Field, schemaMap map[string]*OpenAPIV3SchemaRef, resolvedNames map[string]string, registry *descriptor.Registry) *OpenAPIV3SchemaRef {
	if !isVisible(getFieldVisibilityOption(field), registry) {
		return nil
	}
	var fieldMessage *descriptor.Message
	if field.TypeName != nil {
		fieldMessage, _ = registry.LookupMsg(*field.TypeName, *field.TypeName)
	}
	var opts *descriptorpb.MessageOptions
	if fieldMessage != nil {
		opts = fieldMessage.Options
	}
	if field.Label != nil && *field.Label == descriptorpb.FieldDescriptorProto_LABEL_REPEATED && (opts == nil || opts.MapEntry == nil || !*opts.MapEntry) {
		propertySchema, example := buildPropertySchemaFromFieldType(field, schemaMap, resolvedNames, registry)
		schema := &OpenAPIV3Schema{
			Type:  "array",
			Items: propertySchema,
		}
		if example != nil {
			schema.Example = example
		}
		// Always emit minItems on arrays (default 0). proto3 cannot express an
		// explicit 0, so an array with no min_items annotation still gets
		// minItems: 0, satisfying ibm-array-attributes. maxItems has no natural
		// default and is only emitted when annotated.
		var minItems uint64
		if proto.HasExtension(field.Options, options.E_Openapiv3Field) {
			if fieldExtension, ok := proto.GetExtension(field.Options, options.E_Openapiv3Field).(*options.JSONSchema); ok {
				schema.Description = fieldExtension.Description
				minItems = fieldExtension.MinItems
				schema.MaxItems = fieldExtension.MaxItems
			}
		}
		schema.MinItems = uint64Ptr(minItems)
		return &OpenAPIV3SchemaRef{
			OpenAPIV3Schema: schema,
		}
	}
	propertySchema, _ := buildPropertySchemaFromFieldType(field, schemaMap, resolvedNames, registry)
	return propertySchema
}
func buildPropertySchemaFromFieldType(field *descriptor.Field, schemaMap map[string]*OpenAPIV3SchemaRef, resolvedNames map[string]string, registry *descriptor.Registry) (*OpenAPIV3SchemaRef, RawExample) {
	var title string
	var maximum float64
	var minimum *float64
	var exclusiveMaximum bool
	var exclusiveMinimum bool
	var pattern string
	var format string
	var maxLength uint64
	var minLength uint64
	var multipleOf float64
	var description string
	var readOnly bool
	var deprecated bool
	var extensions OpenAPIV3Extensions = OpenAPIV3Extensions{}
	var jsonExample string
	var fieldExample RawExample
	var arrayExample RawExample
	var rawExample RawExample
	var valueSchema *options.JSONSchema
	if field.Options != nil && field.Options.Deprecated != nil {
		deprecated = *field.Options.Deprecated
	}
	if proto.HasExtension(field.Options, options.E_Openapiv3Field) {
		fieldExtension, ok := proto.GetExtension(field.Options, options.E_Openapiv3Field).(*options.JSONSchema)
		if ok {
			for k, v := range fieldExtension.Extensions {
				extensions[k] = v
			}
			title = fieldExtension.Title
			maximum = fieldExtension.Maximum
			minimum = fieldExtension.Minimum
			exclusiveMaximum = fieldExtension.ExclusiveMaximum
			exclusiveMinimum = fieldExtension.ExclusiveMinimum
			pattern = fieldExtension.Pattern
			format = fieldExtension.Format
			maxLength = fieldExtension.MaxLength
			minLength = fieldExtension.MinLength
			multipleOf = fieldExtension.MultipleOf
			description = fieldExtension.Description
			readOnly = fieldExtension.ReadOnly
			jsonExample = fieldExtension.Example
			valueSchema = fieldExtension.ValueSchema
		}
	}
	isArrayOrMapElement := false
	if jsonExample != "" {
		isArrayOrMapElement = jsonExample[0:1] == "[" || jsonExample[0:1] == "{"
		rawExample = RawExample(jsonExample)
	} else {
		rawExample = nil
	}
	if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_BOOL {
		constrainedExample, err := validateAndCoerceJsonExample(jsonExample, "boolean")
		if err == nil && constrainedExample != "" {
			rawExample = RawExample(constrainedExample)
		}
		if isArrayOrMapElement {
			arrayExample = rawExample
		} else {
			fieldExample = rawExample
		}
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "boolean",
			Title:               title,
			Description:         description,
			Deprecated:          deprecated,
			Example:             fieldExample,
			OpenAPIV3Extensions: extensions,
		}}, arrayExample
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_DOUBLE {
		constrainedExample, err := validateAndCoerceJsonExample(jsonExample, "double")
		if err == nil && constrainedExample != "" {
			rawExample = RawExample(constrainedExample)
		}
		if isArrayOrMapElement {
			arrayExample = rawExample
		} else {
			fieldExample = rawExample
		}
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "number",
			Format:              "double",
			Title:               title,
			Maximum:             maximum,
			Minimum:             minimum,
			ExclusiveMaximum:    exclusiveMaximum,
			ExclusiveMinimum:    exclusiveMinimum,
			MultipleOf:          multipleOf,
			Description:         description,
			Deprecated:          deprecated,
			ReadOnly:            readOnly,
			Example:             fieldExample,
			OpenAPIV3Extensions: extensions,
		}}, arrayExample
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_UINT32 {
		constrainedExample, err := validateAndCoerceJsonExample(jsonExample, "integer")
		if err == nil && constrainedExample != "" {
			rawExample = RawExample(constrainedExample)
		}
		if isArrayOrMapElement {
			arrayExample = rawExample
		} else {
			fieldExample = rawExample
		}
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:    "integer",
			Format:  "int64",
			Title:   title,
			Maximum: maximum,
			// Unsigned integer: natural lower bound is 0, so emit minimum: 0 by
			// default (ibm-integer-attributes). An override raises it.
			Minimum:             unsignedMinimum(minimum),
			ExclusiveMaximum:    exclusiveMaximum,
			ExclusiveMinimum:    exclusiveMinimum,
			MultipleOf:          multipleOf,
			Description:         description,
			Deprecated:          deprecated,
			ReadOnly:            readOnly,
			Example:             fieldExample,
			OpenAPIV3Extensions: extensions,
		}}, arrayExample
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_UINT64 ||
		*field.Type == descriptorpb.FieldDescriptorProto_TYPE_FIXED64 {
		// uint64/fixed64 are JSON strings in proto3: type: string with a digit
		// pattern + length bounds, no numeric/format leaks. Overrides win.
		constrainedExample, err := stringIntExample(jsonExample, false)
		if err == nil {
			rawExample = RawExample(constrainedExample)
		} else {
			rawExample = nil // drop a non-integer 64-bit example
		}
		if isArrayOrMapElement {
			arrayExample = rawExample
		} else {
			fieldExample = rawExample
		}
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "string",
			Format:              sanitizeStringIntFormat(format),
			Title:               title,
			Pattern:             stringInt64Pattern(pattern, false),
			MaxLength:           stringIntMaxLength(maxLength),
			MinLength:           stringIntMinLength(minLength),
			Description:         description,
			Deprecated:          deprecated,
			ReadOnly:            readOnly,
			Example:             fieldExample,
			OpenAPIV3Extensions: extensions,
		}}, arrayExample
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_INT64 ||
		*field.Type == descriptorpb.FieldDescriptorProto_TYPE_SINT64 ||
		*field.Type == descriptorpb.FieldDescriptorProto_TYPE_SFIXED64 {
		// Signed 64-bit ints (int64/sint64/sfixed64): like the unsigned branch
		// above, with a sign-aware pattern.
		constrainedExample, err := stringIntExample(jsonExample, true)
		if err == nil {
			rawExample = RawExample(constrainedExample)
		} else {
			rawExample = nil // drop a non-integer 64-bit example
		}
		if isArrayOrMapElement {
			arrayExample = rawExample
		} else {
			fieldExample = rawExample
		}
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "string",
			Format:              sanitizeStringIntFormat(format),
			Title:               title,
			Pattern:             stringInt64Pattern(pattern, true),
			MaxLength:           stringIntMaxLength(maxLength),
			MinLength:           stringIntMinLength(minLength),
			Description:         description,
			Deprecated:          deprecated,
			ReadOnly:            readOnly,
			Example:             fieldExample,
			OpenAPIV3Extensions: extensions,
		}}, arrayExample
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_FLOAT {
		constrainedExample, err := validateAndCoerceJsonExample(jsonExample, "float")
		if err == nil && constrainedExample != "" {
			rawExample = RawExample(constrainedExample)
		}
		if isArrayOrMapElement {
			arrayExample = rawExample
		} else {
			fieldExample = rawExample
		}
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "number",
			Format:              "float",
			Title:               title,
			Maximum:             maximum,
			Minimum:             minimum,
			ExclusiveMaximum:    exclusiveMaximum,
			ExclusiveMinimum:    exclusiveMinimum,
			MultipleOf:          multipleOf,
			Description:         description,
			Deprecated:          deprecated,
			ReadOnly:            readOnly,
			Example:             fieldExample,
			OpenAPIV3Extensions: extensions,
		}}, arrayExample
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_INT32 {
		constrainedExample, err := validateAndCoerceJsonExample(jsonExample, "integer")
		if err == nil && constrainedExample != "" {
			rawExample = RawExample(constrainedExample)
		}
		if isArrayOrMapElement {
			arrayExample = rawExample
		} else {
			fieldExample = rawExample
		}
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "integer",
			Format:              "int32",
			Title:               title,
			Maximum:             maximum,
			Minimum:             minimum,
			ExclusiveMaximum:    exclusiveMaximum,
			ExclusiveMinimum:    exclusiveMinimum,
			MultipleOf:          multipleOf,
			Description:         description,
			Deprecated:          deprecated,
			ReadOnly:            readOnly,
			Example:             fieldExample,
			OpenAPIV3Extensions: extensions,
		}}, arrayExample
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_STRING {
		constrainedExample, err := validateAndCoerceJsonExample(jsonExample, "string")
		// Guard on the raw annotation: an absent one coerces to `""` and would
		// fabricate example: "" (violating min_length), while still preserving a
		// deliberate empty-string example. Keep in sync with buildPropertySchemaWithReferencesFromFieldType.
		if err == nil && jsonExample != "" {
			rawExample = RawExample(constrainedExample)
		}
		if isArrayOrMapElement {
			arrayExample = rawExample
		} else {
			fieldExample = rawExample
		}
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "string",
			Title:               title,
			Description:         description,
			Deprecated:          deprecated,
			Pattern:             pattern,
			MaxLength:           maxLength,
			MinLength:           uint64Ptr(minLength),
			ReadOnly:            readOnly,
			Example:             fieldExample,
			OpenAPIV3Extensions: extensions,
		}}, arrayExample
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_BYTES {
		constrainedExample, err := validateAndCoerceJsonExample(jsonExample, "string")
		// See the TYPE_STRING note above: emit only when the annotation is present.
		if err == nil && jsonExample != "" {
			rawExample = RawExample(constrainedExample)
		}
		if isArrayOrMapElement {
			arrayExample = rawExample
		} else {
			fieldExample = rawExample
		}
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "string",
			Format:              "byte",
			Title:               title,
			Description:         description,
			Deprecated:          deprecated,
			MaxLength:           maxLength,
			MinLength:           uint64Ptr(minLength),
			ReadOnly:            readOnly,
			Example:             fieldExample,
			OpenAPIV3Extensions: extensions,
		}}, arrayExample
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_ENUM {
		constrainedExample, err := validateAndCoerceJsonExample(jsonExample, "string")
		if err == nil && constrainedExample != "\"\"" {
			rawExample = RawExample(constrainedExample)
		}
		if isArrayOrMapElement {
			arrayExample = rawExample
		}
		return &OpenAPIV3SchemaRef{Ref: "#/components/schemas/" + resolvedNames[*field.TypeName]}, arrayExample
	} else if field.TypeName != nil {
		if schema, ok := wellKnownTypesToOpenAPIV3SchemaMapping[*field.TypeName]; ok && schema != nil {
			typeCategory := openapiTypeCategory(schema)
			constrainedExample, err := wellKnownExample(schema, jsonExample, typeCategory)
			if err == nil && constrainedExample != "\"\"" && constrainedExample != "" {
				rawExample = RawExample(constrainedExample)
			}
			if isArrayOrMapElement {
				arrayExample = rawExample
			} else {
				fieldExample = rawExample
			}
			schemaCopy := *schema // Create a copy to avoid modifying the original schema
			schemaCopy.Title = title
			schemaCopy.Description = description
			schemaCopy.Deprecated = deprecated
			schemaCopy.ReadOnly = readOnly
			if isGoogleTypeDecimal(field) {
				applyDecimalStringOptions(&schemaCopy, pattern, format, maxLength, minLength)
				fieldExample, arrayExample = routeDecimalObjectExample(jsonExample, fieldExample, arrayExample)
			} else if schemaCopy.Type == "string" && (schemaCopy.Format == "int64" || schemaCopy.Format == "uint64") {
				// Int64Value/UInt64Value wrappers render as type: string: drop the
				// invalid format/bounds, emit a digit pattern + length bounds.
				signed := schemaCopy.Format == "int64"
				schemaCopy.Format = sanitizeStringIntFormat(format)
				schemaCopy.Pattern = stringInt64Pattern(pattern, signed)
				schemaCopy.MaxLength = stringIntMaxLength(maxLength)
				schemaCopy.MinLength = stringIntMinLength(minLength)
			} else {
				schemaCopy.Maximum = maximum
				if field.TypeName != nil && *field.TypeName == ".google.protobuf.UInt32Value" {
					// Unsigned 32-bit wrapper renders as type: integer; emit
					// minimum: 0 by default. An override raises the floor.
					schemaCopy.Minimum = unsignedMinimum(minimum)
				} else {
					schemaCopy.Minimum = minimum
				}
				schemaCopy.ExclusiveMaximum = exclusiveMaximum
				schemaCopy.ExclusiveMinimum = exclusiveMinimum
				schemaCopy.MultipleOf = multipleOf
				schemaCopy.Pattern = pattern
				schemaCopy.MaxLength = maxLength
				// Only string wrappers carry minLength; non-string wrappers omit it.
				if schemaCopy.Type == "string" {
					schemaCopy.MinLength = uint64Ptr(minLength)
				}
			}
			schemaCopy.OpenAPIV3Extensions = extensions
			schemaCopy.Example = fieldExample
			return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &schemaCopy}, arrayExample
		} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_MESSAGE {
			schema := &OpenAPIV3Schema{
				Type:       "object",
				Properties: make(map[string]*OpenAPIV3SchemaRef),
			}
			fieldMessage, err := registry.LookupMsg(*field.TypeName, *field.TypeName)
			if err != nil || fieldMessage == nil {
				log.Printf("Warning: could not lookup message for field %s: %v", *field.Name, err)
				return &OpenAPIV3SchemaRef{OpenAPIV3Schema: schema}, arrayExample
			}
			opts := fieldMessage.GetOptions()
			if opts != nil && opts.MapEntry != nil && *opts.MapEntry {
				if len(fieldMessage.Fields) != 2 {
					log.Printf("Warning: map field %s does not have exactly 2 fields", *field.Name)
					return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{Type: "object"}}, arrayExample
				}
				valueField := fieldMessage.Fields[1]
				if valueField == nil {
					log.Printf("Warning: could not find key/value fields for map field %s", *field.Name)
					return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{Type: "object"}}, arrayExample
				}
				additionalProperties, mapExample := buildPropertySchemaFromFieldType(valueField, schemaMap, resolvedNames, registry)
				if additionalProperties != nil {
					additionalProperties = applyValueSchemaForMapValue(additionalProperties, valueSchema, valueField, mapValueSchemaType(valueField))
				}
				return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
					Type:                 "object",
					AdditionalProperties: additionalProperties,
					Title:                title,
					Description:          description,
					Deprecated:           deprecated,
					ReadOnly:             readOnly,
					Example:              mapExample,
					OpenAPIV3Extensions:  extensions,
				}}, arrayExample
			}
			schemaRef := schemaMap[*field.TypeName]
			if schemaRef != nil {
				schema = schemaRef.OpenAPIV3Schema
			} else {
				log.Printf("Warning: could not find schema for message %s", *field.TypeName)
			}
			return &OpenAPIV3SchemaRef{OpenAPIV3Schema: schema}, arrayExample
		}
	}
	return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{Type: "string"}}, arrayExample
}

func getFieldVisibilityOption(fd *descriptor.Field) *visibility.VisibilityRule {
	if fd.Options == nil {
		return nil
	}
	if !proto.HasExtension(fd.Options, visibility.E_FieldVisibility) {
		return nil
	}
	ext := proto.GetExtension(fd.Options, visibility.E_FieldVisibility)
	opts, ok := ext.(*visibility.VisibilityRule)
	if !ok {
		return nil
	}
	return opts
}

func getServiceVisibilityOption(fd *descriptor.Service) *visibility.VisibilityRule {
	if fd.Options == nil {
		return nil
	}
	if !proto.HasExtension(fd.Options, visibility.E_ApiVisibility) {
		return nil
	}
	ext := proto.GetExtension(fd.Options, visibility.E_ApiVisibility)
	opts, ok := ext.(*visibility.VisibilityRule)
	if !ok {
		return nil
	}
	return opts
}

func getMethodVisibilityOption(fd *descriptor.Method) *visibility.VisibilityRule {
	if fd.Options == nil {
		return nil
	}
	if !proto.HasExtension(fd.Options, visibility.E_MethodVisibility) {
		return nil
	}
	ext := proto.GetExtension(fd.Options, visibility.E_MethodVisibility)
	opts, ok := ext.(*visibility.VisibilityRule)
	if !ok {
		return nil
	}
	return opts
}

func getEnumValueVisibilityOption(fd *descriptorpb.EnumValueDescriptorProto) *visibility.VisibilityRule {
	if fd.Options == nil {
		return nil
	}
	if !proto.HasExtension(fd.Options, visibility.E_ValueVisibility) {
		return nil
	}
	ext := proto.GetExtension(fd.Options, visibility.E_ValueVisibility)
	opts, ok := ext.(*visibility.VisibilityRule)
	if !ok {
		return nil
	}
	return opts
}

func isVisible(r *visibility.VisibilityRule, reg *descriptor.Registry) bool {
	if r == nil {
		return true
	}

	restrictions := strings.Split(strings.TrimSpace(r.Restriction), ",")
	// No restrictions results in the element always being visible
	if len(restrictions) == 0 {
		return true
	}

	for _, restriction := range restrictions {
		if reg.GetVisibilityRestrictionSelectors()[strings.TrimSpace(restriction)] {
			return true
		}
	}

	return false
}

func getFieldConfiguration(reg *descriptor.Registry, fd *descriptor.Field) *options.JSONSchema_FieldConfiguration {
	if j, err := getFieldOpenAPIOption(reg, fd); err == nil && j != nil {
		return j.GetFieldConfiguration()
	}
	return nil
}

func getFieldOpenAPIOption(reg *descriptor.Registry, fd *descriptor.Field) (*options.JSONSchema, error) {
	if fd.Options != nil && proto.HasExtension(fd.Options, options.E_Openapiv3Field) {
		ext := proto.GetExtension(fd.Options, options.E_Openapiv3Field)
		opts, ok := ext.(*options.JSONSchema)
		if !ok {
			return nil, fmt.Errorf("extension is %T; want a JSONSchema object", ext)
		}
		return opts, nil
	}
	opts, ok := reg.GetOpenAPIFieldOptionv3(fd.FQFN())
	if !ok {
		return nil, nil
	}
	return opts, nil
}
