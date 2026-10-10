package impl

// storedWaiveCommand is a waive as a request keeps it: what was asked for, in
// names of its own.
//
// It is written out field by field and not marshalled from the command's
// entity, because a request is a record somebody reads a month later: it must
// not change shape when the entity gains a field (Ruling 16). An approval is
// made from this and from nothing else the request holds.
type storedWaiveCommand struct {
	InstanceID string         `json:"instance_id"`
	Kind       string         `json:"kind"`
	NodeID     string         `json:"node_id"`
	Reason     string         `json:"reason"`
	Outputs    map[string]any `json:"outputs,omitzero"`
	VisitKey   string         `json:"visit_key"`
}
