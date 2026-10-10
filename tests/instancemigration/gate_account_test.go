package instancemigration

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
)

// The gate admits a run under an approved request on the account that asked,
// never on the name. A sign-in name is what a caller writes into the
// options, and an account may come to hold a name another once had; the
// account id is the one thing both sides hold that nobody can retype. So an
// apply under an approved request that names the requester by name and
// another account — or no account at all: absent means deny — is refused
// before anything is moved, and the request is left as it was.
func TestTheGateAdmitsAnApprovedRunOnTheRequestersAccountNotTheName(t *testing.T) {
	refused := map[string][]servicecontracts.MigrationOption{
		"the requester's name with another account's id":  {servicecontracts.WithActor("dita"), servicecontracts.WithActorAccount(accountOf("pia"))},
		"the requester's name with an account nobody has": {servicecontracts.WithActor("dita"), servicecontracts.WithActorAccount(uuid.Must(uuid.NewV7()))},
		"the requester's name and no account":             {servicecontracts.WithActor("dita")},
		"the requester's name and the nil account":        {servicecontracts.WithActor("dita"), servicecontracts.WithActorAccount(uuid.Nil)},
		"nobody named at all":                             {servicecontracts.WithActor("")},
	}
	for name, naming := range refused {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			_, v1, v2, requestID := f.askToSkipOps(t)
			f.leftApproved(t, requestID, "1 minute")
			under := append(slices.Clip(skipOps("the role was eliminated")), naming...)
			under = append(under, servicecontracts.WithApprovedRequest(requestID))
			for range 2 {
				_, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, under...)
				if !errors.Is(err, apierr.ErrForbidden) || !strings.Contains(err.Error(), "was asked for by dita") ||
					!strings.Contains(err.Error(), "nothing was moved") {
					t.Fatalf("an apply under the approved request, naming %s: %v, want it refused at the gate", name, err)
				}
				f.assertNothingMoved(t, v1)
			}
			if stored := f.storedStatus(t, requestID); stored != "approved" {
				t.Fatalf("the refused apply left the request %s; the gate only verifies, and writes nothing", stored)
			}
		})
	}

	// The same apply naming the account that asked is the one admitted: what
	// the cases above were refused for is the account, and nothing else.
	t.Run("the requester's account", func(t *testing.T) {
		f := newFixture(t)
		_, v1, v2, requestID := f.askToSkipOps(t)
		f.leftApproved(t, requestID, "1 minute")
		under := append(slices.Clip(skipOps("the role was eliminated")), asDita(), servicecontracts.WithApprovedRequest(requestID))
		result, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, under...)
		if err != nil || result.Changed != 1 {
			t.Fatalf("an apply under the approved request, naming the account that asked: %+v %v, want the instance moved", result, err)
		}
		f.requireSkippedUnder(t, v2, requestID)
	})

	// The name alone admits nobody and refuses nobody: a run that names the
	// account that asked is that account's, whatever it is called.
	t.Run("the requester's account under another name", func(t *testing.T) {
		f := newFixture(t)
		_, v1, v2, requestID := f.askToSkipOps(t)
		f.leftApproved(t, requestID, "1 minute")
		under := append(slices.Clip(skipOps("the role was eliminated")), servicecontracts.WithActor("dita.renamed"),
			asDita(), servicecontracts.WithApprovedRequest(requestID))
		result, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, under...)
		if err != nil || result.Changed != 1 {
			t.Fatalf("an apply naming the account that asked under another name: %+v %v, want the instance moved", result, err)
		}
		// What is recorded is who the stored request says asked — in the
		// ledger and on the trail alike — never the name the caller wrote.
		f.requireSkippedUnder(t, v2, requestID)
		entries, err := f.svc.GetAuditLogs(f.ctx, f.onlyInstance(t).ID)
		if err != nil {
			t.Fatalf("read the trail: %v", err)
		}
		told := 0
		for _, entry := range entries {
			if strings.Contains(entry.Message, "dita.renamed") || strings.Contains(entry.Narrative, "dita.renamed") {
				t.Fatalf("the trail's %s entry names the caller's own word for who authorised it: %q %q", entry.Type, entry.Message, entry.Narrative)
			}
			if entry.Type == impl.EventInstanceMigrated && strings.Contains(entry.Narrative, "authorised by dita.") {
				told++
			}
		}
		if told != 1 {
			t.Fatalf("%d entr(ies) say the instance was moved by a migration authorised by dita, want the one", told)
		}
	})

	// The approve route builds its run from the stored request, account and
	// all, so the run an approval makes is admitted as it always was.
	t.Run("the approval's own run", func(t *testing.T) {
		f := newFixture(t)
		_, _, v2, requestID := f.askToSkipOps(t)
		out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), requestID, "")
		if err != nil || !out.Applied || out.MigrationResult == nil || out.MigrationResult.Changed != 1 {
			t.Fatalf("omar approves: %+v %v, want the approval's run admitted and the instance moved", out, err)
		}
		if got := f.storedStatus(t, requestID); got != string(entities.DeviationRequestApplied) {
			t.Fatalf("the request is %s after its run, want applied", got)
		}
		f.requireSkippedUnder(t, v2, requestID)
	})
}

// requireSkippedUnder fails unless the fixture's one instance runs target
// with one row in its ledger: the skip, under the request, asked for by dita
// — name and account as the stored request holds them — and approved by omar.
func (f *fixture) requireSkippedUnder(t *testing.T, target, requestID uuid.UUID) {
	t.Helper()
	instance := f.onlyInstance(t)
	if instance.Definition == nil || instance.Definition.ID != target {
		t.Fatalf("the instance is on %v, want the version migrated to", instance.Definition)
	}
	rows := f.ledger(t, instance.ID)
	if len(rows) != 1 || rows[0].RequestID != requestID || rows[0].Actor != "dita" || rows[0].ActorID != accountOf("dita") ||
		rows[0].ApprovedBy != "omar" || rows[0].ApprovedByID != accountOf("omar") {
		t.Fatalf("the ledger holds %+v, want the one skip under request %s, asked for by dita and approved by omar", rows, requestID)
	}
}
