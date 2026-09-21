package bpmn_test

import (
	"testing"

	"github.com/google/uuid"
)

// A staged version can be started deliberately, without being promoted.
//
// This is what makes staging useful rather than just cautious: deploy without
// going live, run one instance on it to see that it behaves, then promote. Until
// the start path could name a version, the only way to exercise a new one was to
// make it live for everybody first.
func TestStartingAStagedVersionWithoutPromotingIt(t *testing.T) {
	svc, projectID, ctx := releaseFixture(t)

	v1 := approvalModel(projectID, "v1", "hold")
	if _, err := svc.CreateDefinition(ctx, &v1); err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	v2 := approvalModel(projectID, "v2", "hold-revised")
	if _, err := svc.DeployDefinition(ctx, &v2, false); err != nil {
		t.Fatalf("stage v2: %v", err)
	}

	// Naming the staged version starts it.
	instanceID, err := svc.StartSubProcess(ctx, projectID, "expense-approval", 2, nil, uuid.Nil, "")
	if err != nil {
		t.Fatalf("start the staged version: %v", err)
	}
	instance, err := svc.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("get instance: %v", err)
	}
	def, err := svc.GetDefinition(ctx, instance.Definition.ID)
	if err != nil {
		t.Fatalf("get definition: %v", err)
	}
	if def.Version != 2 {
		t.Fatalf("naming version 2 should start version 2, got v%d", def.Version)
	}

	// And doing so has not promoted it: the next ordinary start is still v1.
	if got := startedVersion(t, ctx, svc, projectID); got != 1 {
		t.Fatalf("trying a staged version must not promote it, an ordinary start got v%d", got)
	}
}

// Naming a version that was never deployed is refused rather than silently
// falling back to the live one, which would run a different process than asked.
func TestStartingAnUndeployedVersionIsRefused(t *testing.T) {
	svc, projectID, ctx := releaseFixture(t)

	v1 := approvalModel(projectID, "v1", "hold")
	if _, err := svc.CreateDefinition(ctx, &v1); err != nil {
		t.Fatalf("deploy v1: %v", err)
	}

	if _, err := svc.StartSubProcess(ctx, projectID, "expense-approval", 9, nil, uuid.Nil, ""); err == nil {
		t.Fatal("starting a version that was never deployed should be refused")
	}
}
