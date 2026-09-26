package idempotency

import (
	"testing"

	"github.com/google/uuid"
)

// TestTheServiceCallKeyIsFrozenAcrossTheRename pins the one string in this
// repository that the GoBPM-to-Metis rename must not touch.
//
// The key is what makes a retry recognisable to the downstream as the same
// request. The engine sends the key stored with the call, so a call already in
// flight keeps its key across an upgrade; the prefix is still a wire value a
// partner's own records hold, and a service task's whole point is that it has
// an effect out in the world: a repeat it does not recognise is a second charge.
//
// If this test fails because someone finished the rename, the fix is to revert
// the prefix, not to update the expectation.
func TestTheServiceCallKeyIsFrozenAcrossTheRename(t *testing.T) {
	instance := uuid.MustParse("0195f3a0-0000-7000-8000-000000000001")
	visit := uuid.MustParse("0195f3a0-0000-7000-8000-000000000002")

	got := ForServiceCall(instance, "charge", "", visit)

	// Changed once, on 2026-09-25, when the visit joined the key so a loop's
	// second pass is a new request. That change was safe for the reason this
	// test exists: a call already in flight keeps the key stored on its record,
	// and the engine sends the stored key, not a recomputed one.
	const want = "gobpm-tMKQXVcLvqmS-zfto5e-MBA3i5flASyT"
	if got != want {
		t.Fatalf("the service-call idempotency key changed:\n got %q\nwant %q\n\n"+
			"This key is what tells a downstream system that a retry is the same request.\n"+
			"Changing it means a job retrying across an upgrade charges the customer twice.\n"+
			"If this moved because the prefix was renamed to match the project, revert it:\n"+
			"the prefix is a wire value, not a brand.", got, want)
	}
}
