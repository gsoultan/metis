package bpmn_test

import (
	"testing"
	"time"
)

// The versions page asks which version of each process is live, and the
// engine answers that from the release timeline on every start. The page's
// answer came from walking the whole timeline, newest first, and letting each
// row overwrite the last — so it named the oldest version ever promoted, and a
// scheduled cutover was not told apart from one that had happened.
func TestTheLiveVersionListedIsTheOneInstancesStartOn(t *testing.T) {
	svc, projectID, ctx := releaseFixture(t)
	listed := func() int {
		t.Helper()
		live, err := svc.ListLiveVersions(ctx, projectID)
		if err != nil {
			t.Fatalf("list live versions: %v", err)
		}
		return live["expense-approval"]
	}

	for _, name := range []string{"v1", "v2", "v3"} {
		def := approvalModel(projectID, name, "hold-"+name)
		if _, err := svc.CreateDefinition(ctx, &def); err != nil {
			t.Fatalf("deploy %s: %v", name, err)
		}
	}
	if got, runs := listed(), startedVersion(t, ctx, svc, projectID); got != runs {
		t.Fatalf("after three deploys the list says v%d is live; new instances start on v%d", got, runs)
	}

	if err := svc.PromoteDefinitionVersion(ctx, projectID, "expense-approval", 2); err != nil {
		t.Fatalf("roll back to v2: %v", err)
	}
	if got, runs := listed(), startedVersion(t, ctx, svc, projectID); got != runs {
		t.Fatalf("after a rollback the list says v%d is live; new instances start on v%d", got, runs)
	}

	if err := svc.ScheduleDefinitionVersion(ctx, projectID, "expense-approval", 3, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("schedule v3: %v", err)
	}
	if got, runs := listed(), startedVersion(t, ctx, svc, projectID); got != runs {
		t.Fatalf("with a cutover an hour away the list says v%d is live; new instances start on v%d", got, runs)
	}
}
