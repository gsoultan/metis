package impl

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// TaskLinkPrefix is how every task link token starts.
const TaskLinkPrefix = "mtl_"

// DefaultTaskLinkLifetime is how long a link is good for when the server
// says nothing else.
const DefaultTaskLinkLifetime = 7 * 24 * time.Hour

// maxLinksPerTask bounds the links one task issues. A task offered to a
// group of thousands is a queue for an inbox, not a mailing.
const maxLinksPerTask = 200

// errLinkNotGood is every link that cannot be used, whatever the reason:
// unknown, used, voided, expired, a task that moved on or a person who left.
// One answer, so a link tells whoever holds it nothing about the task or the
// person beyond what it is for.
var errLinkNotGood = fmt.Errorf("%w: this link is no longer good; ask for the task to be sent again or sign in", apierr.ErrNotFound)

type taskLinkService struct {
	tasks    *taskService
	lifetime time.Duration
	now      func() time.Time
}

// TaskLinksOf is the link side of the task service: the same rules a signed-in
// person acts under, reached through a link. lifetime zero or less is
// DefaultTaskLinkLifetime.
func TaskLinksOf(tasks servicecontracts.TaskService, lifetime time.Duration) servicecontracts.TaskLinkService {
	svc, ok := tasks.(*taskService)
	if !ok {
		return NewUnavailableTaskLinkService()
	}
	if lifetime <= 0 {
		lifetime = DefaultTaskLinkLifetime
	}
	return &taskLinkService{tasks: svc, lifetime: lifetime, now: time.Now}
}

// newTaskLinkToken is 32 random bytes, and the hash it is kept under.
func newTaskLinkToken() (token, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("could not make a task link: %w", err)
	}
	token = TaskLinkPrefix + base64.RawURLEncoding.EncodeToString(raw)
	return token, hashTaskLinkToken(token), nil
}

func hashTaskLinkToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// IssueTaskLinks runs in whatever transaction created or moved the task, in a
// savepoint of its own: a link that cannot be recorded must not undo the
// work it was for, and on PostgreSQL a failed statement would otherwise
// poison the transaction around it.
func (s *taskLinkService) IssueTaskLinks(ctx context.Context, taskID uuid.UUID) ([]servicecontracts.IssuedTaskLink, error) {
	repo := s.tasks.repo
	system := entities.WithSystemContext(ctx)
	m, err := repo.Task().Get(system, taskID)
	if err != nil {
		return nil, err
	}
	task := adapters.TaskEntityAdapter{Model: m}.ToEntity()
	// A task that cannot be completed, or is in no project, is owed no links.
	if task.Project == nil || !task.Status.CanTransitionTo(entities.TaskCompleted) {
		return nil, nil
	}
	people, err := s.namedPeople(system, task)
	if err != nil {
		return nil, err
	}
	issued := make([]servicecontracts.IssuedTaskLink, 0, len(people))
	err = repo.UnitOfWork().Attempt(ctx, func(txCtx context.Context) error {
		for _, person := range people {
			token, hash, err := newTaskLinkToken()
			if err != nil {
				return err
			}
			expires := s.now().Add(s.lifetime)
			if err := repo.TaskLink().Create(txCtx, entities.TaskLink{
				ProjectID:     task.Project.ID,
				TaskID:        task.ID,
				ParticipantID: person.ID,
				ExpiresAt:     expires,
			}, hash); err != nil {
				return err
			}
			issued = append(issued, servicecontracts.IssuedTaskLink{
				Person: person, TaskID: task.ID, Token: token, ExpiresAt: expires,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return issued, nil
}

// namedPeople is everybody in the task's project directory the task names,
// who is active and has an address: the assignee when there is one, else the
// candidate users and the members of the candidate groups. A name that is not
// a person — a User, or nobody — is passed over: a link is for People.
func (s *taskLinkService) namedPeople(ctx context.Context, task entities.Task) ([]entities.Person, error) {
	directory := s.tasks.repo.PeopleDirectory()
	project := task.Project.ID
	seen := map[uuid.UUID]struct{}{}
	var people []entities.Person
	add := func(participant entities.WorkflowUser) {
		if _, dup := seen[participant.ID]; dup || !participant.Active || strings.TrimSpace(participant.Email) == "" {
			return
		}
		seen[participant.ID] = struct{}{}
		people = append(people, entities.PersonFrom(participant))
	}
	byName := func(username string) error {
		participant, err := directory.Named(ctx, project, username)
		if errors.Is(err, apierr.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		add(participant)
		return nil
	}

	if assignee := task.AssigneeUsername(); assignee != "" {
		return people, byName(assignee)
	}
	for _, candidate := range task.CandidateUsers {
		if candidate == nil || candidate.Username == "" {
			continue
		}
		if err := byName(candidate.Username); err != nil {
			return nil, err
		}
	}
	for _, group := range task.CandidateGroups {
		if group == nil {
			continue
		}
		key := group.Name
		if key == "" && group.ID != uuid.Nil {
			key = group.ID.String()
		}
		if key == "" {
			continue
		}
		members, err := directory.MembersOf(ctx, project, key)
		if err != nil {
			return nil, err
		}
		for _, member := range members {
			add(member)
		}
	}
	if len(people) > maxLinksPerTask {
		log.Warn().Str("task_id", task.ID.String()).Int("named", len(people)).Int("sent", maxLinksPerTask).
			Msg("A task names more people than links are sent for; the rest find it in their inbox")
		people = people[:maxLinksPerTask]
	}
	return people, nil
}

func (s *taskLinkService) ReadTaskLink(ctx context.Context, token string) (servicecontracts.TaskLinkView, error) {
	link, err := s.tasks.repo.TaskLink().GetByHash(ctx, hashTaskLinkToken(token))
	if err != nil {
		return servicecontracts.TaskLinkView{}, s.notGood(err)
	}
	person, task, err := s.goodFor(ctx, link)
	if err != nil {
		return servicecontracts.TaskLinkView{}, err
	}
	return servicecontracts.TaskLinkView{Task: task, Person: person, ExpiresAt: link.ExpiresAt}, nil
}

// CompleteByTaskLink holds the link's row while it completes, so two submits
// of one link take turns and the second finds it used. The link is used up
// in the same transaction as the completion: a completion the form refuses
// leaves the link good to try again, and one that commits leaves it spent.
func (s *taskLinkService) CompleteByTaskLink(ctx context.Context, token string, vars map[string]any) error {
	repo := s.tasks.repo
	return repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		link, err := repo.TaskLink().GetByHashForUpdate(txCtx, hashTaskLinkToken(token))
		if err != nil {
			return s.notGood(err)
		}
		person, _, err := s.goodFor(txCtx, link)
		if err != nil {
			return err
		}
		now := s.now()
		if err := repo.TaskLink().MarkUsed(txCtx, link.ID, now); err != nil {
			return errLinkNotGood
		}
		if err := s.tasks.CompletePersonTask(txCtx, person, link.TaskID, vars); err != nil {
			return err
		}
		// The task is done, so every other link to it is too.
		if _, err := repo.TaskLink().VoidOpenForTask(txCtx, link.TaskID, now); err != nil {
			return err
		}
		return nil
	})
}

// goodFor answers who a link acts as and the task it shows, or errLinkNotGood:
// the link itself still good, its person still in the directory and active in
// the link's project, and the task still open and still theirs or offered to
// them — asked exactly as a signed-in person's would be.
func (s *taskLinkService) goodFor(ctx context.Context, link entities.TaskLink) (entities.Person, entities.Task, error) {
	if !link.GoodAt(s.now()) {
		return entities.Person{}, entities.Task{}, errLinkNotGood
	}
	participant, err := s.tasks.repo.PeopleDirectory().Participant(ctx, link.ParticipantID)
	if err != nil {
		return entities.Person{}, entities.Task{}, s.notGood(err)
	}
	if !participant.Active || participant.Project == nil || participant.Project.ID != link.ProjectID {
		return entities.Person{}, entities.Task{}, errLinkNotGood
	}
	person := entities.PersonFrom(participant)
	task, err := s.tasks.GetPersonTask(ctx, person, link.TaskID)
	if err != nil {
		return entities.Person{}, entities.Task{}, s.notGood(err)
	}
	if refuseClosedTask(task.Status) != nil {
		return entities.Person{}, entities.Task{}, errLinkNotGood
	}
	return person, task, nil
}

// notGood folds a not-found into errLinkNotGood and lets anything else — the
// database being down — through as itself, so an outage is not reported to
// somebody as their link having expired.
func (s *taskLinkService) notGood(err error) error {
	if errors.Is(err, apierr.ErrNotFound) || errors.Is(err, apierr.ErrForbidden) {
		return errLinkNotGood
	}
	return err
}

// errNoTaskLinks is what a facade built without links answers.
var errNoTaskLinks = errors.New("task links are not available: this server was wired without them")

type unavailableTaskLinks struct{}

// NewUnavailableTaskLinkService refuses every link call.
func NewUnavailableTaskLinkService() servicecontracts.TaskLinkService { return unavailableTaskLinks{} }

func (unavailableTaskLinks) IssueTaskLinks(context.Context, uuid.UUID) ([]servicecontracts.IssuedTaskLink, error) {
	return nil, errNoTaskLinks
}

func (unavailableTaskLinks) ReadTaskLink(context.Context, string) (servicecontracts.TaskLinkView, error) {
	return servicecontracts.TaskLinkView{}, errNoTaskLinks
}

func (unavailableTaskLinks) CompleteByTaskLink(context.Context, string, map[string]any) error {
	return errNoTaskLinks
}
