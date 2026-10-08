package gengateway

import (
	"fmt"
	"strings"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/internal/descriptor"
	openapiv3options "github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv3/options"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

const (
	requiredMaskCheck = `runtime.RequireFieldMaskQueryParameter(protoReq.GetUpdateMask(), "update_mask")`
	maskFromBody      = `runtime.FieldMaskFromRequestBody(`
)

// requiredMaskCase describes one update binding.
type requiredMaskCase struct {
	httpMethod string
	// body is "" for no body, "*" for the whole request, or the name of the body field.
	body string
	// masks is the number of FieldMask fields in the request message.
	masks int
	// required is the openapiv3_schema json_schema.required list. nil means no option.
	required []string
}

func requiredMaskFixture(c requiredMaskCase) *descriptor.File {
	fieldDescs := []*descriptorpb.FieldDescriptorProto{{
		Name:     proto.String("widget"),
		JsonName: proto.String("widget"),
		Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
		Number:   proto.Int32(1),
	}}
	for i := 0; i < c.masks; i++ {
		name, jsonName := "update_mask", "updateMask"
		if i > 0 {
			name, jsonName = fmt.Sprintf("other_mask_%d", i), fmt.Sprintf("otherMask%d", i)
		}
		fieldDescs = append(fieldDescs, &descriptorpb.FieldDescriptorProto{
			Name:     proto.String(name),
			JsonName: proto.String(jsonName),
			Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
			TypeName: proto.String(".google.protobuf.FieldMask"),
			Number:   proto.Int32(int32(100 + i)),
		})
	}
	msgdesc := &descriptorpb.DescriptorProto{
		Name:  proto.String("UpdateWidgetRequest"),
		Field: fieldDescs,
	}
	if c.required != nil {
		msgdesc.Options = &descriptorpb.MessageOptions{}
		proto.SetExtension(msgdesc.Options, openapiv3options.E_Openapiv3Schema, &openapiv3options.Schema{
			JsonSchema: &openapiv3options.JSONSchema{Required: c.required},
		})
	}
	msg := &descriptor.Message{DescriptorProto: msgdesc}
	for _, fd := range fieldDescs {
		msg.Fields = append(msg.Fields, &descriptor.Field{Message: msg, FieldDescriptorProto: fd})
	}

	binding := &descriptor.Binding{HTTPMethod: c.httpMethod}
	switch c.body {
	case "":
	case "*":
		binding.Body = &descriptor.Body{}
	default:
		binding.Body = &descriptor.Body{FieldPath: descriptor.FieldPath{descriptor.FieldPathComponent{
			Name:   c.body,
			Target: msg.Fields[0],
		}}}
	}

	meth := &descriptorpb.MethodDescriptorProto{
		Name:       proto.String("UpdateWidget"),
		InputType:  proto.String("UpdateWidgetRequest"),
		OutputType: proto.String("UpdateWidgetRequest"),
	}
	svc := &descriptorpb.ServiceDescriptorProto{
		Name:   proto.String("WidgetService"),
		Method: []*descriptorpb.MethodDescriptorProto{meth},
	}
	return crossLinkFixture(&descriptor.File{
		FileDescriptorProto: &descriptorpb.FileDescriptorProto{
			Name:        proto.String("widget.proto"),
			Package:     proto.String("example"),
			MessageType: []*descriptorpb.DescriptorProto{msgdesc},
			Service:     []*descriptorpb.ServiceDescriptorProto{svc},
		},
		GoPkg: descriptor.GoPackage{
			Path: "example.com/path/to/example/example.pb",
			Name: "example_pb",
		},
		Messages: []*descriptor.Message{msg},
		Services: []*descriptor.Service{{
			ServiceDescriptorProto: svc,
			Methods: []*descriptor.Method{{
				MethodDescriptorProto: meth,
				RequestType:           msg,
				ResponseType:          msg,
				Bindings:              []*descriptor.Binding{binding},
			}},
		}},
	})
}

func generateRequiredMask(t *testing.T, c requiredMaskCase, requireFieldMaskInQuery, fieldMaskJSONNamesInQuery bool) string {
	t.Helper()
	got, err := applyTemplate(param{
		File:                      requiredMaskFixture(c),
		RegisterFuncSuffix:        "Handler",
		AllowPatchFeature:         true,
		FieldMaskJSONNamesInQuery: fieldMaskJSONNamesInQuery,
		RequireFieldMaskInQuery:   requireFieldMaskInQuery,
	}, descriptor.NewRegistry())
	if err != nil {
		t.Fatalf("applyTemplate(%+v) failed with %v; want success", c, err)
	}
	return got
}

// assertCheckApplied verifies that both the client and the local handler reject
// a missing query mask, after the query is parsed, and never build the mask from the body.
func assertCheckApplied(t *testing.T, c requiredMaskCase) {
	t.Helper()
	for _, jsonNames := range []bool{false, true} {
		got := generateRequiredMask(t, c, true, jsonNames)
		if n := strings.Count(got, requiredMaskCheck); n != 2 {
			t.Errorf("fieldmask_json_names_in_query=%v: generated handlers contain %d required-mask checks; want 2\n%s", jsonNames, n, got)
		}
		if strings.Contains(got, maskFromBody) {
			t.Errorf("fieldmask_json_names_in_query=%v: generated handlers build the FieldMask from the body\n%s", jsonNames, got)
		}
		for _, handler := range strings.Split(got, "func local_request_")[0:2] {
			parse := strings.Index(handler, "PopulateQueryParameters")
			check := strings.Index(handler, requiredMaskCheck)
			if parse < 0 || check < parse {
				t.Errorf("fieldmask_json_names_in_query=%v: required-mask check runs before the query is parsed\n%s", jsonNames, handler)
			}
		}
	}
}

// assertUnchanged verifies that the flag changes nothing in the generated code.
func assertUnchanged(t *testing.T, c requiredMaskCase) {
	t.Helper()
	for _, jsonNames := range []bool{false, true} {
		with := generateRequiredMask(t, c, true, jsonNames)
		without := generateRequiredMask(t, c, false, jsonNames)
		if with != without {
			t.Errorf("fieldmask_json_names_in_query=%v: require_fieldmask_in_query changed the generated code\nwith:\n%s\nwithout:\n%s", jsonNames, with, without)
		}
		if strings.Contains(with, "RequireFieldMaskQueryParameter") {
			t.Errorf("fieldmask_json_names_in_query=%v: generated handlers contain a required-mask check\n%s", jsonNames, with)
		}
	}
}

func TestRequireFieldMaskInQueryApplies(t *testing.T) {
	for name, c := range map[string]requiredMaskCase{
		"PATCH, named body, mask required by proto name": {httpMethod: "PATCH", body: "widget", masks: 1, required: []string{"update_mask"}},
		"PATCH, named body, mask required by JSON name":  {httpMethod: "PATCH", body: "widget", masks: 1, required: []string{"updateMask"}},
		"PATCH, no body, mask required":                  {httpMethod: "PATCH", body: "", masks: 1, required: []string{"update_mask"}},
	} {
		t.Run(name, func(t *testing.T) { assertCheckApplied(t, c) })
	}
}

func TestRequireFieldMaskInQueryDoesNotApply(t *testing.T) {
	for name, c := range map[string]requiredMaskCase{
		"PATCH, named body, mask optional":           {httpMethod: "PATCH", body: "widget", masks: 1, required: []string{"widget"}},
		"PATCH, named body, empty required list":     {httpMethod: "PATCH", body: "widget", masks: 1, required: []string{}},
		"PATCH, named body, no openapiv3_schema":     {httpMethod: "PATCH", body: "widget", masks: 1},
		"PATCH, no body, mask optional":              {httpMethod: "PATCH", body: "", masks: 1, required: []string{"widget"}},
		"PATCH, body *, mask required":               {httpMethod: "PATCH", body: "*", masks: 1, required: []string{"update_mask"}},
		"PATCH, two FieldMask fields, one required":  {httpMethod: "PATCH", body: "widget", masks: 2, required: []string{"update_mask"}},
		"PUT, named body, mask required":             {httpMethod: "PUT", body: "widget", masks: 1, required: []string{"update_mask"}},
		"POST, named body, mask required":            {httpMethod: "POST", body: "widget", masks: 1, required: []string{"update_mask"}},
		"PATCH, named body, other field named alike": {httpMethod: "PATCH", body: "widget", masks: 1, required: []string{"update_mask_extra", "UpdateMask"}},
	} {
		t.Run(name, func(t *testing.T) { assertUnchanged(t, c) })
	}
}

// The AI evaluations shape: PATCH, body "*", and an optional update_mask inside the body.
func TestRequireFieldMaskInQueryKeepsBodyStarWithOptionalMask(t *testing.T) {
	assertUnchanged(t, requiredMaskCase{httpMethod: "PATCH", body: "*", masks: 1, required: []string{"id"}})
}

// The case settings shape: PATCH with a named body and no FieldMask field.
func TestRequireFieldMaskInQueryKeepsPatchWithoutFieldMask(t *testing.T) {
	assertUnchanged(t, requiredMaskCase{httpMethod: "PATCH", body: "widget", masks: 0, required: []string{"widget"}})
}

// Without the flag, a required mask keeps the default behavior: the handler builds it from the body.
func TestRequireFieldMaskInQueryIsOffByDefault(t *testing.T) {
	got := generateRequiredMask(t, requiredMaskCase{httpMethod: "PATCH", body: "widget", masks: 1, required: []string{"update_mask"}}, false, false)
	if strings.Contains(got, "RequireFieldMaskQueryParameter") {
		t.Errorf("generated handlers contain a required-mask check without the flag\n%s", got)
	}
	if n := strings.Count(got, maskFromBody); n != 2 {
		t.Errorf("generated handlers build the FieldMask from the body %d times; want 2\n%s", n, got)
	}
}
