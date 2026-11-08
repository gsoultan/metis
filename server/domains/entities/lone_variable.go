package entities

import "strings"

// LoneVariable reports the variable a text that is only "${name}" or
// "#{name}" names: an assignee or candidate written that way means whoever the
// variable holds, read when the task is made.
func LoneVariable(written string) (string, bool) {
	s := strings.TrimSpace(written)
	for _, opening := range []string{"${", "#{"} {
		if rest, ok := strings.CutPrefix(s, opening); ok {
			if inner, ok := strings.CutSuffix(rest, "}"); ok {
				inner = strings.TrimSpace(inner)
				if inner != "" && !strings.ContainsAny(inner, " .()[]+-*/<>=!&|'\"") {
					return inner, true
				}
			}
		}
	}
	return "", false
}
