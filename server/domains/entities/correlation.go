package entities

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// correlationPlaceholder matches a single ${...} placeholder in a correlation key template.
var correlationPlaceholder = regexp.MustCompile(`\$\{([^}]*)\}`)

// ResolveCorrelationKey renders a message correlation key template against one
// process instance's variables.
//
// A BPMN message is point-to-point: the correlation key is what picks a single
// waiting instance out of all the instances parked on the same message name. The
// designer writes it as a template over instance variables ("${orderId}"), so it
// has to be evaluated per instance — persisting the raw template would give every
// instance of a definition the identical key and collapse correlation into a
// broadcast.
//
// A template containing no placeholder is a static correlation key and is
// returned unchanged.
//
// An unresolved placeholder is an error, never an empty string. An empty
// correlation key means "do not filter by correlation" in
// SubscriptionRepository.FindMessages, so degrading to empty would take a message
// addressed to one instance and deliver it to every instance waiting on that
// message name.
func ResolveCorrelationKey(template string, vars map[string]any) (string, error) {
	if !strings.Contains(template, "${") {
		return template, nil
	}

	var missing []string
	resolved := correlationPlaceholder.ReplaceAllStringFunc(template, func(match string) string {
		name := strings.TrimSpace(correlationPlaceholder.FindStringSubmatch(match)[1])
		val, ok := vars[name]
		if !ok || val == nil {
			missing = append(missing, name)
			return ""
		}
		return correlationValueToString(val)
	})

	if len(missing) > 0 {
		return "", fmt.Errorf("correlation key %q references undefined variable(s): %s", template, strings.Join(missing, ", "))
	}
	// An unbalanced placeholder ("${orderId" with no closing brace) matches no
	// pattern, so it would otherwise pass through as literal text and be stored
	// as the correlation key — which is precisely the silent mismatch this
	// function exists to prevent. A leftover "${" means the template is
	// malformed, not that it is static.
	if strings.Contains(resolved, "${") {
		return "", fmt.Errorf("correlation key %q has an unterminated ${...} placeholder", template)
	}
	if resolved == "" {
		return "", fmt.Errorf("correlation key %q resolved to an empty value", template)
	}

	return resolved, nil
}

// correlationValueToString renders one variable value as a correlation key segment.
// JSON numbers arrive as float64, so integral values must print without an
// exponent or a trailing ".0": an order id of 4200 has to correlate as "4200",
// which is what the sender put on the wire.
func correlationValueToString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 32)
	default:
		return fmt.Sprint(v)
	}
}
