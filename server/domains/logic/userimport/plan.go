package userimport

import (
	"fmt"

	"github.com/gsoultan/metis/server/domains/entities"
)

// Outcome is what importing one row would do to the directory.
type Outcome string

const (
	// OutcomeNew adds somebody the directory does not have.
	OutcomeNew Outcome = "new"
	// OutcomeUnchanged names somebody exactly as the directory already has them.
	OutcomeUnchanged Outcome = "unchanged"
	// OutcomeUpdate changes a name, an address or whether they are active.
	OutcomeUpdate Outcome = "update"
	// OutcomeLinked finds somebody by username who had no external ID, and
	// gives them the row's.
	OutcomeLinked Outcome = "linked"
	// OutcomeRenamed finds somebody by external ID under another username:
	// they were renamed in the application.
	OutcomeRenamed Outcome = "renamed"
	// OutcomeConflict cannot be applied without taking a username or an
	// external ID from somebody else.
	OutcomeConflict Outcome = "conflict"
)

// Planned is one row and what importing it would do.
type Planned struct {
	Row     Row
	Outcome Outcome
	// Person is who the row lands on: the existing participant, or nil for
	// somebody new.
	Person *entities.WorkflowUser
	// Changes say what would change, in words: `email was "a@x"`.
	Changes []string
	// Reason says why a conflict cannot be applied.
	Reason string
}

// Plan decides, for each row, who it is and what importing it would do,
// without writing anything.
//
// The one place the matching rules live, so a check and an import cannot
// disagree about what a row means:
//
//   - A row with an external ID is matched on it first. Found under the same
//     username, it is that person; under another, they were renamed — unless
//     somebody else already has the new username, which is a conflict.
//   - Not found by external ID, it is matched on username. A person there with
//     no external ID is linked to this one; a person there with a different one
//     is somebody else, and the row is a conflict.
//   - A row without an external ID is matched on username alone, and leaves
//     any external ID the person already has as it is.
//
// One person is the target of at most one row. A second row reaching the same
// person by another route is a conflict, not a second write.
func Plan(existing []entities.WorkflowUser, rows []Row) []Planned {
	byExternal := make(map[string]*entities.WorkflowUser, len(existing))
	byUsername := make(map[string]*entities.WorkflowUser, len(existing))
	for i := range existing {
		person := &existing[i]
		byUsername[person.Username] = person
		if person.ExternalID != "" {
			byExternal[person.ExternalID] = person
		}
	}
	claimedBy := map[*entities.WorkflowUser]int{}

	plans := make([]Planned, 0, len(rows))
	for _, row := range rows {
		plan := planRow(row, byExternal, byUsername)
		if plan.Person != nil && plan.Outcome != OutcomeConflict {
			if line, taken := claimedBy[plan.Person]; taken {
				plan = Planned{Row: row, Outcome: OutcomeConflict, Person: plan.Person,
					Reason: fmt.Sprintf("%q is already matched by line %d", plan.Person.Username, line)}
			} else {
				claimedBy[plan.Person] = row.Line
			}
		}
		plans = append(plans, plan)
	}
	return plans
}

func planRow(row Row, byExternal, byUsername map[string]*entities.WorkflowUser) Planned {
	named := byUsername[row.Username]

	if row.ExternalID != "" {
		if person, found := byExternal[row.ExternalID]; found {
			if person.Username == row.Username {
				return compare(row, person, OutcomeUnchanged)
			}
			if named != nil {
				return Planned{Row: row, Outcome: OutcomeConflict, Person: person,
					Reason: fmt.Sprintf("renamed from %q, but %q already belongs to somebody else", person.Username, row.Username)}
			}
			plan := compare(row, person, OutcomeRenamed)
			plan.Outcome = OutcomeRenamed
			plan.Changes = append([]string{fmt.Sprintf("was %q", person.Username)}, plan.Changes...)
			return plan
		}
		if named != nil {
			if named.ExternalID == "" {
				plan := compare(row, named, OutcomeLinked)
				plan.Outcome = OutcomeLinked
				plan.Changes = append([]string{"external ID will be set"}, plan.Changes...)
				return plan
			}
			return Planned{Row: row, Outcome: OutcomeConflict, Person: named,
				Reason: fmt.Sprintf("%q already belongs to external ID %q", row.Username, named.ExternalID)}
		}
		return Planned{Row: row, Outcome: OutcomeNew}
	}

	if named == nil {
		return Planned{Row: row, Outcome: OutcomeNew}
	}
	return compare(row, named, OutcomeUnchanged)
}

// compare fills in what would change for a row that lands on person, and makes
// an unchanged outcome an update when anything does.
func compare(row Row, person *entities.WorkflowUser, outcome Outcome) Planned {
	var changes []string
	if row.DisplayName != person.DisplayName {
		changes = append(changes, fmt.Sprintf("name was %q", person.DisplayName))
	}
	if row.Email != person.Email {
		changes = append(changes, fmt.Sprintf("email was %q", person.Email))
	}
	if row.Active != person.Active {
		if row.Active {
			changes = append(changes, "will be active again")
		} else {
			changes = append(changes, "will be deactivated")
		}
	}
	if outcome == OutcomeUnchanged && len(changes) > 0 {
		outcome = OutcomeUpdate
	}
	return Planned{Row: row, Outcome: outcome, Person: person, Changes: changes}
}
