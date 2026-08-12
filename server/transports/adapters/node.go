package adapters

import (
	pbentities "github.com/gsoultan/gobpm/api/proto/entities"
	"github.com/gsoultan/gobpm/server/domains/entities"
)

// NodeToProto converts a domain Node to protobuf Node message.
func NodeToProto(n *entities.Node) *pbentities.Node {
	if n == nil {
		return nil
	}
	// map candidate users/groups
	var users []*pbentities.User
	if len(n.CandidateUsers) > 0 {
		users = make([]*pbentities.User, 0, len(n.CandidateUsers))
		for _, u := range n.CandidateUsers {
			if u != nil {
				users = append(users, UserPBAdapter{User: *u}.ToProto())
			}
		}
	}
	var groups []*pbentities.Group
	if len(n.CandidateGroups) > 0 {
		groups = make([]*pbentities.Group, 0, len(n.CandidateGroups))
		for _, g := range n.CandidateGroups {
			if g != nil {
				groups = append(groups, GroupPBAdapter{Group: *g}.ToProto())
			}
		}
	}
	return &pbentities.Node{
		Id:              n.ID,
		Name:            n.Name,
		Type:            string(n.Type),
		Assignee:        userRef(n.Assignee),
		Incoming:        append([]string(nil), n.Incoming...),
		Outgoing:        append([]string(nil), n.Outgoing...),
		CandidateUsers:  users,
		CandidateGroups: groups,
	}
}

// NodesToProto maps a slice of domain nodes to protobuf nodes.
func NodesToProto(nodes []*entities.Node) []*pbentities.Node {
	if len(nodes) == 0 {
		return nil
	}
	out := make([]*pbentities.Node, len(nodes))
	for i, n := range nodes {
		out[i] = NodeToProto(n)
	}
	return out
}
