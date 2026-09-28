package testutils

// FormDeclaring returns the properties of a user task whose form has one field
// per id, in the shape the designer saves a form: a list of fields under
// form_definition.
//
// Completing a task sets only the variables its form declares, so a step that
// a test completes with variables has to declare them, as a step built in the
// designer does.
func FormDeclaring(ids ...string) map[string]any {
	fields := make([]any, 0, len(ids))
	for _, id := range ids {
		fields = append(fields, map[string]any{"id": id, "label": id, "type": "text"})
	}
	return map[string]any{"form_definition": fields}
}
