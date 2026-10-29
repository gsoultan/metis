package postgres_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/repositories/store/processdefinition"
	"github.com/gsoultan/metis/server/repositories/store/processinstance"
	projectstore "github.com/gsoultan/metis/server/repositories/store/project"
	"github.com/gsoultan/storm/runtime"

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
	ex := testutils.Executor(db)
	home, found, err := projectstore.New().IDEq(project).One(ctx, ex)
	if err != nil || !found {
		t.Fatalf("read the organization: %v (found=%v)", err, found)
	}
	sib := projectstore.Create()
	sib.SetID(sibling)
	sib.SetOrganizationID(home.OrganizationID)
	sib.SetName("Sibling")
	if _, err := sib.Insert(ctx, ex); err != nil {
		t.Fatalf("seed the sibling project: %v", err)
	}

	def := func(project uuid.UUID, key, name string, version int) uuid.UUID {
		t.Helper()
		id := testutils.SeedDefinitionVersion(ctx, t, db, project, key, version)
		rename := processdefinition.MutateKey(id)
		rename.SetName(name)
		if err := rename.Update(ctx, ex); err != nil {
			t.Fatalf("name %s v%d: %v", key, version, err)
		}
		return id
	}
	inst := func(project, def uuid.UUID, status, tokens string, gone bool) {
		t.Helper()
		now := time.Now()
		row := processinstance.Row{ID: uuid.Must(uuid.NewV7()), CreatedAt: now, UpdatedAt: now, ProjectID: project,
			DefinitionID: def, Status: status, Tokens: runtime.JSON(tokens), CompletedNodes: runtime.JSON("[]"),
			CompensatedNodes: runtime.JSON("[]"), MultiInstance: runtime.JSON("{}"), Joins: runtime.JSON("{}")}
		if gone {
			row.DeletedAt = runtime.Null[time.Time]{V: now, Valid: true}
		}
		if _, err := processinstance.InsertAll(ctx, ex, []processinstance.Row{row}); err != nil {
			t.Fatalf("seed an instance: %v", err)
		}
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
