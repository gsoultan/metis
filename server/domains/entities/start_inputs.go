package entities

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

// StartFormProperty is the start step's property that holds its inputs: what
// a run of the process must be started with. It is a form, in the shape a
// task's form has — a list of fields, or an object holding the list under
// "fields" — so the designer edits both with one editor.
const StartFormProperty = "start_form"

// The kinds of input the engine checks. A field of any other kind is taken as
// it comes: a designer newer than this engine may know kinds this one does
// not, and refusing every start of such a process would be worse than not
// checking one field of it.
const (
	startInputText     = "text"
	startInputLongText = "textarea"
	startInputNumber   = "number"
	startInputYesNo    = "boolean"
	startInputDate     = "date"
	startInputChoice   = "select"
	// A section is a heading in the form. It holds no value.
	startInputSection = "section"
)

// maxStartInputs bounds how many inputs one start step declares. A process
// definition is untrusted input, and every start walks this list.
const maxStartInputs = 200

// StartInput is one thing a run is started with: a variable by name, what
// kind of value it holds, and whether a start may leave it out.
type StartInput struct {
	// ID is the variable the input sets.
	ID string
	// Type is the field's kind, as the form names it.
	Type string
	// Required says a start that does not give the input is refused, unless
	// Default fills it.
	Required bool
	// Default is what the run starts with when the start gives nothing. nil
	// when the field has none.
	Default any
	// Choices are the values a choice accepts. Empty when the field lists
	// none itself — its choices come from somewhere the engine does not
	// read — and then any text is taken.
	Choices []string
	// Conditional says the form shows the field only sometimes. The engine
	// does not evaluate the form's own conditions, so it cannot tell a field
	// left out from a field never shown, and does not require one.
	Conditional bool
}

// HasStartInputs reports whether a step carries a start form at all, readable
// or not.
func HasStartInputs(n *Node) bool {
	if n == nil || n.Properties == nil {
		return false
	}
	raw, present := n.Properties[StartFormProperty]
	if !present || raw == nil {
		return false
	}
	if text, ok := raw.(string); ok {
		return strings.TrimSpace(text) != ""
	}
	return true
}

// StartInputsOf reads the inputs a start step declares. A step with no start
// form declares none, and a run of it starts with whatever it is given.
func StartInputsOf(n *Node) ([]StartInput, error) {
	if !HasStartInputs(n) {
		return nil, nil
	}
	return ParseStartInputs(n.Properties[StartFormProperty])
}

// ParseStartInputs reads inputs from a step property: a form as JSON text, or
// the same form already decoded.
func ParseStartInputs(raw any) ([]StartInput, error) {
	form := raw
	if text, ok := raw.(string); ok {
		form = nil
		if err := json.Unmarshal([]byte(text), &form); err != nil {
			return nil, errors.New("the inputs cannot be read: they are not JSON")
		}
	}
	fields, ok := startFormFields(form)
	if !ok {
		return nil, errors.New("the inputs are not a list of fields")
	}
	if len(fields) > maxStartInputs {
		return nil, fmt.Errorf("the start step declares %d inputs; the most is %d", len(fields), maxStartInputs)
	}

	inputs := make([]StartInput, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for i, field := range fields {
		described, ok := field.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("input %d is not a field", i+1)
		}
		input := startInputOf(described)
		if input.Type == startInputSection {
			continue
		}
		if input.ID == "" {
			return nil, fmt.Errorf("input %d has no name", i+1)
		}
		if _, twice := seen[input.ID]; twice {
			return nil, fmt.Errorf("input %q is declared twice", input.ID)
		}
		seen[input.ID] = struct{}{}
		if input.Default != nil {
			if problem := input.problemWith(input.Default); problem != "" {
				return nil, fmt.Errorf("the default of input %q %s", input.ID, problem)
			}
		}
		inputs = append(inputs, input)
	}
	return inputs, nil
}

// startFormFields returns a form's list of fields: the form itself when it is
// a list, or what it holds under "fields".
func startFormFields(form any) ([]any, bool) {
	switch shaped := form.(type) {
	case []any:
		return shaped, true
	case map[string]any:
		fields, ok := shaped["fields"].([]any)
		return fields, ok
	}
	return nil, false
}

func startInputOf(field map[string]any) StartInput {
	input := StartInput{}
	if id, ok := field["id"].(string); ok {
		input.ID = strings.TrimSpace(id)
	}
	if kind, ok := field["type"].(string); ok {
		input.Type = kind
	}
	if required, ok := field["required"].(bool); ok {
		input.Required = required
	}
	if value, ok := field["defaultValue"]; ok && !isNoStartValue(value) {
		input.Default = value
	}
	if logic, ok := field["logic"].(map[string]any); ok {
		if hidden, ok := logic["hiddenIf"].(string); ok {
			input.Conditional = strings.TrimSpace(hidden) != ""
		}
	}
	if input.Type == startInputChoice && listsItsOwnChoices(field) {
		var options []any
		if listed, ok := field["options"].([]any); ok {
			options = listed
		}
		for _, option := range options {
			described, ok := option.(map[string]any)
			if !ok {
				continue
			}
			if value, ok := described["value"].(string); ok {
				input.Choices = append(input.Choices, value)
			}
		}
	}
	return input
}

// listsItsOwnChoices reports whether a choice's options are the ones written
// on the field, rather than read from a variable or fetched when the form is
// shown.
func listsItsOwnChoices(field map[string]any) bool {
	source, ok := field["dataSource"].(map[string]any)
	if !ok {
		return true
	}
	kind, named := source["type"].(string)
	return !named || kind == "" || kind == "static"
}

// isNoStartValue reports whether a value is the same as giving none: nothing,
// or text with nothing in it. false and 0 are values.
func isNoStartValue(value any) bool {
	if value == nil {
		return true
	}
	text, ok := value.(string)
	return ok && strings.TrimSpace(text) == ""
}

// MissingDefault names the required inputs a start with no variables could not
// fill: those with no default. A process that starts by itself has nobody to
// give them.
func MissingDefault(inputs []StartInput) []string {
	var missing []string
	for _, input := range inputs {
		if input.Required && !input.Conditional && input.Default == nil {
			missing = append(missing, input.ID)
		}
	}
	return missing
}

// AdmitStartInputs checks what a run is being started with against what its
// start step declares, and returns what the run starts with: vars, with the
// default of each input the start left out.
//
// problems lists, by input, why the start is refused; it is empty when the
// start is admitted. vars is never changed: the caller's map may be shared
// between the several runs one message starts.
//
// Variables the step does not declare pass through untouched. A process whose
// start declares nothing starts with anything, as it always did.
func AdmitStartInputs(inputs []StartInput, vars map[string]any) (admitted map[string]any, problems []string) {
	if len(inputs) == 0 {
		return vars, nil
	}
	admitted = make(map[string]any, len(vars)+len(inputs))
	for name, value := range vars {
		admitted[name] = value
	}
	for _, input := range inputs {
		value, given := admitted[input.ID]
		if !given || isNoStartValue(value) {
			if input.Default != nil {
				admitted[input.ID] = input.Default
				continue
			}
			if input.Required && !input.Conditional {
				problems = append(problems, input.ID+" is required")
			}
			continue
		}
		if problem := input.problemWith(value); problem != "" {
			problems = append(problems, input.ID+" "+problem)
		}
	}
	if len(problems) > 0 {
		return nil, problems
	}
	return admitted, nil
}

// problemWith says what is wrong with a value given for the input, as the end
// of a sentence that begins with the input's name. "" when nothing is.
func (i StartInput) problemWith(value any) string {
	switch i.Type {
	case startInputText, startInputLongText:
		if _, ok := value.(string); !ok {
			return "must be text"
		}
	case startInputNumber:
		if !isStartNumber(value) {
			return "must be a number"
		}
	case startInputYesNo:
		if _, ok := value.(bool); !ok {
			return "must be true or false"
		}
	case startInputDate:
		text, ok := value.(string)
		if !ok || !isStartDate(text) {
			return "must be a date, such as 2026-01-31"
		}
	case startInputChoice:
		text, ok := value.(string)
		if !ok {
			return "must be text"
		}
		if len(i.Choices) > 0 && !slices.Contains(i.Choices, text) {
			return "must be one of " + strings.Join(i.Choices, ", ")
		}
	}
	return ""
}

func isStartNumber(value any) bool {
	switch number := value.(type) {
	case float64:
		return !math.IsNaN(number) && !math.IsInf(number, 0)
	case float32:
		return !math.IsNaN(float64(number)) && !math.IsInf(float64(number), 0)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	case json.Number:
		_, err := number.Float64()
		return err == nil
	}
	return false
}

// startDateLayouts are the ways a date arrives: a day, as a date field gives
// it, or an instant.
var startDateLayouts = []string{"2006-01-02", time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02T15:04"}

func isStartDate(text string) bool {
	for _, layout := range startDateLayouts {
		if _, err := time.Parse(layout, text); err == nil {
			return true
		}
	}
	return false
}
