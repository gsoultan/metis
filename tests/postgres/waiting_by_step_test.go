package postgres_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/tests/testutils"
)

// Where running work is sitting: the live tokens of the active and suspended
// instances per process key and step, and per key how many instances hold
// them, named by the key's newest version; a token is live until it completes,
// one with no node is counted on no step, and an instance whose tokens are not
// an array holds none. Removed instances and other organizations' work are not
// counted.
func TestWaitingWorkIsCountedPerKeyAndStep(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	ctx, _, project := testutils.ScopedProject(t, repo)
	_, _, elsewhere := testutils.ScopedProject(t, repo)
	sibling := uuid.Must(uuid.NewV7())
	exec := func(sql string, args ...any) {
		t.Helper()
		if err := db.Exec(sql, args...).Error; err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	org := ""
	if err := db.Raw(`SELECT organization_id::text FROM projects WHERE id = ?`, project).Scan(&org).Error; err != nil {
		t.Fatalf("read the organization: %v", err)
	}
	exec(`INSERT INTO projects (id, created_at, updated_at, organization_id, name) VALUES (?, now(), now(), ?, 'Sibling')`, sibling, org)

	def := func(project uuid.UUID, key, name string, version int) uuid.UUID {
		t.Helper()
		id := uuid.Must(uuid.NewV7())
		exec(`INSERT INTO process_definitions (id, created_at, updated_at, project_id, key, name, version, nodes, flows)
		      VALUES (?, now(), now(), ?, ?, ?, ?, '[]', '[]')`, id, project, key, name, version)
		return id
	}
	inst := func(project, def uuid.UUID, status, tokens string, gone bool) {
		t.Helper()
		deleted := "NULL"
		if gone {
			deleted = "now()"
		}
		exec(`INSERT INTO process_instances (id, created_at, updated_at, project_id, definition_id, status, variables,
		                                     tokens, completed_nodes, compensated_nodes, multi_instance, joins, deleted_at)
		      VALUES (gen_random_uuid(), now(), now(), ?, ?, ?, '', ?, '[]', '[]', '{}', '{}', `+deleted+`)`,
			project, def, status, tokens)
	}
	k1v1, k1v2 := def(project, "k1", "K one", 1), def(project, "k1", "K two", 2)
	k2 := def(project, "k2", "Two", 1)
	inst(project, k1v1, "active", `[{"node_id":"a","status":"waiting"},{"node_id":"b","status":"completed"}]`, false)
	inst(project, k1v2, "active", `[{"node_id":"a"},{"node_id":"a","status":"waiting"},{"status":"waiting"}]`, false)
	inst(project, k2, "suspended", `{"node_id":"c"}`, false)
	inst(project, k2, "completed", `[{"node_id":"x"}]`, false)
	inst(project, k2, "active", `[{"node_id":"y"}]`, true)
	inst(project, k2, "active", `[{"node_id":"z","status":null}]`, false)
	inst(project, k2, "active", `null`, false)
	inst(sibling, def(sibling, "k3", "Three", 1), "active", `[{"node_id":"s"}]`, false)
	inst(elsewhere, def(elsewhere, "k1", "Theirs", 7), "active", `[{"node_id":"a"}]`, false)

	describe := func(rows []contracts.WaitingRow) string {
		var out []string
		for _, r := range rows {
			out = append(out, fmt.Sprintf("%s/%s:%d/%d %s", r.ProcessKey, r.NodeID, r.Waiting, r.Instances, r.ProcessName))
		}
		sort.Strings(out)
		return strings.Join(out, " ")
	}
	for name, c := range map[string]struct {
		project uuid.UUID
		want    string
	}{
		"one project": {project, "k1/:1/1 K two k1/:4/2 K two k1/a:3/2 K two k2/:1/1 Two k2/z:1/1 Two"},
		"every project the caller may see": {uuid.Nil,
			"k1/:1/1 K two k1/:4/2 K two k1/a:3/2 K two k2/:1/1 Two k2/z:1/1 Two k3/:1/1 Three k3/s:1/1 Three"},
	} {
		rows, err := repo.Process().WaitingByStep(ctx, c.project)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := describe(rows); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", name, got, c.want)
		}
	}
}
