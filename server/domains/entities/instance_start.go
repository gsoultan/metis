package entities

import "github.com/google/uuid"

// MaxBusinessKeyLength is the longest business key an instance can carry, in
// characters: the column is varchar(255).
const MaxBusinessKeyLength = 255

// InstanceStart is everything a new instance is started with.
//
// Version zero starts the live version. A ParentInstanceID other than
// uuid.Nil makes it a sub-process of that instance, called from ParentNodeID.
type InstanceStart struct {
	ProjectID     uuid.UUID
	DefinitionKey string
	Version       int
	Variables     map[string]any
	// BusinessKey is the caller's name for the case. Empty for none.
	BusinessKey string

	ParentInstanceID uuid.UUID
	ParentNodeID     string
}
