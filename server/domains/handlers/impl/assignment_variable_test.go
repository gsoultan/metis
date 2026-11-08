package impl

import (
	"slices"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// A task given to "${submittedBy}" goes to whoever the variable names, and the
// definition's node, which every instance shares, is left as written.
func TestAnAssigneeWrittenAsAVariableIsReadWhenTheTaskIsMade(t *testing.T) {
	users := []*entities.User{{Username: "${reviewers}"}, {Username: "ada"}}
	groups := []*entities.Group{{Name: "${team}"}, {Name: "finance"}}
	node := entities.Node{ID: "revise", Assignee: "${submittedBy}", CandidateUsers: users, CandidateGroups: groups}

	got := resolveVariableAssignment(node, map[string]any{
		"submittedBy": "rina",
		"reviewers":   []any{"bo", "cy"},
		"team":        "night-shift, day-shift",
	})
	if got.Assignee != "rina" {
		t.Errorf("assignee %q, want rina", got.Assignee)
	}
	var names []string
	for _, u := range got.CandidateUsers {
		names = append(names, u.Username)
	}
	if !slices.Equal(names, []string{"bo", "cy", "ada"}) {
		t.Errorf("candidate users %v", names)
	}
	names = nil
	for _, g := range got.CandidateGroups {
		names = append(names, g.Name)
	}
	if !slices.Equal(names, []string{"night-shift", "day-shift", "finance"}) {
		t.Errorf("candidate groups %v", names)
	}
	if users[0].Username != "${reviewers}" || groups[0].Name != "${team}" {
		t.Error("the definition's node was changed")
	}
}

// What is not a lone variable, or names a variable that is not there, is left
// as written: the task is then reported as stuck rather than given to nobody.
func TestAnAssigneeThatIsNotALoneVariableIsLeftAsWritten(t *testing.T) {
	node := entities.Node{Assignee: "${a.b}", CandidateUsers: []*entities.User{{Username: "${missing}"}}}
	got := resolveVariableAssignment(node, map[string]any{"a": "x"})
	if got.Assignee != "${a.b}" || got.CandidateUsers[0].Username != "${missing}" {
		t.Errorf("got %q and %q", got.Assignee, got.CandidateUsers[0].Username)
	}
	plain := entities.Node{Assignee: "dana"}
	if resolveVariableAssignment(plain, map[string]any{"dana": "x"}).Assignee != "dana" {
		t.Error("a plain name was read as a variable")
	}
}
