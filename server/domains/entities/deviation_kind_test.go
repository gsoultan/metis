package entities

import "testing"

// The ledger's vocabulary is a closed set. A kind the code does not know is a
// row nobody can read back with confidence, so Record refuses it; and which
// kinds need a reason is the rule an auditor reads the ledger by.
func TestDeviationKindsAreAClosedSetWithTheirReasonRule(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind               DeviationKind
		valid, needsReason bool
		reasonForbidden    bool
	}{
		{DeviationWaive, true, true, false},
		{DeviationCancel, true, true, false},
		{DeviationHold, true, true, false},
		{DeviationControlWaived, true, false, true},
		{DeviationReassign, true, true, false},
		{DeviationDelegate, true, true, false},
		{DeviationResolve, true, true, false},
		{DeviationRelease, true, true, false},
		{DeviationTaskEdit, true, true, false},
		{DeviationAdHocActivation, true, false, false},
		{"skip", false, false, false},
		{"", false, false, false},
	}
	for _, c := range cases {
		if got := c.kind.Valid(); got != c.valid {
			t.Errorf("%q.Valid() = %v, want %v", c.kind, got, c.valid)
		}
		if got := c.kind.ReasonRequired(); got != c.needsReason {
			t.Errorf("%q.ReasonRequired() = %v, want %v", c.kind, got, c.needsReason)
		}
		if got := c.kind.ReasonForbidden(); got != c.reasonForbidden {
			t.Errorf("%q.ReasonForbidden() = %v, want %v", c.kind, got, c.reasonForbidden)
		}
	}
}

func TestDeviationScopesOriginsAndStatusesAreClosedSets(t *testing.T) {
	t.Parallel()
	for _, scope := range []DeviationScope{DeviationScopeTask, DeviationScopeIteration, DeviationScopeInstance} {
		if !scope.Valid() {
			t.Errorf("scope %q is refused", scope)
		}
	}
	if DeviationScope("forever").Valid() {
		t.Error("an unknown scope is accepted")
	}
	for _, origin := range []DeviationOrigin{DeviationOriginInPlace, DeviationOriginMigration, DeviationOriginTask, DeviationOriginAdHoc} {
		if !origin.Valid() {
			t.Errorf("origin %q is refused", origin)
		}
	}
	if DeviationOrigin("api").Valid() {
		t.Error("an unknown origin is accepted")
	}
	live := map[DeviationStatus]bool{
		DeviationApplied: true, DeviationPendingApproval: true,
		DeviationRejected: false, DeviationExpired: false, DeviationStale: false,
	}
	for status, wantLive := range live {
		if !status.Valid() {
			t.Errorf("status %q is refused", status)
		}
		if got := status.Live(); got != wantLive {
			t.Errorf("%q.Live() = %v, want %v", status, got, wantLive)
		}
	}
	if DeviationStatus("done").Valid() {
		t.Error("an unknown status is accepted")
	}
}
