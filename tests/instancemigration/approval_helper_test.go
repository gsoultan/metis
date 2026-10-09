package instancemigration

import (
	"context"
	"testing"

	"github.com/google/uuid"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
)

// accountOf is the account id of the test administrator called name: derived
// from the name, so the same name is the same account wherever it is used.
func accountOf(name string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("metis-test-account:"+name))
}

// adminAs is ctx from a signed-in administrator called name, whose account id
// is derived from the name.
func adminAs(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, pkgauth.UserContextKey, entities.User{
		ID: accountOf(name), Username: name, Roles: []string{entities.RoleAdmin}})
}

// requestCount is how many requests for a second administrator there are.
func (f *fixture) requestCount(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := f.db.Raw(`SELECT count(*) FROM deviation_requests`).Row().Scan(&n); err != nil {
		t.Fatalf("count the requests: %v", err)
	}
	return n
}

// storedStatus is a request's status as its row holds it, whatever a reader
// would be told it reads as now.
func (f *fixture) storedStatus(t *testing.T, requestID uuid.UUID) string {
	t.Helper()
	var stored string
	if err := f.db.Raw(`SELECT status FROM deviation_requests WHERE id = ?`, requestID).Row().Scan(&stored); err != nil {
		t.Fatalf("read the request: %v", err)
	}
	return stored
}

// assertNothingMoved fails unless the one instance is still running v1 with
// its operations approval open, nothing in its ledger and nothing on its
// trail that a migration writes.
func (f *fixture) assertNothingMoved(t *testing.T, v1 uuid.UUID) {
	t.Helper()
	instance := f.onlyInstance(t)
	if instance.Definition == nil || instance.Definition.ID != v1 || instance.Status != entities.ProcessActive {
		t.Fatalf("the instance is %s on %v, want it running the version it was started on", instance.Status, instance.Definition)
	}
	if open := f.openTasks(t); len(open) != 1 || open[0].NodeID() != "opsApprove" {
		t.Fatalf("%d task(s) are open (%+v), want the one operations approval", len(open), open)
	}
	if rows := f.ledger(t, instance.ID); len(rows) != 0 {
		t.Fatalf("the ledger holds %d row(s) for an instance nothing was done to: %+v", len(rows), rows)
	}
	f.assertNoMigrationEntries(t, instance.ID)
}
