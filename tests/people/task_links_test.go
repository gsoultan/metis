package people_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

func (w world) seedGroup(t *testing.T, name string, members ...uuid.UUID) {
	t.Helper()
	var groupID uuid.UUID
	if err := w.conn.Main().QueryRow(t.Context(),
		"INSERT INTO workflow_groups (project_id, name) VALUES ($1, $2) RETURNING id", w.project, name).Scan(&groupID); err != nil {
		t.Fatalf("seed group %s: %v", name, err)
	}
	for _, member := range members {
		if _, err := w.conn.Main().Exec(t.Context(),
			"INSERT INTO workflow_group_memberships (workflow_group_id, workflow_user_id) VALUES ($1, $2)", groupID, member); err != nil {
			t.Fatalf("put %s in %s: %v", member, name, err)
		}
	}
}

// links issues the links to a task and returns them by username.
func (w world) links(t *testing.T, task entities.Task) map[string]string {
	t.Helper()
	ctx := entities.WithTenantContext(t.Context(), w.tenant(t))
	issued, err := w.svc.IssueTaskLinks(ctx, task.ID)
	if err != nil {
		t.Fatalf("issue the links to %s: %v", task.Name, err)
	}
	byName := map[string]string{}
	for _, link := range issued {
		if !strings.HasPrefix(link.Token, "mtl_") {
			t.Fatalf("link token %q does not look like one", link.Token)
		}
		byName[link.Person.Username] = link.Token
	}
	return byName
}

func (w world) readLink(t *testing.T, token string) (int, string) {
	t.Helper()
	return w.do(t, http.MethodPost, "/api/v1/people/links/read", "", map[string]string{"token": token})
}

func (w world) completeByLink(t *testing.T, token string) (int, string) {
	t.Helper()
	return w.do(t, http.MethodPost, "/api/v1/people/links/complete", "", map[string]any{
		"token": token, "variables": map[string]any{"approved": true},
	})
}

// TestATaskLinkActsOnceForItsPersonAndThenIsSpent is the link on the tables
// and through the API as the server builds it, with no sign-in anywhere.
func TestATaskLinkActsOnceForItsPersonAndThenIsSpent(t *testing.T) {
	w := newWorld(t)
	named := w.startTask(t, w.project, "named", entities.Node{Assignee: "ada"})
	links := w.links(t, named)
	token, ok := links["ada"]
	if !ok || len(links) != 1 {
		t.Fatalf("links issued to ada's task: %v, want one for ada", links)
	}

	// Opening it, twice, changes nothing: a mail scanner opening every link
	// must not act on any task.
	for range 2 {
		if code, body := w.readLink(t, token); code != http.StatusOK || !strings.Contains(body, named.ID.String()) || !strings.Contains(body, `"username":"ada"`) {
			t.Fatalf("open ada's link: %d %s", code, body)
		}
	}
	if got := w.status(t, named.ID); got == entities.TaskCompleted {
		t.Fatal("opening a link completed the task")
	}
	if code, body := w.do(t, http.MethodGet, "/api/v1/people/links/read", "", nil); code == http.StatusOK {
		t.Errorf("a GET of the link route answered %s; only a POST opens a link", body)
	}

	// A token nobody issued, and one a letter off, are refused alike.
	for _, wrong := range []string{"mtl_" + strings.Repeat("A", 43), token[:len(token)-1] + "x", ""} {
		if code, body := w.completeByLink(t, wrong); code != http.StatusNotFound {
			t.Errorf("complete with token %q = %d %s, want 404", wrong, code, body)
		}
	}

	if code, body := w.completeByLink(t, token); code != http.StatusOK {
		t.Fatalf("complete through ada's link: %d %s", code, body)
	}
	if got := w.status(t, named.ID); got != entities.TaskCompleted {
		t.Fatalf("task after ada's link completed it: %q, want completed", got)
	}
	if code, body := w.completeByLink(t, token); code != http.StatusNotFound {
		t.Fatalf("a used link completing again = %d %s, want 404", code, body)
	}
	if code, body := w.readLink(t, token); code != http.StatusNotFound {
		t.Fatalf("a used link opening again = %d %s, want 404", code, body)
	}
}

func TestOnAPoolTaskTheFirstLinkSubmittedWins(t *testing.T) {
	w := newWorld(t)
	ada := w.personID(t, "ada")
	bob := w.seedPerson(t, "bob", "bob@example.com")
	quiet := w.seedPerson(t, "cy", "") // no address: nowhere to send a link
	w.seedGroup(t, "approvers", ada, bob, quiet)

	pool := w.startTask(t, w.project, "pool", entities.Node{CandidateGroups: []*entities.Group{{Name: "approvers"}}})
	links := w.links(t, pool)
	if len(links) != 2 || links["ada"] == "" || links["bob"] == "" {
		t.Fatalf("links to the approvers' task: %v, want one each for ada and bob and none for cy", links)
	}
	if code, body := w.completeByLink(t, links["bob"]); code != http.StatusOK {
		t.Fatalf("bob completes the pool task through his link: %d %s", code, body)
	}
	if code, body := w.readLink(t, links["ada"]); code != http.StatusNotFound {
		t.Fatalf("ada's link after bob completed the task = %d %s, want 404", code, body)
	}
	if code, body := w.completeByLink(t, links["ada"]); code != http.StatusNotFound {
		t.Fatalf("ada completing through her link after bob = %d %s, want 404", code, body)
	}
}

func TestALinkStopsWorkingWhenItsPersonOrItsTimeIsGone(t *testing.T) {
	w := newWorld(t)
	cases := []struct {
		name  string
		spoil func(t *testing.T, task entities.Task)
	}{
		{"its person is deactivated", func(t *testing.T, _ entities.Task) {
			if _, err := w.conn.Main().Exec(t.Context(), "UPDATE workflow_users SET active = false WHERE id = $1", w.personID(t, "ada")); err != nil {
				t.Fatal(err)
			}
		}},
		{"it has expired", func(t *testing.T, task entities.Task) {
			if _, err := w.conn.Main().Exec(t.Context(), "UPDATE task_links SET expires_at = now() - interval '1 minute' WHERE task_id = $1", task.ID); err != nil {
				t.Fatal(err)
			}
		}},
		{"it was voided", func(t *testing.T, task entities.Task) {
			if _, err := w.conn.Main().Exec(t.Context(), "UPDATE task_links SET voided_at = now() WHERE task_id = $1", task.ID); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := w.conn.Main().Exec(t.Context(), "UPDATE workflow_users SET active = true WHERE project_id = $1", w.project); err != nil {
				t.Fatal(err)
			}
			task := w.startTask(t, w.project, fmt.Sprintf("case-%d", i), entities.Node{Assignee: "ada"})
			token := w.links(t, task)["ada"]
			if code, body := w.readLink(t, token); code != http.StatusOK {
				t.Fatalf("the link before: %d %s", code, body)
			}
			tc.spoil(t, task)
			if code, body := w.completeByLink(t, token); code != http.StatusNotFound {
				t.Fatalf("complete after %s = %d %s, want 404", tc.name, code, body)
			}
			if got := w.status(t, task.ID); got == entities.TaskCompleted {
				t.Fatalf("the task was completed after %s", tc.name)
			}
		})
	}
}

// TestNoLinkIsIssuedForANameThatIsNoPerson: a task given to a User, or to a
// name nobody in the directory has, sends no link to anybody.
func TestNoLinkIsIssuedForANameThatIsNoPerson(t *testing.T) {
	w := newWorld(t)
	for _, name := range []string{"dana", "nobody-of-that-name"} {
		task := w.startTask(t, w.project, "for-"+name, entities.Node{Assignee: name})
		if links := w.links(t, task); len(links) != 0 {
			t.Errorf("links to a task for %s: %v, want none", name, links)
		}
	}
}
