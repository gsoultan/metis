package bpmn_test

import (
	"errors"
	"testing"
	"time"

	"github.com/gsoultan/metis/internal/pkg/apierr"
)

// A cutover arranged for the future does not take effect early, and does take
// effect once its time has passed — with nothing running in between.
//
// The clock is the only mechanism: the live version is resolved from the release
// timeline on every read, so this test moves the *schedule* into the near past
// rather than waiting, and asserts the same read gives a different answer.
func TestScheduledVersionTakesOverWhenItsTimeArrives(t *testing.T) {
	svc, projectID, ctx := releaseFixture(t)

	v1 := approvalModel(projectID, "v1", "hold")
	if _, err := svc.CreateDefinition(ctx, &v1); err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	v2 := approvalModel(projectID, "v2", "hold-revised")
	if _, err := svc.DeployDefinition(ctx, &v2, false); err != nil {
		t.Fatalf("stage v2: %v", err)
	}

	// Arranged for an hour out: v1 keeps taking work until then.
	cutover := time.Now().UTC().Add(time.Hour)
	if err := svc.ScheduleDefinitionVersion(ctx, projectID, "expense-approval", 2, cutover); err != nil {
		t.Fatalf("schedule v2: %v", err)
	}
	if got := startedVersion(t, ctx, svc, projectID); got != 1 {
		t.Fatalf("a cutover an hour away must not take effect now, instance started on v%d", got)
	}

	versions, err := svc.ListDefinitionVersions(ctx, projectID, "expense-approval")
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if versions[0].Version != 2 || versions[0].Live {
		t.Fatalf("v2 is scheduled, not live yet: got v%d live=%v", versions[0].Version, versions[0].Live)
	}
	if versions[0].ScheduledFor.IsZero() {
		t.Fatal("v2 should report when it is arranged to take over")
	}
	if !versions[0].ScheduledFor.Equal(cutover.Truncate(time.Second)) &&
		versions[0].ScheduledFor.Sub(cutover).Abs() > time.Second {
		t.Fatalf("scheduled time should round-trip, wanted ~%s got %s", cutover, versions[0].ScheduledFor)
	}

	// Bring the same cutover into the past. Nothing else happens — no worker
	// runs, no flag is flipped — and the next read resolves differently.
	if err := svc.PromoteDefinitionVersion(ctx, projectID, "expense-approval", 2); err != nil {
		t.Fatalf("bring the cutover forward: %v", err)
	}
	if got := startedVersion(t, ctx, svc, projectID); got != 2 {
		t.Fatalf("once the cutover has passed, new instances should start on v2, got v%d", got)
	}
}

// A time already gone is refused rather than quietly treated as "now". The two
// are different acts, and a past cutover landing behind an existing entry would
// change nothing at all while reporting success.
func TestSchedulingInThePastIsRefused(t *testing.T) {
	svc, projectID, ctx := releaseFixture(t)

	v1 := approvalModel(projectID, "v1", "hold")
	if _, err := svc.CreateDefinition(ctx, &v1); err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	v2 := approvalModel(projectID, "v2", "hold-revised")
	if _, err := svc.DeployDefinition(ctx, &v2, false); err != nil {
		t.Fatalf("stage v2: %v", err)
	}

	err := svc.ScheduleDefinitionVersion(ctx, projectID, "expense-approval", 2, time.Now().UTC().Add(-time.Hour))
	if err == nil {
		t.Fatal("scheduling a cutover in the past should be refused")
	}
	if !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("expected an invalid-argument refusal, got %v", err)
	}
	if got := startedVersion(t, ctx, svc, projectID); got != 1 {
		t.Fatalf("a refused schedule must not move the live version, got v%d", got)
	}
}

// Scheduling a version nobody deployed is refused, for the same reason promoting
// one is: the entry would resolve to nothing and silently fall back.
func TestSchedulingAnUndeployedVersionIsRefused(t *testing.T) {
	svc, projectID, ctx := releaseFixture(t)

	v1 := approvalModel(projectID, "v1", "hold")
	if _, err := svc.CreateDefinition(ctx, &v1); err != nil {
		t.Fatalf("deploy v1: %v", err)
	}

	err := svc.ScheduleDefinitionVersion(ctx, projectID, "expense-approval", 9, time.Now().UTC().Add(time.Hour))
	if err == nil {
		t.Fatal("scheduling a version that was never deployed should be refused")
	}
	if !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("expected a not-found refusal, got %v", err)
	}
}

// A pending cutover can be called off, and calling it off leaves the version
// that is live alone.
func TestCancellingAScheduledCutover(t *testing.T) {
	svc, projectID, ctx := releaseFixture(t)

	v1 := approvalModel(projectID, "v1", "hold")
	if _, err := svc.CreateDefinition(ctx, &v1); err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	v2 := approvalModel(projectID, "v2", "hold-revised")
	if _, err := svc.DeployDefinition(ctx, &v2, false); err != nil {
		t.Fatalf("stage v2: %v", err)
	}
	if err := svc.ScheduleDefinitionVersion(ctx, projectID, "expense-approval", 2, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatalf("schedule v2: %v", err)
	}

	versions, err := svc.ListDefinitionVersions(ctx, projectID, "expense-approval")
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if versions[0].ScheduledReleaseID.String() == "" || versions[0].ScheduledFor.IsZero() {
		t.Fatal("v2 should carry the cutover that can be cancelled")
	}

	if err := svc.CancelScheduledVersion(ctx, projectID, versions[0].ScheduledReleaseID); err != nil {
		t.Fatalf("cancel the cutover: %v", err)
	}

	versions, err = svc.ListDefinitionVersions(ctx, projectID, "expense-approval")
	if err != nil {
		t.Fatalf("list versions after cancelling: %v", err)
	}
	if !versions[0].ScheduledFor.IsZero() {
		t.Fatalf("the cutover was cancelled, so nothing should be pending, got %s", versions[0].ScheduledFor)
	}
	if got := startedVersion(t, ctx, svc, projectID); got != 1 {
		t.Fatalf("cancelling a future cutover must leave the live version alone, got v%d", got)
	}
}

// A cutover that has already taken effect is history. Cancelling it would
// rewrite which version has been in force, and strand the instances that started
// on it under a timeline that no longer admits to having chosen it.
func TestAnAppliedCutoverCannotBeCancelled(t *testing.T) {
	svc, projectID, ctx := releaseFixture(t)

	v1 := approvalModel(projectID, "v1", "hold")
	if _, err := svc.CreateDefinition(ctx, &v1); err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	v2 := approvalModel(projectID, "v2", "hold-revised")
	if _, err := svc.CreateDefinition(ctx, &v2); err != nil {
		t.Fatalf("deploy v2: %v", err)
	}

	// v2 was promoted by that deploy, so its timeline entry is already in force.
	versions, err := svc.ListDefinitionVersions(ctx, projectID, "expense-approval")
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if !versions[0].Live {
		t.Fatalf("v2 should be live after a promoting deploy")
	}
	if !versions[0].ScheduledFor.IsZero() {
		t.Fatal("a cutover that has taken effect is not pending")
	}

	// Cancelling by an id that names no *pending* entry is refused.
	err = svc.CancelScheduledVersion(ctx, projectID, versions[0].ID)
	if err == nil {
		t.Fatal("cancelling an applied cutover should be refused")
	}
	if !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("expected a not-found refusal, got %v", err)
	}
	if got := startedVersion(t, ctx, svc, projectID); got != 2 {
		t.Fatalf("a refused cancel must leave the live version alone, got v%d", got)
	}
}
