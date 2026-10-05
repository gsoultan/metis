package deviation_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// A deviation written apart from the change it records can survive a change
// that rolled back, or be lost from one that committed. So the repository
// refuses a write that is not inside a transaction, and the first caller who
// forgets the unit of work fails in its first test.
func TestADeviationIsRecordedOnlyInTheTransactionThatMakesIt(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})

	if _, err := h.repo.Deviation().Create(h.tenantContext(), h.sample(instanceID)); !errors.Is(err, repocontracts.ErrDeviationOutsideTransaction) {
		t.Fatalf("a write outside a transaction: got %v, want ErrDeviationOutsideTransaction", err)
	}
	if rows, err := h.repo.Deviation().ListByInstance(h.tenantContext(), instanceID); err != nil || len(rows) != 0 {
		t.Fatalf("after a refused write the ledger holds %d row(s) (err %v), want none", len(rows), err)
	}

	written, err := h.write(h.tenantContext(), h.sample(instanceID))
	if err != nil {
		t.Fatalf("a write inside a transaction: %v", err)
	}
	rows, err := h.repo.Deviation().ListByInstance(h.tenantContext(), instanceID)
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != written.ID {
		t.Fatalf("the ledger holds %d row(s), want the one written", len(rows))
	}
	got := rows[0]
	if got.Kind != entities.DeviationReassign || got.Scope != entities.DeviationScopeTask ||
		got.Origin != entities.DeviationOriginTask || got.Status != entities.DeviationApplied ||
		got.Actor != "boss" || got.Reason != "alice is on leave" || got.Node == nil || got.Node.Name != "Approve" ||
		got.Project == nil || got.Project.ID != h.projID || got.Instance == nil || got.Instance.ID != instanceID {
		t.Errorf("the row read back is %+v", got)
	}
	if got.After["tasks"].(map[string]any)["t1"].(map[string]any)["assignee"] != "bob" {
		t.Errorf("after reads back as %v", got.After)
	}

	// A transaction that rolls back takes its row with it.
	rollback := errors.New("the change failed")
	err = h.repo.UnitOfWork().Do(h.tenantContext(), func(tx context.Context) error {
		if _, err := h.repo.Deviation().Create(tx, h.sample(instanceID)); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("the rolled-back unit of work answered %v", err)
	}
	if rows, _ := h.repo.Deviation().ListByInstance(h.tenantContext(), instanceID); len(rows) != 1 {
		t.Fatalf("a rolled-back change left its row: the ledger holds %d", len(rows))
	}
}

// before and after carry business variables, which are encrypted at rest on
// the instance and on every copy the engine keeps (pg/sealed.go). A ledger row
// that stored them in clear would be a sixth copy anybody with read access to
// the table could open.
func TestARecordedDeviationKeepsItsBusinessDataSealed(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	d := h.sample(instanceID)
	d.Before = map[string]any{"variables": map[string]any{"amount": "seventy-thousand-four-hundred"}}
	d.After = map[string]any{"variables": map[string]any{"amount": "eighty-thousand"}}
	d.Details = map[string]any{"override": "not_a_candidate"}
	written, err := h.write(h.tenantContext(), d)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	var before, after, details string
	if err := h.db.Raw(`SELECT before::text, after::text, details::text FROM instance_deviations WHERE id = ?`, written.ID).
		Row().Scan(&before, &after, &details); err != nil {
		t.Fatalf("read the stored row: %v", err)
	}
	for name, stored := range map[string]string{"before": before, "after": after} {
		if strings.Contains(stored, "thousand") {
			t.Errorf("%s is stored in clear: %s", name, stored)
		}
	}
	if !strings.Contains(details, "not_a_candidate") {
		t.Errorf("details is not business data and is meant to stay readable in SQL: %s", details)
	}
	rows, _ := h.repo.Deviation().ListByInstance(h.tenantContext(), instanceID)
	if len(rows) != 1 || rows[0].Before["variables"].(map[string]any)["amount"] != "seventy-thousand-four-hundred" {
		t.Fatalf("the sealed row does not read back: %+v", rows)
	}
}

// An instance's deviations are its organization's record. A caller from
// another organization can neither plant a row against it nor read its rows.
func TestAnotherOrganizationNeitherWritesNorReadsAnInstancesDeviations(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	if _, err := h.write(h.tenantContext(), h.sample(instanceID)); err != nil {
		t.Fatalf("write: %v", err)
	}
	other, err := h.svc.CreateOrganization(context.Background(), "Other Org", "")
	if err != nil {
		t.Fatalf("create the other organization: %v", err)
	}
	elsewhere := entities.WithTenantContext(context.Background(), entities.TenantContext{TenantID: other.ID.String()})

	if _, err := h.write(elsewhere, h.sample(instanceID)); !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("another organization writing against this instance: got %v, want not found", err)
	}
	if _, err := h.repo.Deviation().ListByInstance(elsewhere, instanceID); !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("another organization reading this instance's ledger: got %v, want not found", err)
	}
	if rows, _ := h.repo.Deviation().ListByInstance(h.tenantContext(), instanceID); len(rows) != 1 {
		t.Fatalf("the ledger holds %d rows, want only the organization's own", len(rows))
	}
}

// A row naming an instance that is not in the project it names would point a
// reader at the wrong case. With no foreign key on instance_id (lock order),
// the repository checks it.
func TestARowNamingAnInstanceOutsideItsProjectIsRefused(t *testing.T) {
	h := newDeviationHarness(t)
	existing := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	other, err := h.svc.CreateProject(h.tenantContext(), h.orgID, "Another Project", "")
	if err != nil {
		t.Fatalf("create the second project: %v", err)
	}

	missing := h.sample(uuid.Must(uuid.NewV7()))
	if _, err := h.write(h.tenantContext(), missing); !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("a row naming an instance that does not exist: got %v, want not found", err)
	}

	// The instance exists, in this organization, but not in the project the row names.
	wrongProject := h.sample(existing)
	wrongProject.Project = &entities.Project{ID: other.ID}
	if _, err := h.write(h.tenantContext(), wrongProject); !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("a row naming an instance in another project of the same organization: got %v, want not found", err)
	}
	for _, id := range []uuid.UUID{existing, missing.Instance.ID} {
		if n := h.rowCount(t, id); n != 0 {
			t.Fatalf("a refused row left %d row(s) against instance %s", n, id)
		}
	}
}

// A kind or an instance the engine writer left out is its own mistake, not the
// client's: it is a plain error (a 500 that is logged), the unit of work rolls
// back, and no row is left.
func TestAMalformedDeviationIsRefusedAndLeavesNoRow(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})

	badKind := h.sample(instanceID)
	badKind.Kind = "reassign-ish"
	noInstance := h.sample(instanceID)
	noInstance.Instance = nil
	noProject := h.sample(instanceID)
	noProject.Project = nil
	for name, d := range map[string]entities.Deviation{"a bad kind": badKind, "no instance": noInstance, "no project": noProject} {
		_, err := h.write(h.tenantContext(), d)
		if err == nil {
			t.Fatalf("%s: the deviation was recorded", name)
		}
		if errors.Is(err, apierr.ErrInvalidArgument) || errors.Is(err, apierr.ErrNotFound) {
			t.Errorf("%s: refused as a client error (%v); an engine writer's mistake is a server error", name, err)
		}
	}
	if n := h.rowCount(t, instanceID); n != 0 {
		t.Fatalf("refused deviations left %d row(s)", n)
	}
}

// One act writes several rows in one transaction, which share created_at; the
// ledger still reads them in the order they were written.
func TestRowsWrittenInOneTransactionComeBackInWriteOrder(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	var want []uuid.UUID
	err := h.repo.UnitOfWork().Do(h.tenantContext(), func(tx context.Context) error {
		for range 3 {
			written, err := h.repo.Deviation().Create(tx, func() entities.Deviation {
				d := h.sample(instanceID)
				d.ID = uuid.Nil
				return d
			}())
			if err != nil {
				return err
			}
			want = append(want, written.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	rows, err := h.repo.Deviation().ListByInstance(h.tenantContext(), instanceID)
	if err != nil || len(rows) != 3 {
		t.Fatalf("read %d rows (err %v), want 3", len(rows), err)
	}
	for i := range rows {
		if rows[i].ID != want[i] {
			t.Fatalf("row %d is %s, want %s: rows of one transaction read in write order", i, rows[i].ID, want[i])
		}
	}
}

// A visit key identifies the work one in-place command acted on (design §7.6);
// 3a-2 asks for the live row of a visit before acting, so a retry replays
// instead of acting twice.
func TestTheLiveRowOfAVisitIsFoundAndOnlyIt(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	d := h.sample(instanceID)
	d.VisitKey = "dv1-the-visit"
	written, err := h.write(h.tenantContext(), d)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	found, ok, err := h.repo.Deviation().FindLiveByVisit(h.tenantContext(), instanceID, "dv1-the-visit")
	if err != nil || !ok || found.ID != written.ID || found.VisitKey != "dv1-the-visit" {
		t.Fatalf("the visit's row: %+v, %v, %v", found, ok, err)
	}
	if _, ok, err := h.repo.Deviation().FindLiveByVisit(h.tenantContext(), instanceID, "dv1-another-visit"); err != nil || ok {
		t.Fatalf("another visit found a row (%v, %v)", ok, err)
	}
	if _, err := h.write(h.tenantContext(), d); err == nil {
		t.Fatal("a second live row for the same visit was written; the unique index is the last line against acting twice")
	}
	other, err := h.svc.CreateOrganization(context.Background(), "Visit Other Org", "")
	if err != nil {
		t.Fatalf("create the other organization: %v", err)
	}
	elsewhere := entities.WithTenantContext(context.Background(), entities.TenantContext{TenantID: other.ID.String()})
	if _, _, err := h.repo.Deviation().FindLiveByVisit(elsewhere, instanceID, "dv1-the-visit"); !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("another organization asking for the visit: got %v, want not found", err)
	}
}

func TestAnInstancesDeviationsComeBackOldestFirst(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	var want []uuid.UUID
	for range 3 {
		written, err := h.write(h.tenantContext(), h.sample(instanceID))
		if err != nil {
			t.Fatalf("write: %v", err)
		}
		want = append(want, written.ID)
	}
	rows, err := h.repo.Deviation().ListByInstance(h.tenantContext(), instanceID)
	if err != nil || len(rows) != 3 {
		t.Fatalf("read %d rows (err %v), want 3", len(rows), err)
	}
	for i := range rows {
		if rows[i].ID != want[i] {
			t.Fatalf("row %d is %s, want %s: the ledger reads oldest first", i, rows[i].ID, want[i])
		}
	}
}

// A hold, a cancel and a waived control carry little or nothing to put in
// before, after or details, and the columns are NOT NULL: a deviation with an
// empty map must be recorded, not refused, or the change it records would be
// refused with it.
func TestADeviationWithNothingToCarryIsRecordedWithEmptyMaps(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})

	for name, empty := range map[string]map[string]any{"nil": nil, "empty": {}} {
		d := h.sample(instanceID)
		d.Kind, d.Scope, d.Origin = entities.DeviationHold, entities.DeviationScopeInstance, entities.DeviationOriginMigration
		d.Before, d.After, d.Details = empty, empty, empty
		written, err := h.write(h.tenantContext(), d)
		if err != nil {
			t.Fatalf("%s maps: the deviation was refused: %v", name, err)
		}
		if written.Before == nil || len(written.Before) != 0 || written.After == nil || len(written.After) != 0 ||
			written.Details == nil || len(written.Details) != 0 {
			t.Errorf("%s maps: the row returned by the write carries before=%v after=%v details=%v, want empty maps", name, written.Before, written.After, written.Details)
		}
		var before, after, details string
		if err := h.db.Raw(`SELECT before::text, after::text, details::text FROM instance_deviations WHERE id = ?`, written.ID).
			Row().Scan(&before, &after, &details); err != nil {
			t.Fatalf("%s maps: read the stored row: %v", name, err)
		}
		if before != "{}" || after != "{}" || details != "{}" {
			t.Errorf("%s maps: stored as before=%s after=%s details=%s, want {} for each", name, before, after, details)
		}
	}

	rows, err := h.repo.Deviation().ListByInstance(h.tenantContext(), instanceID)
	if err != nil || len(rows) != 2 {
		t.Fatalf("read %d rows (err %v), want 2", len(rows), err)
	}
	for _, row := range rows {
		if row.Before == nil || len(row.Before) != 0 || row.After == nil || len(row.After) != 0 ||
			row.Details == nil || len(row.Details) != 0 {
			t.Errorf("an empty row reads back as before=%v after=%v details=%v, want empty maps", row.Before, row.After, row.Details)
		}
	}
}

// Every column the repository writes comes back as it went in.
func TestADeviationReadsBackExactlyAsItWasWritten(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	decided := time.Date(2026, 10, 3, 9, 30, 0, 0, time.UTC)
	d := h.sample(instanceID)
	d.Task = &entities.Task{ID: uuid.Must(uuid.NewV7())}
	d.IterationID = "iteration-2"
	d.ActorID = uuid.Must(uuid.NewV7())
	d.AuditEntryID = uuid.Must(uuid.NewV7())
	d.VisitKey = "dv1-exact"
	// The ledger references the request a row names (migration 34), so the
	// request has to be there: written as a waive that waits for approval
	// writes it, through the repository.
	request, err := h.createRequest(h.tenantContext(), h.sampleRequest(instanceID, d.VisitKey))
	if err != nil {
		t.Fatalf("write the request the row names: %v", err)
	}
	d.RequestID = request.ID
	d.ApprovedBy = "carol"
	d.ApprovedByID = uuid.Must(uuid.NewV7())
	d.DecidedAt = &decided
	d.Status = entities.DeviationPendingApproval
	d.Before = map[string]any{"variables": map[string]any{"amount": "70400"}, "tasks": map[string]any{"t1": map[string]any{"assignee": "alice"}}}
	d.After = map[string]any{"variables": map[string]any{"amount": "80000"}}
	d.Details = map[string]any{"override": "not_a_candidate", "count": "3"}

	if _, err := h.write(h.tenantContext(), d); err != nil {
		t.Fatalf("write: %v", err)
	}
	rows, err := h.repo.Deviation().ListByInstance(h.tenantContext(), instanceID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("read %d rows (err %v), want 1", len(rows), err)
	}
	got := rows[0]
	if got.CreatedAt.IsZero() {
		t.Error("the row has no creation time")
	}
	got.CreatedAt = time.Time{}
	if got.DecidedAt == nil || !got.DecidedAt.Equal(decided) {
		t.Errorf("decided at reads back as %v, want %v", got.DecidedAt, decided)
	}
	got.DecidedAt, d.DecidedAt = nil, nil
	if got.Task == nil || got.Task.ID != d.Task.ID {
		t.Errorf("the task reads back as %+v, want %s", got.Task, d.Task.ID)
	}
	got.Task, d.Task = nil, nil
	if !reflect.DeepEqual(got, d) {
		t.Errorf("the row reads back as\n%+v\nwant\n%+v", got, d)
	}
}
