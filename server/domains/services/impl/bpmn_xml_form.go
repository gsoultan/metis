package impl

import (
	"encoding/json"
	"strings"

	"github.com/gsoultan/metis/server/domains/entities"
)

// formDefinitionProperty is where a step keeps the form it asks a person to
// fill in: a list of fields, as the designer saves it.
const formDefinitionProperty = "form_definition"

// bpmnExtensionElements is the part of <extensionElements> this engine reads:
// a Camunda form. Each element is read through its plain name and written
// through its prefixed one, as the attributes on bpmnNode are.
//
// A form is not decoration. A task may set only the variables its form
// declares, so a user task imported without its form could be completed with
// nothing at all: every variable an approval exists to record was refused.
type bpmnExtensionElements struct {
	FormData        *bpmnFormData `xml:"formData"`
	CamundaFormData *bpmnFormData `xml:"camunda:formData"`
}

type bpmnFormData struct {
	Fields        []bpmnFormField `xml:"formField"`
	CamundaFields []bpmnFormField `xml:"camunda:formField"`
}

type bpmnFormField struct {
	ID           string          `xml:"id,attr"`
	Label        string          `xml:"label,attr,omitempty"`
	Type         string          `xml:"type,attr,omitempty"`
	DefaultValue string          `xml:"defaultValue,attr,omitempty"`
	Values       []bpmnFormValue `xml:"value"`
	CamundaVals  []bpmnFormValue `xml:"camunda:value"`
}

type bpmnFormValue struct {
	ID   string `xml:"id,attr"`
	Name string `xml:"name,attr,omitempty"`
}

// Camunda's form field types and the designer's, both ways. A Camunda type
// with no designer equivalent reads as text, which is what Camunda's own task
// list does with a custom type.
var (
	camundaToFieldType = map[string]string{
		"string": "text", "long": "number", "boolean": "boolean", "date": "date", "enum": "select",
	}
	fieldToCamundaType = map[string]string{
		"text": "string", "textarea": "string", "number": "long", "boolean": "boolean", "date": "date", "select": "enum",
	}
)

// importedForm is the form a step's <extensionElements> carries, as the list
// of fields the designer would have saved, or nil when it carries none.
func importedForm(ext *bpmnExtensionElements) []any {
	if ext == nil || ext.FormData == nil {
		return nil
	}
	var fields []any
	for _, f := range ext.FormData.Fields {
		id := strings.TrimSpace(f.ID)
		if id == "" {
			continue
		}
		kind, ok := camundaToFieldType[strings.TrimSpace(f.Type)]
		if !ok {
			kind = "text"
		}
		field := map[string]any{"id": id, "label": cmpLabel(f.Label, id), "type": kind}
		if f.DefaultValue != "" {
			field["defaultValue"] = f.DefaultValue
		}
		if kind == "select" {
			options := make([]any, 0, len(f.Values))
			for _, v := range f.Values {
				options = append(options, map[string]any{"value": v.ID, "label": cmpLabel(v.Name, v.ID)})
			}
			field["options"] = options
		}
		fields = append(fields, field)
	}
	return fields
}

// exportedForm writes a step's form as Camunda's formData, or nil when the
// step has none. Sections are layout, not fields, and are left out.
func exportedForm(n *entities.Node) *bpmnExtensionElements {
	raw := entities.PropertyText(n.Properties, formDefinitionProperty)
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var form any
	if json.Unmarshal([]byte(raw), &form) != nil {
		return nil
	}
	data := &bpmnFormData{}
	for _, item := range fieldsOf(form) {
		field, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := field["id"].(string)
		kind, _ := field["type"].(string)
		camundaType, known := fieldToCamundaType[kind]
		if id == "" || !known {
			continue
		}
		label, _ := field["label"].(string)
		out := bpmnFormField{ID: id, Label: label, Type: camundaType}
		if v, ok := field["defaultValue"].(string); ok {
			out.DefaultValue = v
		}
		if options, ok := field["options"].([]any); ok {
			for _, o := range options {
				option, _ := o.(map[string]any)
				value, _ := option["value"].(string)
				name, _ := option["label"].(string)
				if value != "" {
					out.CamundaVals = append(out.CamundaVals, bpmnFormValue{ID: value, Name: name})
				}
			}
		}
		data.CamundaFields = append(data.CamundaFields, out)
	}
	if len(data.CamundaFields) == 0 {
		return nil
	}
	return &bpmnExtensionElements{CamundaFormData: data}
}

func cmpLabel(label, fallback string) string {
	if strings.TrimSpace(label) != "" {
		return label
	}
	return fallback
}
