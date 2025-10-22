package genopenapi

import (
	"fmt"
	"log"
	"maps"
	"sort"
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

func buildOpenAPIV3Paths(param param, resolvedNames map[string]string) (OpenAPIV3Paths, map[string]*OpenAPIV3SchemaRef, error) {
	paths := OpenAPIV3Paths{}
	schemasToAddToComponents := map[string]*OpenAPIV3SchemaRef{}
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
						responses = extractOpenAPIV3ResponsesFromProtoExtension(operation)
						if operation.ExternalDocs != nil && operation.ExternalDocs.Description != "" && operation.ExternalDocs.Url != "" {
							externalDocs = &OpenAPIV3ExternalDocs{
								Description: operation.ExternalDocs.Description,
								URL:         operation.ExternalDocs.Url,
							}
						}
					}
				}
				path := b.PathTmpl.Template

				// Ensure the path item exists
				pathItem, ok := paths[path]
				httpMethod := b.HTTPMethod
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

func extractOpenAPIV3ResponsesFromProtoExtension(operation *options.Operation) OpenAPIV3Responses {
	responses := OpenAPIV3Responses{}
	for statusCode, response := range operation.Responses {
		if response != nil {
			if statusCode != successStatusCode {
				var ref string
				var content map[string]OpenAPIV3MediaType
				if response.Schema != nil && response.Schema.JsonSchema != nil && response.Schema.JsonSchema.Ref != "" {
					ref = "#/components/schemas/" + response.Schema.JsonSchema.Ref
					content := make(map[string]OpenAPIV3MediaType)
					content["application/json"] = OpenAPIV3MediaType{
						Schema: &OpenAPIV3SchemaRef{
							Ref: ref,
						},
					}
				} else {
					content = make(map[string]OpenAPIV3MediaType)
					content["application/json"] = OpenAPIV3MediaType{}
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
				responses[statusCode] = OpenAPIV3ResponseRef{
					OpenAPIV3Response: &OpenAPIV3Response{
						Description: response.Description,
						Headers:     headers,
						Content:     content,
					},
				}
			} else {
				// The 200 response is reserved for the main response body
				continue
			}
		}
	}
	return responses
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
			responseContent["application/json"] = OpenAPIV3MediaType{
				Schema: &OpenAPIV3SchemaRef{
					OpenAPIV3Schema: schema,
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

func buildPathParameters(binding *descriptor.Binding, registry *descriptor.Registry, resolvedNames map[string]string) []OpenAPIV3ParameterRef {
	parameterRefs := []OpenAPIV3ParameterRef{}
	for _, param := range binding.PathParams {
		paramName := param.FieldPath.String()
		field := param.Target
		if !isVisible(getFieldVisibilityOption(field), registry) {
			continue
		}
		fieldOpenApiV3Schema := buildPropertySchemaWithReferencesFromField(field, registry, resolvedNames)
		if fieldOpenApiV3Schema != nil {
			parameterRef := OpenAPIV3ParameterRef{
				OpenAPIV3Parameter: &OpenAPIV3Parameter{
					Name:     paramName,
					In:       "path",
					Required: true,
					Schema:   fieldOpenApiV3Schema,
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
					Name:     *field.Name,
					In:       "query",
					Required: false,
					Schema:   queryParameterSchema,
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
				Name:     *field.Name,
				In:       "query",
				Required: false,
				Schema:   queryParameterSchema,
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
	bodyRepresentation := extractRequestBodyFieldCombinations(binding, registry)
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
				Type:                 "object",
				Properties:           bodyProperties,
				Required:             bodyRepresentation.requiredFields,
				Title:                bodyRepresentation.title,
				Description:          bodyRepresentation.description,
				AdditionalProperties: false,
				OpenAPIV3Extensions:  bodyRepresentation.extensions,
			}
			oneOfSchemas[combinationName] = &OpenAPIV3SchemaRef{
				OpenAPIV3Schema: &schema,
			}
		}
	}
	oneOfSchemaRefs := []*OpenAPIV3SchemaRef{}
	for combinationName := range oneOfSchemas {
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
	return &OpenAPIV3RequestBodyRef{
		OpenAPIV3RequestBody: &OpenAPIV3RequestBody{
			Content: bodyContent,
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

func extractRequestBodyFieldCombinations(binding *descriptor.Binding, registry *descriptor.Registry) openAPIV3BodyRepresentation {
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

	combinationsOfFieldsPartOfOneofGroups := generateOneOfCombinations(oneofGroups, *fieldMessage.Name)
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

	combinationsOfFieldsPartOfOneofGroups := generateOneOfCombinations(oneofGroups, *message.Name)

	oneOfSchemas := []*OpenAPIV3SchemaRef{}
	for combinationName := range combinationsOfFieldsPartOfOneofGroups {
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
		OneOf: oneOfSchemas,
	}
}

func buildOpenAPIV3SchemaFromMessage(message *descriptor.Message, schemaMap map[string]*OpenAPIV3SchemaRef, resolvedNames map[string]string, registry *descriptor.Registry) (*OpenAPIV3Schema, map[string]*OpenAPIV3SchemaRef) {
	var fieldsNotPartOfOneofGroup []*descriptor.Field
	oneofGroups := make(map[string][]*descriptor.Field)
	var title string
	var description string
	var externalDocs *OpenAPIV3ExternalDocs
	var extensions OpenAPIV3Extensions
	var requiredFields []string

	if proto.HasExtension(message.Options, options.E_Openapiv3Schema) {
		schemaExtension, ok := proto.GetExtension(message.Options, options.E_Openapiv3Schema).(*options.Schema)
		if ok && schemaExtension != nil {
			title = schemaExtension.GetJsonSchema().GetTitle()
			description = schemaExtension.GetJsonSchema().GetDescription()
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

	combinationsOfFieldsPartOfOneofGroups := generateOneOfCombinations(oneofGroups, *message.Name)

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

	if len(oneOfSchemas) == 1 {
		for _, schema := range oneOfSchemas {
			return schema.OpenAPIV3Schema, map[string]*OpenAPIV3SchemaRef{}
		}
	}

	oneOfSchemaRefs := []*OpenAPIV3SchemaRef{}
	for combinationName := range oneOfSchemas {
		schemaRef := OpenAPIV3SchemaRef{
			Ref: "#/components/schemas/" + combinationName,
		}
		oneOfSchemaRefs = append(oneOfSchemaRefs, &schemaRef)
	}

	return &OpenAPIV3Schema{
		OneOf: oneOfSchemaRefs,
	}, oneOfSchemas
}

func generateOneOfCombinations(oneofGroups map[string][]*descriptor.Field, messageName string) map[string]map[string]*descriptor.Field {
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
		namedCombinations[combinationName] = combination
	}

	return namedCombinations
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
	return &OpenAPIV3Schema{
		Type:                "object",
		Title:               title,
		Description:         description,
		ExternalDocs:        externalDocs,
		OpenAPIV3Extensions: extensions,
		Properties:          properties,
		Required:            requiredFields,
	}
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
	return &OpenAPIV3Schema{
		Type:                "object",
		Title:               title,
		Description:         description,
		ExternalDocs:        externalDocs,
		OpenAPIV3Extensions: extensions,
		Properties:          properties,
		Required:            requiredFields,
	}
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
		schema := &OpenAPIV3Schema{
			Type:  "array",
			Items: buildPropertySchemaWithReferencesFromFieldType(field, registry, resolvedNames),
		}
		return &OpenAPIV3SchemaRef{
			OpenAPIV3Schema: schema,
		}
	}
	return buildPropertySchemaWithReferencesFromFieldType(field, registry, resolvedNames)
}

func buildPropertySchemaWithReferencesFromFieldType(field *descriptor.Field, registry *descriptor.Registry, resolvedNames map[string]string) *OpenAPIV3SchemaRef {
	var title string
	var maximum float64
	var minimum float64
	var exclusiveMaximum bool
	var exclusiveMinimum bool
	var pattern string
	var maxLength uint64
	var minLength uint64
	var multipleOf float64
	var description string
	var readOnly bool
	var deprecated bool
	var example RawExample
	var extensions OpenAPIV3Extensions
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
			maxLength = fieldExtension.MaxLength
			minLength = fieldExtension.MinLength
			multipleOf = fieldExtension.MultipleOf
			description = fieldExtension.Description
			readOnly = fieldExtension.ReadOnly
			example = RawExample(fieldExtension.Example)
		}
	}
	if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_BOOL {
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "boolean",
			Title:               title,
			Description:         description,
			Deprecated:          deprecated,
			ReadOnly:            readOnly,
			Example:             example,
			OpenAPIV3Extensions: extensions,
		}}
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_DOUBLE {
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
			Example:             example,
			OpenAPIV3Extensions: extensions,
		}}
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_FLOAT {
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
			Example:             example,
			OpenAPIV3Extensions: extensions,
		}}
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_UINT32 {
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "integer",
			Format:              "int64",
			Title:               title,
			Maximum:             maximum,
			Minimum:             max(minimum, 0),
			ExclusiveMaximum:    exclusiveMaximum,
			ExclusiveMinimum:    exclusiveMinimum,
			MultipleOf:          multipleOf,
			Description:         description,
			Deprecated:          deprecated,
			ReadOnly:            readOnly,
			Example:             example,
			OpenAPIV3Extensions: extensions,
		}}
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_UINT64 {
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "string",
			Format:              "int64",
			Title:               title,
			Maximum:             maximum,
			Minimum:             max(minimum, 0),
			ExclusiveMaximum:    exclusiveMaximum,
			ExclusiveMinimum:    exclusiveMinimum,
			MultipleOf:          multipleOf,
			Description:         description,
			Deprecated:          deprecated,
			ReadOnly:            readOnly,
			Example:             example,
			OpenAPIV3Extensions: extensions,
		}}
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_INT32 {
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
			Example:             example,
			OpenAPIV3Extensions: extensions,
		}}
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_INT64 {
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "integer",
			Format:              "int64",
			Title:               title,
			Maximum:             maximum,
			Minimum:             minimum,
			ExclusiveMaximum:    exclusiveMaximum,
			ExclusiveMinimum:    exclusiveMinimum,
			MultipleOf:          multipleOf,
			Description:         description,
			Deprecated:          deprecated,
			ReadOnly:            readOnly,
			Example:             example,
			OpenAPIV3Extensions: extensions,
		}}
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_STRING {
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "string",
			Title:               title,
			Description:         description,
			Deprecated:          deprecated,
			Pattern:             pattern,
			MaxLength:           maxLength,
			MinLength:           minLength,
			ReadOnly:            readOnly,
			Example:             example,
			OpenAPIV3Extensions: extensions,
		}}
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_BYTES {
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "string",
			Format:              "byte",
			Title:               title,
			Description:         description,
			Deprecated:          deprecated,
			MaxLength:           maxLength,
			MinLength:           minLength,
			ReadOnly:            readOnly,
			Example:             example,
			OpenAPIV3Extensions: extensions,
		}}
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_ENUM {
		if field.TypeName != nil {
			return &OpenAPIV3SchemaRef{Ref: "#/components/schemas/" + resolvedNames[*field.TypeName]}
		}
	} else if field.TypeName != nil {
		if schema, ok := wellKnownTypesToOpenAPIV3SchemaMapping[*field.TypeName]; ok && schema != nil {
			schemaCopy := *schema // Create a copy to avoid modifying the original schema
			schemaCopy.Title = title
			schemaCopy.Description = description
			schemaCopy.Deprecated = deprecated
			schemaCopy.ReadOnly = readOnly
			schemaCopy.Maximum = maximum
			schemaCopy.Minimum = minimum
			schemaCopy.ExclusiveMaximum = exclusiveMaximum
			schemaCopy.ExclusiveMinimum = exclusiveMinimum
			schemaCopy.MultipleOf = multipleOf
			schemaCopy.Pattern = pattern
			schemaCopy.MaxLength = maxLength
			schemaCopy.MinLength = minLength
			schemaCopy.OpenAPIV3Extensions = extensions
			schemaCopy.Example = example
			return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &schemaCopy}
		} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_MESSAGE {
			fieldMessage, err := registry.LookupMsg(*field.TypeName, *field.TypeName)
			if err != nil {
				log.Printf("Warning: could not lookup message for field %s: %v", *field.Name, err)
				return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{Type: "object"}}
			}
			opts := fieldMessage.GetOptions()
			// We need to check if this field is an actual message, or a message generated by the protobuf compiler
			// to represent a map. Map entry messages have the option map_entry set to true.
			if opts != nil && opts.MapEntry != nil && *opts.MapEntry {
				if len(fieldMessage.Fields) != 2 {
					log.Printf("Warning: map field %s does not have exactly 2 fields", *field.Name)
					return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{Type: "object"}}
				}
				valueField := fieldMessage.Fields[1]
				if valueField == nil {
					log.Printf("Warning: could not find key/value fields for map field %s", *field.Name)
					return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{Type: "object"}}
				}
				return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
					Type:                 "object",
					AdditionalProperties: buildPropertySchemaWithReferencesFromFieldType(valueField, registry, resolvedNames),
					Title:                title,
					Description:          description,
					Deprecated:           deprecated,
					ReadOnly:             readOnly,
					Example:              example,
					OpenAPIV3Extensions:  extensions,
				}}
			} else {
				return &OpenAPIV3SchemaRef{Ref: "#/components/schemas/" + resolvedNames[*field.TypeName]}
			}
		}
	}
	return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{Type: "string"}}
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
		schema := &OpenAPIV3Schema{
			Type:  "array",
			Items: buildPropertySchemaFromFieldType(field, schemaMap, resolvedNames, registry),
		}
		return &OpenAPIV3SchemaRef{
			OpenAPIV3Schema: schema,
		}
	}
	return buildPropertySchemaFromFieldType(field, schemaMap, resolvedNames, registry)
}
func buildPropertySchemaFromFieldType(field *descriptor.Field, schemaMap map[string]*OpenAPIV3SchemaRef, resolvedNames map[string]string, registry *descriptor.Registry) *OpenAPIV3SchemaRef {
	// This function handles the logic from your original code, mapping protobuf types to OpenAPI types.
	var title string
	var maximum float64
	var minimum float64
	var exclusiveMaximum bool
	var exclusiveMinimum bool
	var pattern string
	var maxLength uint64
	var minLength uint64
	var multipleOf float64
	var description string
	var readOnly bool
	var deprecated bool
	var extensions OpenAPIV3Extensions = OpenAPIV3Extensions{}
	var example RawExample
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
			maxLength = fieldExtension.MaxLength
			minLength = fieldExtension.MinLength
			multipleOf = fieldExtension.MultipleOf
			description = fieldExtension.Description
			readOnly = fieldExtension.ReadOnly
			example = RawExample(fieldExtension.Example)
		}
	}
	if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_BOOL {
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "boolean",
			Title:               title,
			Description:         description,
			Deprecated:          deprecated,
			Example:             example,
			OpenAPIV3Extensions: extensions,
		}}
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_DOUBLE {
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
			Example:             example,
			OpenAPIV3Extensions: extensions,
		}}
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_UINT32 {
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "integer",
			Format:              "int64",
			Title:               title,
			Maximum:             maximum,
			Minimum:             max(minimum, 0),
			ExclusiveMaximum:    exclusiveMaximum,
			ExclusiveMinimum:    exclusiveMinimum,
			MultipleOf:          multipleOf,
			Description:         description,
			Deprecated:          deprecated,
			ReadOnly:            readOnly,
			Example:             example,
			OpenAPIV3Extensions: extensions,
		}}
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_UINT64 {
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "string",
			Format:              "int64",
			Title:               title,
			Maximum:             maximum,
			Minimum:             max(minimum, 0),
			ExclusiveMaximum:    exclusiveMaximum,
			ExclusiveMinimum:    exclusiveMinimum,
			MultipleOf:          multipleOf,
			Description:         description,
			Deprecated:          deprecated,
			ReadOnly:            readOnly,
			Example:             example,
			OpenAPIV3Extensions: extensions,
		}}
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_FLOAT {
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
			Example:             example,
			OpenAPIV3Extensions: extensions,
		}}
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_INT32 {
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
			Example:             example,
			OpenAPIV3Extensions: extensions,
		}}
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_INT64 {
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "integer",
			Format:              "int64",
			Title:               title,
			Maximum:             maximum,
			Minimum:             minimum,
			ExclusiveMaximum:    exclusiveMaximum,
			ExclusiveMinimum:    exclusiveMinimum,
			MultipleOf:          multipleOf,
			Description:         description,
			Deprecated:          deprecated,
			ReadOnly:            readOnly,
			Example:             example,
			OpenAPIV3Extensions: extensions,
		}}
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_STRING {
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "string",
			Title:               title,
			Description:         description,
			Deprecated:          deprecated,
			Pattern:             pattern,
			MaxLength:           maxLength,
			MinLength:           minLength,
			ReadOnly:            readOnly,
			Example:             example,
			OpenAPIV3Extensions: extensions,
		}}
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_BYTES {
		return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
			Type:                "string",
			Format:              "byte",
			Title:               title,
			Description:         description,
			Deprecated:          deprecated,
			MaxLength:           maxLength,
			MinLength:           minLength,
			ReadOnly:            readOnly,
			Example:             example,
			OpenAPIV3Extensions: extensions,
		}}
	} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_ENUM {
		return &OpenAPIV3SchemaRef{Ref: "#/components/schemas/" + resolvedNames[*field.TypeName]}
	} else if field.TypeName != nil {
		if schema, ok := wellKnownTypesToOpenAPIV3SchemaMapping[*field.TypeName]; ok && schema != nil {
			schemaCopy := *schema // Create a copy to avoid modifying the original schema
			schemaCopy.Title = title
			schemaCopy.Description = description
			schemaCopy.Deprecated = deprecated
			schemaCopy.ReadOnly = readOnly
			schemaCopy.Maximum = maximum
			schemaCopy.Minimum = minimum
			schemaCopy.ExclusiveMaximum = exclusiveMaximum
			schemaCopy.ExclusiveMinimum = exclusiveMinimum
			schemaCopy.MultipleOf = multipleOf
			schemaCopy.Pattern = pattern
			schemaCopy.MaxLength = maxLength
			schemaCopy.MinLength = minLength
			schemaCopy.OpenAPIV3Extensions = extensions
			schemaCopy.Example = example
			return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &schemaCopy}
		} else if *field.Type == descriptorpb.FieldDescriptorProto_TYPE_MESSAGE {
			schema := &OpenAPIV3Schema{
				Type:       "object",
				Properties: make(map[string]*OpenAPIV3SchemaRef),
			}
			fieldMessage, err := registry.LookupMsg(*field.TypeName, *field.TypeName)
			if err != nil || fieldMessage == nil {
				log.Printf("Warning: could not lookup message for field %s: %v", *field.Name, err)
				return &OpenAPIV3SchemaRef{OpenAPIV3Schema: schema}
			}
			opts := fieldMessage.GetOptions()
			if opts != nil && opts.MapEntry != nil && *opts.MapEntry {
				if len(fieldMessage.Fields) != 2 {
					log.Printf("Warning: map field %s does not have exactly 2 fields", *field.Name)
					return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{Type: "object"}}
				}
				valueField := fieldMessage.Fields[1]
				if valueField == nil {
					log.Printf("Warning: could not find key/value fields for map field %s", *field.Name)
					return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{Type: "object"}}
				}
				return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{
					Type:                 "object",
					AdditionalProperties: buildPropertySchemaFromFieldType(valueField, schemaMap, resolvedNames, registry),
					Title:                title,
					Description:          description,
					Deprecated:           deprecated,
					ReadOnly:             readOnly,
					Example:              example,
					OpenAPIV3Extensions:  extensions,
				}}
			}
			schemaRef := schemaMap[*field.TypeName]
			if schemaRef != nil {
				schema = schemaRef.OpenAPIV3Schema
			} else {
				log.Printf("Warning: could not find schema for message %s", *field.TypeName)
			}
			return &OpenAPIV3SchemaRef{OpenAPIV3Schema: schema}
		}
	}
	return &OpenAPIV3SchemaRef{OpenAPIV3Schema: &OpenAPIV3Schema{Type: "string"}}
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
