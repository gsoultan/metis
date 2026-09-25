package impl

import (
	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// projectOf returns the project a definition was deployed into, or the nil ID
// for one that names none — which the decision service refuses, rather than
// looking a table up across every project a key might match in.
func projectOf(def *entities.ProcessDefinition) uuid.UUID {
	if def == nil || def.Project == nil {
		return uuid.Nil
	}
	return def.Project.ID
}
