package entities_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// wholeTask is a task with every field set, so its JSON shows every key.
func wholeTask(status entities.TaskStatus, assignee, owner string, state entities.DelegationState) entities.Task {
	due := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	task := entities.Task{
		ID:              uuid.MustParse("019a0000-0000-7000-8000-000000000001"),
		Project:         &entities.Project{ID: uuid.MustParse("019a0000-0000-7000-8000-000000000002")},
		IterationID:     "1",
		Name:            "Approve the refund",
		Description:     "Over the limit",
		Type:            entities.UserTask,
		Status:          status,
		Assignee:        &entities.User{Username: assignee},
		DelegationState: state,
		CandidateUsers:  []*entities.User{{Username: "citra"}},
		Priority:        70,
		DueDate:         &due,
		FormKey:         "refund",
		Variables:       map[string]any{"amount": 9000},
		CreatedAt:       time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC),
	}
	if owner != "" {
		task.Owner = &entities.User{Username: owner}
	}
	return task
}

func encoded(t *testing.T, value any) string {
	t.Helper()
	out, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return string(out)
}

// The pin for the REST shape of a task: the JSON of entities.Task is what GET
// /api/v1/tasks sends. A delegation that is live or finished is sent as it
// always was, key for key, and so is a task that was never delegated.
func TestATasksJSONIsWhatItWas(t *testing.T) {
	const head = `{"id":"019a0000-0000-7000-8000-000000000001","project":{"id":"019a0000-0000-7000-8000-000000000002","name":""},` +
		`"iteration_id":"1","name":"Approve the refund","description":"Over the limit","type":"userTask",`
	user := func(name string) string {
		return `{"id":"00000000-0000-0000-0000-000000000000","username":"` + name + `","full_name":"","display_name":"","email":"","roles":null}`
	}
	tail := `"candidate_users":[` + user("citra") + `],"priority":70,` +
		`"due_date":"2026-10-09T08:00:00Z","form_key":"refund","variables":{"amount":9000},"created_at":"2026-10-02T08:00:00Z"}`
	for _, c := range []struct {
		name string
		task entities.Task
		want string
	}{
		{"with its delegate", wholeTask(entities.TaskDelegated, "citra", "budi", entities.DelegationPending),
			head + `"status":"delegated","assignee":` + user("citra") + `,"owner":` + user("budi") + `,"delegation_state":"pending",` + tail},
		{"back with its owner", wholeTask(entities.TaskClaimed, "budi", "budi", entities.DelegationResolved),
			head + `"status":"claimed","assignee":` + user("budi") + `,"owner":` + user("budi") + `,"delegation_state":"resolved",` + tail},
		{"never delegated", wholeTask(entities.TaskClaimed, "budi", "", ""),
			head + `"status":"claimed","assignee":` + user("budi") + `,` + tail},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := encoded(t, c.task); got != c.want {
				t.Fatalf("sent\n%s\nwant\n%s", got, c.want)
			}
			// However it is reached: by pointer, in a list, inside a reply.
			inReply := encoded(t, struct {
				Task  *entities.Task  `json:"task"`
				Tasks []entities.Task `json:"tasks"`
			}{&c.task, []entities.Task{c.task}})
			if want := `{"task":` + c.want + `,"tasks":[` + c.want + `]}`; inReply != want {
				t.Fatalf("inside a reply it is sent\n%s\nwant\n%s", inReply, want)
			}
		})
	}
}

// A pod on the release before this one claims, releases or completes a task
// this release delegated, and the engine withdraws one by writing its status
// alone: the row keeps its owner and its pending mark under a status that is
// not "delegated". Sent as it stood, it told a client the task was waiting to
// be handed back, and left the client to work out that it was not.
func TestAPendingMarkOnATaskThatIsNotWaitingToBeHandedBackIsNotSent(t *testing.T) {
	for _, c := range []struct {
		name string
		task entities.Task
	}{
		{"claimed by an old pod", wholeTask(entities.TaskClaimed, "citra", "budi", entities.DelegationPending)},
		{"withdrawn while with its delegate", wholeTask(entities.TaskCanceled, "citra", "budi", entities.DelegationPending)},
		{"pending with nobody to go back to", wholeTask(entities.TaskDelegated, "citra", "", entities.DelegationPending)},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := encoded(t, c.task); strings.Contains(got, `"owner"`) || strings.Contains(got, `"delegation_state"`) {
				t.Fatalf("a task that is not waiting to be handed back was sent with its leftover delegation:\n%s", got)
			}
			if want := encoded(t, wholeTask(c.task.Status, "citra", "", "")); encoded(t, c.task) != want {
				t.Fatalf("sent\n%s\nwant it as a task never delegated is sent\n%s", encoded(t, c.task), want)
			}
			// Encoding it does not change the task it was read from.
			if c.task.DelegationState != entities.DelegationPending {
				t.Fatalf("encoding the task changed it: %+v", c.task)
			}
		})
	}
}
