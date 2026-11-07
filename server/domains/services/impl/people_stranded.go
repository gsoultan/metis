package impl

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
)

// reach is who in a project can still act on a task: the organization's
// accounts, the project's people and whether each is active, and which groups
// have somebody in them who can.
//
// Read once per report, then asked per task, so a project with a thousand open
// tasks costs one read of each directory plus one per distinct group name.
type reach struct {
	accounts map[string]bool
	people   map[string]entities.WorkflowUser
	groups   map[string]groupReach
}

// groupReach is what one name a task offers work to resolves to.
type groupReach struct {
	// exists is whether the organization or the project has a group of that
	// name (or id) at all.
	exists bool
	// canAct is whether at least one of its members can act: an account, or
	// an active person.
	canAct bool
}

const (
	whyGone     = "gone"
	whyInactive = "inactive"
	whyEmpty    = "empty"
)

// StrandedTasks lists the project's open tasks that nobody can act on.
func (s *peopleSwitchOverService) StrandedTasks(ctx context.Context, projectID uuid.UUID) ([]entities.StrandedTask, error) {
	// The directory is read first: it asks the tenant scope about the project,
	// so a project outside the caller's organization goes no further.
	dir, err := s.read(ctx, projectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.Task().ListOpenByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("read the project's open tasks: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	tasks := make([]entities.Task, 0, len(rows))
	for _, m := range rows {
		tasks = append(tasks, adapters.TaskEntityAdapter{Model: m}.ToEntity())
	}
	r, err := s.reachFor(ctx, projectID, dir, tasks)
	if err != nil {
		return nil, err
	}
	var out []entities.StrandedTask
	for _, t := range tasks {
		if stranded, ok := r.strand(t); ok {
			out = append(out, stranded)
		}
	}
	// Longest waiting first: the one that has been stuck longest is the one
	// somebody is most likely already chasing.
	slices.SortStableFunc(out, func(a, b entities.StrandedTask) int { return a.Since.Compare(b.Since) })
	return out, nil
}

// reachFor reads who can act, for the names the tasks use.
func (s *peopleSwitchOverService) reachFor(ctx context.Context, projectID uuid.UUID, dir directory, tasks []entities.Task) (reach, error) {
	project, err := s.repo.Project().Get(ctx, projectID)
	if err != nil {
		return reach{}, err
	}
	org := uuid.UUID(project.OrganizationID)
	// Only the organization's accounts: a username is global, and an account
	// elsewhere is nobody here.
	accounts, err := s.repo.User().ListByOrganization(ctx, org)
	if err != nil {
		return reach{}, fmt.Errorf("read the organization's Users: %w", err)
	}
	r := reach{accounts: make(map[string]bool, len(accounts)), people: dir.people, groups: map[string]groupReach{}}
	for _, a := range accounts {
		r.accounts[a.Username] = true
	}

	names := map[string]struct{}{}
	for _, t := range tasks {
		for _, g := range t.CandidateGroups {
			if key := groupKey(g); key != "" {
				names[key] = struct{}{}
			}
		}
	}
	if len(names) == 0 {
		return r, nil
	}
	platform, err := s.repo.Group().List(ctx, org)
	if err != nil {
		return reach{}, fmt.Errorf("read the organization's groups: %w", err)
	}
	for name := range names {
		var g groupReach
		for _, pg := range platform {
			if pg.Name != name && uuid.UUID(pg.ID).String() != name {
				continue
			}
			g.exists = true
			members, err := s.repo.Group().ListGroupMembers(ctx, uuid.UUID(pg.ID))
			if err != nil {
				return reach{}, fmt.Errorf("read the members of %s: %w", pg.Name, err)
			}
			if len(members) > 0 {
				g.canAct = true
			}
		}
		if !g.canAct {
			members, err := s.repo.PeopleDirectory().MembersOf(ctx, projectID, name)
			if err != nil {
				return reach{}, fmt.Errorf("read the members of %s: %w", name, err)
			}
			if _, ok := dir.groups[name]; ok || len(members) > 0 {
				g.exists = true
			}
			for _, m := range members {
				if m.Active {
					g.canAct = true
					break
				}
			}
		}
		r.groups[name] = g
	}
	return r, nil
}

// groupKey is the name a task offers work to, as authorization matches it:
// the group's name, or its id when it names none.
func groupKey(g *entities.Group) string {
	if g == nil {
		return ""
	}
	if name := strings.TrimSpace(g.Name); name != "" {
		return name
	}
	if g.ID != uuid.Nil {
		return g.ID.String()
	}
	return ""
}

// person says why a name a task uses for somebody cannot act, or "" when it
// can: an account of the organization, or an active person of the project.
func (r reach) person(name string) string {
	if r.accounts[name] {
		return ""
	}
	p, ok := r.people[name]
	switch {
	case !ok:
		return whyGone
	case !p.Active:
		return whyInactive
	}
	return ""
}

// strand reports whether nobody can act on t, and why.
//
// A name that is an expression is left alone: who it names was settled when
// the task was made, and a task still carrying one is not this report's to
// judge. A task that names nobody falls to administrators and operators, who
// can always act, so it is never stranded either.
func (r reach) strand(t entities.Task) (entities.StrandedTask, bool) {
	out := entities.StrandedTask{TaskID: t.ID, Name: t.Name, NodeID: t.NodeID(), Since: t.CreatedAt}
	if t.Instance != nil {
		out.InstanceID = t.Instance.ID
	}
	if holder := t.AssigneeUsername(); holder != "" {
		if isAssignmentExpression(holder) {
			return out, false
		}
		why := r.person(holder)
		if why == "" {
			return out, false
		}
		out.Assignee = holder
		out.Kind = entities.StrandedAssigneeGone
		if why == whyInactive {
			out.Kind = entities.StrandedAssigneeInactive
		}
		out.Missing = []entities.StrandedName{{Name: holder, Kind: gapPerson, Why: why}}
		return out, true
	}
	if t.FallsToOperators() {
		return out, false
	}
	for _, u := range t.CandidateUsers {
		if u == nil {
			continue
		}
		if isAssignmentExpression(u.Username) {
			return out, false
		}
		why := r.person(u.Username)
		if why == "" {
			return out, false
		}
		out.Missing = append(out.Missing, entities.StrandedName{Name: u.Username, Kind: gapPerson, Why: why})
	}
	for _, g := range t.CandidateGroups {
		key := groupKey(g)
		if key == "" {
			continue
		}
		if isAssignmentExpression(key) {
			return out, false
		}
		reached := r.groups[key]
		if reached.canAct {
			return out, false
		}
		why := whyGone
		if reached.exists {
			why = whyEmpty
		}
		out.Missing = append(out.Missing, entities.StrandedName{Name: key, Kind: gapGroup, Why: why})
	}
	if len(out.Missing) == 0 {
		return out, false
	}
	out.Kind = entities.StrandedNoCandidate
	return out, true
}
