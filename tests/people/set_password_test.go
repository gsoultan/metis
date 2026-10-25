package people_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/pg"
)

// signIn signs ada in to the world's project and returns her People token.
func (w world) signIn(t *testing.T) string {
	t.Helper()
	code, body := w.do(t, http.MethodPost, "/api/v1/people/login", "", map[string]string{
		"project_id": w.project.String(), "username": "ada", "password": personPassword,
	})
	if code != http.StatusOK {
		t.Fatalf("ada signs in: %d %s", code, body)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("read the sign-in: %v", err)
	}
	return out.Token
}

func (w world) seedPerson(t *testing.T, username, email string) uuid.UUID {
	t.Helper()
	system := entities.WithSystemContext(t.Context())
	if _, err := pg.NewWorkflowUserRepository(w.conn).Upsert(system, entities.WorkflowUser{
		Project: &entities.Project{ID: w.project}, Username: username, Email: email, Active: true,
	}); err != nil {
		t.Fatalf("seed %s: %v", username, err)
	}
	return w.personID(t, username)
}

func (w world) personID(t *testing.T, username string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := w.conn.Main().QueryRow(t.Context(),
		"SELECT id FROM workflow_users WHERE project_id = $1 AND username = $2", w.project, username).Scan(&id); err != nil {
		t.Fatalf("read %s: %v", username, err)
	}
	return id
}

// TestAnAdministratorGivesAPersonTheirPassword: the only way a person gets a
// password for the People login. Setting one ends the sessions they had, and
// it reaches only the person it names in the project it names.
func TestAnAdministratorGivesAPersonTheirPassword(t *testing.T) {
	w := newWorld(t)
	ada := w.personID(t, "ada")
	old := w.signIn(t)
	// A session is compared to the password change by the second, and one
	// sealed in the same second is kept: it is the one about to be handed out.
	time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second)))
	path := "/api/v1/participants/" + ada.String() + "/password?project_id=" + w.project.String()
	const fresh = "a much better passphrase"

	if code, body := w.do(t, http.MethodPut, path, w.userToken, map[string]string{"password": "short"}); code != http.StatusBadRequest {
		t.Errorf("a password too short to replace one = %d %s, want 400", code, body)
	}
	if code, body := w.do(t, http.MethodPut, path, old, map[string]string{"password": fresh}); code != http.StatusUnauthorized {
		t.Errorf("a People token setting a password = %d %s, want 401", code, body)
	}
	other := "/api/v1/participants/" + w.seedPerson(t, "bob", "").String() + "/password?project_id=00000000-0000-0000-0000-000000000001"
	if code, body := w.do(t, http.MethodPut, other, w.userToken, map[string]string{"password": fresh}); code == http.StatusOK {
		t.Errorf("setting a password naming a project the person is not in answered %s", body)
	}

	if code, body := w.do(t, http.MethodPut, path, w.userToken, map[string]string{"password": fresh}); code != http.StatusOK {
		t.Fatalf("set ada's password: %d %s", code, body)
	}
	if code, body := w.do(t, http.MethodGet, "/api/v1/people/me", old, nil); code != http.StatusUnauthorized {
		t.Errorf("ada's session from before the new password = %d %s, want 401", code, body)
	}
	if code, _ := w.do(t, http.MethodPost, "/api/v1/people/login", "", map[string]string{
		"project_id": w.project.String(), "username": "ada", "password": personPassword,
	}); code == http.StatusOK {
		t.Error("ada's old password still signs her in")
	}
	if code, body := w.do(t, http.MethodPost, "/api/v1/people/login", "", map[string]string{
		"project_id": w.project.String(), "username": "ada", "password": fresh,
	}); code != http.StatusOK {
		t.Fatalf("ada signs in with her new password: %d %s", code, body)
	}
}
