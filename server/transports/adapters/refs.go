package adapters

import (
	pbentities "github.com/gsoultan/gobpm/api/proto/entities"
)

// The protobuf entities model relationships as nested messages, matching the
// project's rule that a relational reference is an object rather than an ID
// (.junie/guidelines.md §3). The adapters below had been left writing the older
// flat `*_id string` shape, so `buf generate` produced code that no longer
// compiled — the protos and the checked-in generated code had silently diverged.
//
// `ref` builds the "shell" object the guidelines describe: an entity carrying
// only its identifier, for when the full object is not loaded.

// projectRef returns a Project carrying only its id, or nil.
func projectRef(id string) *pbentities.Project {
	if id == "" {
		return nil
	}
	return &pbentities.Project{Id: id}
}

// organizationRef returns an Organization carrying only its id, or nil.
func organizationRef(id string) *pbentities.Organization {
	if id == "" {
		return nil
	}
	return &pbentities.Organization{Id: id}
}

// instanceRef returns a ProcessInstance carrying only its id, or nil.
func instanceRef(id string) *pbentities.ProcessInstance {
	if id == "" {
		return nil
	}
	return &pbentities.ProcessInstance{Id: id}
}

// definitionRef returns a ProcessDefinition carrying only its id, or nil.
func definitionRef(id string) *pbentities.ProcessDefinition {
	if id == "" {
		return nil
	}
	return &pbentities.ProcessDefinition{Id: id}
}

// nodeRef returns a Node carrying only its id, or nil for an empty id.
func nodeRef(id string) *pbentities.Node {
	if id == "" {
		return nil
	}
	return &pbentities.Node{Id: id}
}

// userRef returns a User carrying only its username, or nil.
func userRef(username string) *pbentities.User {
	if username == "" {
		return nil
	}
	return &pbentities.User{Username: username}
}

// nodeRefs maps node ids to shell Node messages.
func nodeRefs(ids []string) []*pbentities.Node {
	if len(ids) == 0 {
		return nil
	}
	out := make([]*pbentities.Node, 0, len(ids))
	for _, id := range ids {
		if n := nodeRef(id); n != nil {
			out = append(out, n)
		}
	}
	return out
}
