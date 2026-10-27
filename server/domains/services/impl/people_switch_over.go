package impl

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

const (
	gapPerson = "person"
	gapGroup  = "group"
)

type peopleSwitchOverService struct {
	repo         repositories.Repository
	participants repocontracts.WorkflowUserRepository
}

// PeopleSwitchOverOf builds the switch-over report on the platform's
// repositories and the People directory the participants service writes.
func PeopleSwitchOverOf(repo repositories.Repository, participants servicecontracts.WorkflowUserService) servicecontracts.PeopleSwitchOverService {
	concrete, ok := participants.(*workflowUserService)
	if !ok {
		return unavailablePeopleSwitchOver{}
	}
	return &peopleSwitchOverService{repo: repo, participants: concrete.participants}
}

// directory is a project's People and People groups, by name.
type directory struct {
	people map[string]entities.WorkflowUser
	groups map[string]entities.WorkflowGroup
}

// read reads the directory first: it asks the tenant scope about the project,
// so a project outside the caller's organization goes no further.
func (s *peopleSwitchOverService) read(ctx context.Context, projectID uuid.UUID) (directory, error) {
	if projectID == uuid.Nil {
		return directory{}, apierr.Invalidf("a project is required")
	}
	people, err := s.participants.ListByProject(ctx, projectID, 0)
	if err != nil {
		return directory{}, err
	}
	groups, err := s.participants.ListGroups(ctx, projectID)
	if err != nil {
		return directory{}, err
	}
	d := directory{people: make(map[string]entities.WorkflowUser, len(people)), groups: make(map[string]entities.WorkflowGroup, len(groups))}
	for _, p := range people {
		d.people[p.Username] = p
	}
	for _, g := range groups {
		d.groups[g.Name] = g
	}
	return d, nil
}

func (s *peopleSwitchOverService) DirectoryGaps(ctx context.Context, projectID uuid.UUID) ([]entities.DirectoryGap, error) {
	dir, err := s.read(ctx, projectID)
	if err != nil {
		return nil, err
	}
	gaps := map[string]*entities.DirectoryGap{}
	note := func(kind, name string) *entities.DirectoryGap {
		name = strings.TrimSpace(name)
		if name == "" || isAssignmentExpression(name) {
			return nil
		}
		if kind == gapPerson {
			if _, ok := dir.people[name]; ok {
				return nil
			}
		} else if _, ok := dir.groups[name]; ok {
			return nil
		}
		key := kind + "\x00" + name
		if gaps[key] == nil {
			gaps[key] = &entities.DirectoryGap{Name: name, Kind: kind}
		}
		return gaps[key]
	}

	tasks, err := s.repo.Task().ListOpenByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("read the project's open tasks: %w", err)
	}
	for _, m := range tasks {
		task := adapters.TaskEntityAdapter{Model: m}.ToEntity()
		seen := map[*entities.DirectoryGap]bool{}
		count := func(g *entities.DirectoryGap) {
			if g != nil && !seen[g] {
				seen[g] = true
				g.OpenTasks++
			}
		}
		if task.Assignee != nil {
			count(note(gapPerson, task.Assignee.Username))
		}
		for _, u := range task.CandidateUsers {
			if u != nil {
				count(note(gapPerson, u.Username))
			}
		}
		for _, g := range task.CandidateGroups {
			if g != nil {
				count(note(gapGroup, g.Name))
			}
		}
	}

	keys, err := s.repo.Definition().ListKeysByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("read the project's models: %w", err)
	}
	for _, key := range keys {
		m, err := s.repo.Definition().GetLiveByProjectKey(ctx, projectID, key)
		if err != nil {
			// A key with no live version — every one withdrawn — names nobody
			// who will be given work.
			continue
		}
		def := adapters.DefinitionEntityAdapter{Model: m}.ToEntity()
		seen := map[*entities.DirectoryGap]bool{}
		mention := func(g *entities.DirectoryGap) {
			if g != nil && !seen[g] {
				seen[g] = true
				g.Models = append(g.Models, key)
			}
		}
		for _, node := range def.Nodes {
			if node == nil {
				continue
			}
			mention(note(gapPerson, node.Assignee))
			for _, u := range node.CandidateUsers {
				if u != nil {
					mention(note(gapPerson, u.Username))
				}
			}
			for _, g := range node.CandidateGroups {
				if g != nil {
					mention(note(gapGroup, g.Name))
				}
			}
		}
	}

	if err := s.describe(ctx, projectID, gaps); err != nil {
		return nil, err
	}
	out := make([]entities.DirectoryGap, 0, len(gaps))
	for _, g := range gaps {
		out = append(out, *g)
	}
	slices.SortFunc(out, func(a, b entities.DirectoryGap) int {
		if a.Kind != b.Kind {
			return strings.Compare(a.Kind, b.Kind)
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out, nil
}

// describe says, for each gap, what it can be filled from: a User account of
// that name in the project's organization, or the organization's group of
// that name. Never another organization's: a username is global, and the
// report must not read out somebody else's account.
func (s *peopleSwitchOverService) describe(ctx context.Context, projectID uuid.UUID, gaps map[string]*entities.DirectoryGap) error {
	if len(gaps) == 0 {
		return nil
	}
	project, err := s.repo.Project().Get(ctx, projectID)
	if err != nil {
		return err
	}
	org := uuid.UUID(project.OrganizationID)
	accounts, err := s.repo.User().ListByOrganization(ctx, org)
	if err != nil {
		return fmt.Errorf("read the organization's Users: %w", err)
	}
	byName := make(map[string]int, len(accounts))
	for i, a := range accounts {
		byName[a.Username] = i
	}
	platformGroups, err := s.repo.Group().List(ctx, org)
	if err != nil {
		return fmt.Errorf("read the organization's groups: %w", err)
	}
	for _, gap := range gaps {
		switch gap.Kind {
		case gapPerson:
			if i, ok := byName[gap.Name]; ok {
				gap.HasAccount = true
				gap.DisplayName = firstNonEmpty(accounts[i].DisplayName, accounts[i].FullName)
				gap.Email = accounts[i].Email
			}
		case gapGroup:
			for _, g := range platformGroups {
				if g.Name != gap.Name {
					continue
				}
				members, err := s.repo.Group().ListGroupMembers(ctx, uuid.UUID(g.ID))
				if err != nil {
					return fmt.Errorf("read the members of %s: %w", g.Name, err)
				}
				gap.HasPlatformGroup = true
				for _, m := range members {
					gap.Members = append(gap.Members, m.Username)
				}
				slices.Sort(gap.Members)
			}
		}
	}
	return nil
}

func (s *peopleSwitchOverService) FillDirectoryGaps(ctx context.Context, projectID uuid.UUID, people, groups []string) (entities.DirectoryGapImport, error) {
	var result entities.DirectoryGapImport
	gaps, err := s.DirectoryGaps(ctx, projectID)
	if err != nil {
		return result, err
	}
	byKey := make(map[string]entities.DirectoryGap, len(gaps))
	for _, g := range gaps {
		byKey[g.Kind+"\x00"+g.Name] = g
	}
	// Accounts by name, for the members of a group as much as for a person.
	project, err := s.repo.Project().Get(ctx, projectID)
	if err != nil {
		return result, err
	}
	accounts, err := s.repo.User().ListByOrganization(ctx, uuid.UUID(project.OrganizationID))
	if err != nil {
		return result, fmt.Errorf("read the organization's Users: %w", err)
	}
	account := make(map[string]entities.WorkflowUser, len(accounts))
	for _, a := range accounts {
		account[a.Username] = entities.WorkflowUser{
			Project:     &entities.Project{ID: projectID},
			Username:    a.Username,
			DisplayName: firstNonEmpty(a.DisplayName, a.FullName),
			Email:       a.Email,
			Active:      true,
		}
	}
	skip := func(name, reason string) {
		result.Skipped = append(result.Skipped, entities.ImportProblem{Username: name, Reason: reason})
	}
	// ensure adds a person from their account unless they are already there,
	// and returns their directory entry.
	ensure := func(name string) (entities.WorkflowUser, bool, error) {
		if existing, err := s.participants.GetByUsername(ctx, projectID, name); err == nil {
			return existing, false, nil
		}
		from, ok := account[name]
		if !ok {
			return entities.WorkflowUser{}, false, nil
		}
		if _, err := s.participants.Upsert(ctx, from); err != nil {
			return entities.WorkflowUser{}, false, err
		}
		added, err := s.participants.GetByUsername(ctx, projectID, name)
		return added, err == nil, err
	}

	for _, name := range people {
		gap, ok := byKey[gapPerson+"\x00"+name]
		switch {
		case !ok:
			skip(name, "not a name this project's work uses that its People lack")
			continue
		case !gap.HasAccount:
			skip(name, "no User of this name in the organization to add them from; import them instead")
			continue
		}
		_, added, err := ensure(name)
		if err != nil {
			return result, err
		}
		if added {
			result.People++
		}
	}
	for _, name := range groups {
		gap, ok := byKey[gapGroup+"\x00"+name]
		switch {
		case !ok:
			skip(name, "not a group this project's work uses that its People lack")
			continue
		case !gap.HasPlatformGroup:
			skip(name, "no group of this name in the organization to make it from; import it instead")
			continue
		}
		group, created, err := s.participants.EnsureGroup(ctx, projectID, name)
		if err != nil {
			return result, err
		}
		if created {
			result.Groups++
		}
		for _, member := range gap.Members {
			person, added, err := ensure(member)
			if err != nil {
				return result, err
			}
			if person.ID == uuid.Nil {
				skip(member, "a member of "+name+" with no account in the organization")
				continue
			}
			if added {
				result.People++
			}
			if err := s.participants.AddToGroup(ctx, person.ID, group.ID); err != nil {
				return result, err
			}
		}
	}
	return result, nil
}

// isAssignmentExpression reports whether a name is worked out when a task is
// made, such as ${initiator}, rather than being somebody.
func isAssignmentExpression(name string) bool {
	return strings.Contains(name, "${") || strings.Contains(name, "#{")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

type unavailablePeopleSwitchOver struct{}

// NewUnavailablePeopleSwitchOverService answers every call with the reason the
// directory is unavailable.
func NewUnavailablePeopleSwitchOverService() servicecontracts.PeopleSwitchOverService {
	return unavailablePeopleSwitchOver{}
}

func (unavailablePeopleSwitchOver) DirectoryGaps(context.Context, uuid.UUID) ([]entities.DirectoryGap, error) {
	return nil, apierr.Invalidf("%s", unavailableReason)
}

func (unavailablePeopleSwitchOver) FillDirectoryGaps(context.Context, uuid.UUID, []string, []string) (entities.DirectoryGapImport, error) {
	return entities.DirectoryGapImport{}, apierr.Invalidf("%s", unavailableReason)
}
