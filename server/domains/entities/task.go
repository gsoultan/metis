package entities

import (
	"time"

	"github.com/google/uuid"
)

// Task represents a user task or an activity in a process instance.
type Task struct {
	ID       uuid.UUID        `json:"id"`
	Project  *Project         `json:"project,omitzero"`
	Instance *ProcessInstance `json:"instance,omitzero"`
	Node     *Node            `json:"node,omitzero"`
	// IterationID names which run of a multi-instance step this task is for.
	// Empty on a task of a step that runs once, and on a task created before
	// the column existed.
	IterationID string     `json:"iteration_id,omitzero"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitzero"`
	Type        NodeType   `json:"type"`
	Status      TaskStatus `json:"status"` // e.g., "unclaimed", "claimed", "completed"
	Assignee    *User      `json:"assignee,omitzero"`
	// Owner is who delegated the task and waits for it back; nil for a task
	// that has not been delegated.
	Owner *User `json:"owner,omitzero"`
	// DelegationState is "pending" while a delegate has the task and
	// "resolved" once they handed it back.
	DelegationState DelegationState `json:"delegation_state,omitzero"`
	CandidateUsers  []*User         `json:"candidate_users,omitzero"`
	CandidateGroups []*Group        `json:"candidate_groups,omitzero"`
	Priority        int             `json:"priority,omitzero"`
	DueDate         *time.Time      `json:"due_date,omitzero"`
	FormKey         string          `json:"form_key,omitzero"`
	FormDefinition  string          `json:"form_definition,omitzero"`
	Variables       map[string]any  `json:"variables,omitzero"`
	CreatedAt       time.Time       `json:"created_at,omitzero"`
}

// NodeID returns the BPMN node ID this task was created for, or "" when the
// task carries no node reference. Callers routinely need the ID rather than the
// whole node, and the relation is a pointer, so this keeps the nil check in one
// place instead of at every call site.
func (t Task) NodeID() string {
	if t.Node == nil {
		return ""
	}
	return t.Node.ID
}

// AssigneeUsername returns the username of the task assignee, or "" when the
// task is unassigned.
func (t Task) AssigneeUsername() string {
	if t.Assignee == nil {
		return ""
	}
	return t.Assignee.Username
}

// OwnerUsername returns the username of whoever delegated the task, or "" when
// nobody did.
func (t Task) OwnerUsername() string {
	if t.Owner == nil {
		return ""
	}
	return t.Owner.Username
}

// AwaitsHandBack reports whether the task is with a delegate who has to hand
// it back to its owner before anybody completes it. It is the one place that
// rule is written: everything that asks "is this a pending delegation" asks
// here.
//
// All three are asked for. A row delegated by a release that kept no owner
// has nobody to go back to, and is its assignee's to complete — as it was.
// And while pods of that release are still running, one of them can claim or
// release a task this release delegated: it writes the status it knows and
// leaves the owner and the pending mark it does not, on a task that is no
// longer delegated. That task is its holder's, not a delegate's.
func (t Task) AwaitsHandBack() bool {
	return t.Status == TaskDelegated && t.DelegationState == DelegationPending && t.OwnerUsername() != ""
}

// HasStaleDelegation reports whether the task carries an owner or a delegation
// state that no longer describes it: it is neither with a delegate (see
// AwaitsHandBack) nor back in its owner's hands. A release that does not know
// the two fields leaves them behind when it claims, releases or hands on a
// task that had them.
func (t Task) HasStaleDelegation() bool {
	if t.Owner == nil && t.DelegationState == "" {
		return false
	}
	if t.AwaitsHandBack() {
		return false
	}
	cameBack := t.Status == TaskClaimed && t.DelegationState == DelegationResolved &&
		t.OwnerUsername() != "" && t.AssigneeUsername() == t.OwnerUsername()
	return !cameBack
}

// FallsToOperators reports whether only an administrator or an operator may
// take the task — claim it, complete it, or give it to somebody — because
// nobody was named for it: it has no assignee, no candidate users and no
// candidate groups.
//
// Nobody being named is not everybody being named: absent constraint means
// deny. It used to mean "anyone", so somebody in accounts payable could pick up
// and complete an approval nobody had meant them to have. Somebody still has to
// be able to take such a task or it waits for ever, and that is the people who
// run the system day to day (TakesUnnamedWork).
//
// Whatever kind of task it is. A manual task was left open to anybody in its
// organization while the designer had no field to name anybody for one; it
// names people the way a user task does now, so one that names nobody is the
// operators' as well.
func (t Task) FallsToOperators() bool {
	return t.AssigneeUsername() == "" && len(t.CandidateUsers) == 0 && len(t.CandidateGroups) == 0
}
