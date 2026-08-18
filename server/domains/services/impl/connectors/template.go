package connectors

import (
	"fmt"
	"strings"

	"github.com/gsoultan/gobpm/server/domains/logic/feel"
)

// Filling in a manifest's templates.
//
// A manifest says `{{config.instance_url}}/services/data/sobjects/Lead` and
// `{"LastName": "{{input.last_name}}"}`. The braces are the only syntax this
// adds; everything inside them is FEEL, the same language conditions, mappings
// and decision cells already use. One language across the product is worth more
// than a template syntax tuned for templates.

const (
	openTag  = "{{"
	closeTag = "}}"
)

// maxTemplateExpressions bounds how many substitutions one template may make.
//
// A manifest is installed by an administrator rather than posted by a stranger,
// so this is a guard rail and not a defence — but an installed document is still
// a document, and one that expands a thousand expressions per call is a
// performance problem nobody will trace back to here.
const maxTemplateExpressions = 64

// renderString substitutes every `{{ … }}` in a template.
//
// A template that is *entirely* one expression returns that expression's value
// with its type intact — `{{input.amount}}` is the number 500, not the text
// "500" — because a body field that should be a number and arrives as a string
// is rejected by roughly every API that cares.
func renderString(template string, scope map[string]any) (any, error) {
	if !strings.Contains(template, openTag) {
		return template, nil
	}

	// The whole-template case, checked first so the type survives.
	trimmed := strings.TrimSpace(template)
	if strings.HasPrefix(trimmed, openTag) && strings.HasSuffix(trimmed, closeTag) {
		inner := trimmed[len(openTag) : len(trimmed)-len(closeTag)]
		if !strings.Contains(inner, openTag) {
			return evaluate(inner, scope)
		}
	}

	var out strings.Builder
	rest := template
	for count := 0; strings.Contains(rest, openTag); count++ {
		if count >= maxTemplateExpressions {
			return nil, fmt.Errorf("connector template: more than %d expressions in one value", maxTemplateExpressions)
		}
		before, after, _ := strings.Cut(rest, openTag)
		expression, remainder, closed := strings.Cut(after, closeTag)
		if !closed {
			return nil, fmt.Errorf("connector template: %q opens an expression that is never closed", template)
		}

		value, err := evaluate(expression, scope)
		if err != nil {
			return nil, err
		}
		out.WriteString(before)
		out.WriteString(asText(value))
		rest = remainder
	}
	out.WriteString(rest)
	return out.String(), nil
}

// renderValue substitutes templates anywhere inside a body.
//
// A body is nested — objects inside arrays inside objects — and the templates
// are in the leaves, so this walks rather than looking only at the top level.
func renderValue(value any, scope map[string]any) (any, error) {
	switch v := value.(type) {
	case string:
		return renderString(v, scope)
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			rendered, err := renderValue(item, scope)
			if err != nil {
				return nil, err
			}
			// A field whose template resolved to nothing is omitted rather than
			// sent as null: most APIs treat an explicit null as "clear this
			// field", which is not what an absent input meant.
			if rendered == nil {
				continue
			}
			out[key] = rendered
		}
		return out, nil
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			rendered, err := renderValue(item, scope)
			if err != nil {
				return nil, err
			}
			out = append(out, rendered)
		}
		return out, nil
	default:
		return value, nil
	}
}

// renderStrings substitutes templates in a map of header or query values.
func renderStrings(in map[string]string, scope map[string]any) (map[string]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(in))
	for key, template := range in {
		value, err := renderString(template, scope)
		if err != nil {
			return nil, err
		}
		text := asText(value)
		// An empty header is not the same as no header — some APIs reject one
		// and ignore the other — so a template that resolved to nothing leaves
		// the header off entirely.
		if text == "" {
			continue
		}
		out[key] = text
	}
	return out, nil
}

// evaluate runs one expression, returning nothing for a null.
func evaluate(expression string, scope map[string]any) (any, error) {
	value, err := feel.Evaluate(strings.TrimSpace(expression), scope)
	if err != nil {
		return nil, fmt.Errorf("connector template: could not read %q: %w", strings.TrimSpace(expression), err)
	}
	if value.Kind == feel.KindNull {
		return nil, nil
	}
	return value.ToAny(), nil
}

// asText renders a value for a place that can only hold text — a URL, a header.
func asText(value any) string {
	if value == nil {
		return ""
	}
	if text, isText := value.(string); isText {
		return text
	}
	return fmt.Sprintf("%v", value)
}
