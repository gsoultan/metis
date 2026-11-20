package impl

import (
	"fmt"
	"strings"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

// maxStartProblemsShown bounds how many refused inputs one error names. The
// rest are counted, so a start with a hundred wrong inputs answers in a line.
const maxStartProblemsShown = 10

// admittedStartVariables returns what a run begun at start starts with: vars,
// with the default of each declared input the start left out. It refuses a
// start that leaves out a required input or gives one the wrong kind of
// value, naming each.
//
// It is asked for every way into a new run — the API, a message, a signal, a
// schedule — because they all begin a run through the same command.
func admittedStartVariables(processKey string, start *entities.Node, vars map[string]any) (map[string]any, error) {
	inputs, err := entities.StartInputsOf(start)
	if err != nil {
		// Deploy refuses inputs it cannot read, so this is a version stored
		// some other way. Starting it unchecked would be the wrong guess.
		return nil, apierr.Invalidf("%s cannot be started: %v", processKey, err)
	}
	admitted, problems := entities.AdmitStartInputs(inputs, vars)
	if len(problems) == 0 {
		return admitted, nil
	}
	shown := problems
	more := ""
	if len(shown) > maxStartProblemsShown {
		more = fmt.Sprintf("; and %d more", len(shown)-maxStartProblemsShown)
		shown = shown[:maxStartProblemsShown]
	}
	return nil, apierr.Invalidf("%s cannot be started: %s%s", processKey, strings.Join(shown, "; "), more)
}
