package user_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// GET /api/v1/roles answers "what does each role let somebody do" from the
// gates themselves, through the whole production chain. Anybody signed in may
// ask — Dana is a designer and not an administrator — and nobody signed out.

type legendReply struct {
	Roles []struct {
		Role    string `json:"role"`
		Actions []struct {
			Method string `json:"method"`
			Area   string `json:"area"`
			Label  string `json:"label"`
		} `json:"actions"`
	} `json:"roles"`
}

func (r legendReply) methodsOf(role string) ([]string, bool) {
	for _, entry := range r.Roles {
		if entry.Role != role {
			continue
		}
		methods := []string{}
		for _, action := range entry.Actions {
			methods = append(methods, action.Method)
		}
		return methods, true
	}
	return nil, false
}

func TestAnybodySignedInCanReadWhatEachRoleIsRequiredFor(t *testing.T) {
	w := newProfileWorld(t)

	status, body := w.do(t, http.MethodGet, "/api/v1/roles", w.token, nil)
	if status != http.StatusOK {
		t.Fatalf("a designer asking what each role is for: got %d (%s), want 200", status, body)
	}
	var reply legendReply
	if err := json.Unmarshal([]byte(body), &reply); err != nil {
		t.Fatalf("decode the legend: %v (%s)", err, body)
	}

	var roles []string
	for _, entry := range reply.Roles {
		roles = append(roles, entry.Role)
	}
	want := []string{entities.RoleAdmin, entities.RoleDesigner, entities.RoleOperator, entities.RoleQueryAuthor}
	if !slices.Equal(roles, want) {
		t.Fatalf("the legend lists %v, want %v", roles, want)
	}

	admin, _ := reply.methodsOf(entities.RoleAdmin)
	designer, _ := reply.methodsOf(entities.RoleDesigner)
	operator, _ := reply.methodsOf(entities.RoleOperator)
	if !slices.Contains(admin, "UpdateUser") || slices.Contains(designer, "UpdateUser") {
		t.Errorf("changing an account is an administrator's: admin %v, designer %v", admin, designer)
	}
	if !slices.Contains(designer, "CreateDefinition") {
		t.Errorf("a designer is not listed as creating definitions: %v", designer)
	}
	// Operators resolve incidents. Migrating running instances is an
	// administrator's alone, whatever a sentence about the role once said.
	if !slices.Contains(operator, "ResolveIncident") || slices.Contains(operator, "MigrateInstances") {
		t.Errorf("the operator's list does not match its gates: %v", operator)
	}
	if queryAuthor, listed := reply.methodsOf(entities.RoleQueryAuthor); !listed || len(queryAuthor) != 0 {
		t.Errorf("the query author is checked where a process is deployed, not by a gate: listed %v, %v",
			listed, queryAuthor)
	}
	for _, entry := range reply.Roles {
		for _, action := range entry.Actions {
			if action.Method == "UpdateUser" && (action.Label != "Update user" || action.Area != "accounts") {
				t.Errorf("UpdateUser reads %q in %q, want \"Update user\" in \"accounts\"", action.Label, action.Area)
			}
		}
	}

	status, body = w.do(t, http.MethodGet, "/api/v1/roles", "", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("nobody signed in: got %d (%s), want 401", status, body)
	}
}
