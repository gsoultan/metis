// Package mapping resolves a step's mapping of names to values.
//
// Shared by every step that maps variables — a decision's inputs and outputs,
// and the parameters a connector step hands its connector — so a mapping means
// the same thing wherever it is written. Two copies of this are how one of them
// starts accepting FEEL and the other does not.
package mapping

import (
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/server/domains/logic/feel"
)

// Resolve turns a mapping of target name → source into concrete values.
//
// A source was previously a variable name and nothing else, so a mapping could
// rename a value and no more: computing `total` from `price` and `quantity`
// meant adding a script task beside the decision purely to do the arithmetic.
// A source is now a FEEL expression, which subsumes the old behaviour — a bare
// name is still a variable reference — and adds everything else:
// `price * quantity`, `applicant.address.city`, `sum(items.price)`.
//
// A source that resolves to nothing is omitted rather than written as null,
// which is what the name-only version did: mapping an absent variable left the
// target unset instead of setting it to nothing.
func Resolve(mapping map[string]any, source map[string]any) map[string]any {
	out := make(map[string]any, len(mapping))
	for target, expression := range mapping {
		text, isText := expression.(string)
		if !isText {
			// A non-string source is a constant the author wrote into the
			// mapping; pass it through untouched.
			out[target] = expression
			continue
		}

		// The plain-name case first: it is the common one, it cannot fail, and
		// it keeps a variable whose name is also a FEEL keyword working.
		if value, ok := source[text]; ok {
			out[target] = value
			continue
		}

		value, err := feel.Evaluate(text, source)
		if err != nil {
			log.Warn().
				Err(err).
				Str("target", target).
				Str("expression", text).
				Msg("Mapping expression could not be evaluated; the target is left unset")
			continue
		}
		if value.IsNull() {
			continue
		}
		out[target] = value.ToAny()
	}
	return out
}
