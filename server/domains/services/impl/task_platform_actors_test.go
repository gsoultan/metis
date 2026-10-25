package impl

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
)

func TestTheSwitchOverWindowIsOpenUnlessItIsClosedOnPurpose(t *testing.T) {
	for raw, want := range map[string]bool{
		"": true, "true": true, "1": true, " TRUE ": true,
		"false": false, "0": false, "FALSE": false,
		"of": true, // unreadable: queues keep running rather than all stopping
	} {
		t.Setenv(EnvTasksAllowPlatformActors, raw)
		if got := PlatformActorsAllowed(); got != want {
			t.Errorf("%s=%q: allowed = %v, want %v", EnvTasksAllowPlatformActors, raw, got, want)
		}
	}
}

func TestAUsersClaimIsRefusedOnceTheWindowIsClosed(t *testing.T) {
	t.Setenv(EnvTasksAllowPlatformActors, "false")
	ran := false
	err := asPlatformActor("claim", uuid.New(), "dana", func() error { ran = true; return nil })
	if !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("claim with the window closed: %v, want forbidden", err)
	}
	if ran {
		t.Fatal("the claim ran with the window closed")
	}

	t.Setenv(EnvTasksAllowPlatformActors, "")
	if err := asPlatformActor("claim", uuid.New(), "dana", func() error { ran = true; return nil }); err != nil || !ran {
		t.Fatalf("claim with the window open: ran=%v err=%v", ran, err)
	}
}
