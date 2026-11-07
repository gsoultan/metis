package impl

import (
	"fmt"
	"strings"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
)

// Deciding who does the work.
//
// An approval matrix — "under 10k the team lead, over 10k the CFO, anything
// from a new supplier goes to compliance" — is business policy, and it changes
// far more often than the shape of the process does. Written into a diagram it
// takes a modeller, a redeploy and a regression test to change a threshold.
//
// A user task can instead name a decision table, and take its assignee,
// candidate groups, priority and due date from whatever that table decides. The
// process says *that* somebody approves; the table says who.

// AssignmentDecisionKey is the node property naming the table that decides.
const AssignmentDecisionKey = "assignment_decision_key"

// AssignmentDecisionVersion pins a version, as business rule tasks can.
const AssignmentDecisionVersion = "assignment_decision_version"

// The output columns an assignment table may set. Anything else it produces is
// ignored here — a table is free to decide several things at once and have only
// some of them mean assignment.
const (
	assignmentAssignee        = "assignee"
	assignmentCandidateUsers  = "candidate_users"
	assignmentCandidateGroups = "candidate_groups"
	assignmentPriority        = "priority"
	assignmentDueDate         = "due_date"
)

// applyAssignment overlays a decision's outputs onto a node.
//
// Only what the decision actually returned is applied: a table that decides the
// group but not the priority leaves the priority as the diagram set it. That is
// what makes this additive — a node with no assignment table behaves exactly as
// it always did, and one with a table that returns nothing does too.
func applyAssignment(node entities.Node, values map[string]any) entities.Node {
	if assignee, ok := textValue(values[assignmentAssignee]); ok {
		node.Assignee = assignee
	}
	if users, ok := textListValue(values[assignmentCandidateUsers]); ok {
		node.CandidateUsers = make([]*entities.User, len(users))
		for i, username := range users {
			node.CandidateUsers[i] = &entities.User{Username: username}
		}
	}
	if groups, ok := textListValue(values[assignmentCandidateGroups]); ok {
		node.CandidateGroups = make([]*entities.Group, len(groups))
		for i, name := range groups {
			node.CandidateGroups[i] = &entities.Group{Name: name}
		}
	}
	if priority, ok := intValue(values[assignmentPriority]); ok {
		node.Priority = priority
	}
	if due, ok := dueDateValue(values[assignmentDueDate]); ok {
		node.DueDate = due
	}
	return node
}

// textValue reads a string output, ignoring an empty one.
//
// An empty assignee is not "assign to nobody" — it is a table that had nothing
// to say, and overwriting the diagram's value with it would silently unassign a
// task the modeller had assigned.
func textValue(raw any) (string, bool) {
	text, isText := raw.(string)
	if !isText {
		return "", false
	}
	trimmed := strings.TrimSpace(text)
	return trimmed, trimmed != ""
}

// textListValue reads a list output, which a table may express as a real list
// or as one comma-separated cell — the second is what an author types into a
// grid.
func textListValue(raw any) ([]string, bool) {
	switch v := raw.(type) {
	case []string:
		return trimmedNonEmpty(v), len(trimmedNonEmpty(v)) > 0
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if text, ok := textValue(item); ok {
				out = append(out, text)
			}
		}
		return out, len(out) > 0
	case string:
		out := trimmedNonEmpty(strings.Split(v, ","))
		return out, len(out) > 0
	default:
		return nil, false
	}
}

func trimmedNonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, item := range in {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// intValue reads a numeric output. Decision outputs arrive as float64 through
// JSON whatever the author typed.
func intValue(raw any) (int, bool) {
	switch v := raw.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	default:
		return 0, false
	}
}

// dueDateValue reads a due date, as either an instant or a duration from now.
//
// A table that decides "urgent things are due in four hours" cannot write an
// absolute timestamp — it does not know when the task will be created. An
// ISO-8601 duration is how it says that, and it is resolved here against the
// moment the task appears.
func dueDateValue(raw any) (string, bool) {
	text, ok := textValue(raw)
	if !ok {
		return "", false
	}
	if _, err := time.Parse(time.RFC3339, text); err == nil {
		return text, true
	}
	// ParseTimerExpression already reads both an ISO-8601 duration and an
	// absolute instant, which is exactly the pair a due date can be. Reusing it
	// keeps one answer to "what does P4H mean" rather than two that drift.
	now := time.Now()
	if at, err := entities.ParseTimerExpression(text, now); err == nil && at.After(now) {
		return at.Format(time.RFC3339), true
	}
	// Neither an instant nor a duration. Left alone rather than guessed at: a
	// due date nobody can parse is better absent than wrong.
	return "", false
}

// assignmentDecisionOf reads the table a node names, if it names one.
func assignmentDecisionOf(node entities.Node) (key string, version int) {
	key = strings.TrimSpace(node.GetStringProperty(AssignmentDecisionKey))
	if key == "" {
		return "", 0
	}
	switch v := node.Properties[AssignmentDecisionVersion].(type) {
	case float64:
		version = int(v)
	case int:
		version = v
	}
	return key, version
}

// describeAssignment is what the timeline says about it.
func describeAssignment(key string, values map[string]any) string {
	parts := make([]string, 0, 4)
	if assignee, ok := textValue(values[assignmentAssignee]); ok {
		parts = append(parts, "assignee "+assignee)
	}
	if groups, ok := textListValue(values[assignmentCandidateGroups]); ok {
		parts = append(parts, "groups "+strings.Join(groups, ", "))
	}
	if priority, ok := intValue(values[assignmentPriority]); ok {
		parts = append(parts, fmt.Sprintf("priority %d", priority))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%s decided nothing about who should do this", key)
	}
	return fmt.Sprintf("%s set %s", key, strings.Join(parts, ", "))
}

// resolveVariableAssignment reads an assignee or candidate written as a lone
// variable — "${submittedBy}", as Camunda models write "${initiator}" — from
// the instance's variables when the task is made.
//
// Only a lone variable is read. Anything else in the braces is left as
// written, which names nobody, as it did before: an expression language here
// would be a second one to sandbox. A variable that is missing or empty
// leaves the name as written too, so the task is reported as stuck rather
// than quietly offered to nobody. The definition's node is not changed: the
// candidate lists are copied before anything in them is replaced.
func resolveVariableAssignment(node entities.Node, vars map[string]any) entities.Node {
	if name, ok := loneVariable(node.Assignee); ok {
		if who, ok := textValue(vars[name]); ok {
			node.Assignee = who
		}
	}
	if anyLoneVariable(len(node.CandidateUsers), func(i int) string { return userName(node.CandidateUsers[i]) }) {
		users := make([]*entities.User, 0, len(node.CandidateUsers))
		for _, u := range node.CandidateUsers {
			for _, who := range expandName(userName(u), vars) {
				users = append(users, &entities.User{Username: who})
			}
		}
		node.CandidateUsers = users
	}
	if anyLoneVariable(len(node.CandidateGroups), func(i int) string { return groupName(node.CandidateGroups[i]) }) {
		groups := make([]*entities.Group, 0, len(node.CandidateGroups))
		for _, g := range node.CandidateGroups {
			name := groupName(g)
			if _, isVariable := loneVariable(name); !isVariable {
				groups = append(groups, g)
				continue
			}
			for _, who := range expandName(name, vars) {
				groups = append(groups, &entities.Group{Name: who})
			}
		}
		node.CandidateGroups = groups
	}
	return node
}

// loneVariable reports the variable "${name}" or "#{name}" names.
func loneVariable(written string) (string, bool) {
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

func anyLoneVariable(n int, at func(int) string) bool {
	for i := range n {
		if _, ok := loneVariable(at(i)); ok {
			return true
		}
	}
	return false
}

// expandName is the names one written name stands for: itself, or what the
// variable it names holds — one name, a list, or a comma-separated text.
func expandName(written string, vars map[string]any) []string {
	name, ok := loneVariable(written)
	if !ok {
		if written == "" {
			return nil
		}
		return []string{written}
	}
	if values, ok := textListValue(vars[name]); ok {
		return values
	}
	return []string{written}
}

func userName(u *entities.User) string {
	if u == nil {
		return ""
	}
	return u.Username
}

func groupName(g *entities.Group) string {
	if g == nil {
		return ""
	}
	return g.Name
}
