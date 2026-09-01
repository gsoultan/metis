package idempotency

import (
	"testing"

	"github.com/google/uuid"
)

// TestTheServiceCallKeyIsFrozenAcrossTheRename pins the one string in this
// repository that the GoBPM-to-Metis rename must not touch.
//
// The key is regenerated per attempt rather than read back from service_calls,
// so it is what makes a retry recognisable to the downstream as the same
// request. Renaming the prefix would make every in-flight retry that spans an
// upgrade look new, and a service task's whole point is that it has an effect
// out in the world: the second execution is a second charge.
//
// If this test fails because someone finished the rename, the fix is to revert
// the prefix, not to update the expectation.
func TestTheServiceCallKeyIsFrozenAcrossTheRename(t *testing.T) {
	instance := uuid.MustParse("0195f3a0-0000-7000-8000-000000000001")

	got := ForServiceCall(instance, "charge", "")

	const want = "gobpm-WW4QaTdxXwIxuL8FB8ZaZIVEwfyCpJDq"
	if got != want {
		t.Fatalf("the service-call idempotency key changed:\n got %q\nwant %q\n\n"+
			"This key is what tells a downstream system that a retry is the same request.\n"+
			"Changing it means a job retrying across an upgrade charges the customer twice.\n"+
			"If this moved because the prefix was renamed to match the project, revert it:\n"+
			"the prefix is a wire value, not a brand.", got, want)
	}
}
