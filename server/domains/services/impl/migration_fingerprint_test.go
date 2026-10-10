package impl

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// The fingerprint is "what an approver agreed to": the same policy in any map
// or slice order is the same fingerprint, and any change of policy is not.
func TestMigrationFingerprint(t *testing.T) {
	src, tgt := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	one, two := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	base := servicecontracts.MigrationOptions{
		Actions:      map[string]servicecontracts.NodeAction{"a": {Kind: servicecontracts.NodeActionSkip, Reason: "moot"}, "b": {Kind: servicecontracts.NodeActionHold, Reason: "ask"}},
		Acknowledged: []string{"x", "y"}, Instances: []uuid.UUID{one, two},
	}
	holds := []entities.ComplianceHold{{NodeID: "x"}, {NodeID: "y"}}
	fp := migrationFingerprint(src, tgt, map[string]string{"p": "q", "r": "s"}, base, holds)
	reordered := base
	reordered.Acknowledged, reordered.Instances = []string{"y", "x"}, []uuid.UUID{two, one}
	if got := migrationFingerprint(src, tgt, map[string]string{"r": "s", "p": "q"}, reordered, []entities.ComplianceHold{{NodeID: "y"}, {NodeID: "x"}}); got != fp {
		t.Fatal("the same policy in another order is another fingerprint")
	}
	if !strings.HasPrefix(fp, "mf1-") || len(fp) != 36 {
		t.Fatalf("fingerprint %q", fp)
	}
	changed := []func(o *servicecontracts.MigrationOptions){
		func(o *servicecontracts.MigrationOptions) {
			o.Actions = map[string]servicecontracts.NodeAction{"a": {Kind: servicecontracts.NodeActionSkip, Reason: "other"}, "b": o.Actions["b"]}
		},
		func(o *servicecontracts.MigrationOptions) {
			o.Actions = map[string]servicecontracts.NodeAction{"a": {Kind: servicecontracts.NodeActionCancel, Reason: "moot"}, "b": o.Actions["b"]}
		},
		func(o *servicecontracts.MigrationOptions) {
			o.Actions = map[string]servicecontracts.NodeAction{"a": o.Actions["a"]}
		},
		func(o *servicecontracts.MigrationOptions) { o.Acknowledged = []string{"x"} },
		func(o *servicecontracts.MigrationOptions) { o.Instances = []uuid.UUID{one} },
		func(o *servicecontracts.MigrationOptions) { o.Instances = nil },
	}
	for i, change := range changed {
		o := base
		change(&o)
		if migrationFingerprint(src, tgt, map[string]string{"p": "q", "r": "s"}, o, holds) == fp {
			t.Errorf("change %d left the fingerprint as it was", i)
		}
	}
	if migrationFingerprint(src, tgt, map[string]string{"p": "z", "r": "s"}, base, holds) == fp ||
		migrationFingerprint(tgt, src, map[string]string{"p": "q", "r": "s"}, base, holds) == fp ||
		migrationFingerprint(src, tgt, map[string]string{"p": "q", "r": "s"}, base, holds[:1]) == fp {
		t.Error("a changed mapping, direction or hold set kept the fingerprint")
	}
	// Who asked and who approved are not the policy.
	named := base
	named.Actor, named.ActorID = "dita", two
	named.Approval = servicecontracts.MigrationApproval{RequestID: one, ApprovedBy: "omar"}
	if migrationFingerprint(src, tgt, map[string]string{"p": "q", "r": "s"}, named, holds) != fp {
		t.Error("the actor, the actor's account or the approval changed the fingerprint")
	}
	// A reason is the reason whatever spaces were typed round it, and an
	// instance or a control named twice is named once.
	spaced := base
	spaced.Actions = map[string]servicecontracts.NodeAction{"a": {Kind: servicecontracts.NodeActionSkip, Reason: "  moot\n"}, "b": base.Actions["b"]}
	spaced.Acknowledged, spaced.Instances = []string{"x", "y", "x"}, []uuid.UUID{one, two, one}
	if migrationFingerprint(src, tgt, map[string]string{"p": "q", "r": "s"}, spaced, holds) != fp {
		t.Error("spaces round a reason, or a name given twice, changed the fingerprint")
	}
}

// Two policies are never one fingerprint because of where their parts are
// cut: a step's id, a reason and a mapping are whatever somebody typed, and
// may hold any character a separator could be.
func TestTheFingerprintOfOnePolicyIsNotAnothersCutElsewhere(t *testing.T) {
	src, tgt := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	skip := func(node, reason string) servicecontracts.MigrationOptions {
		return servicecontracts.MigrationOptions{Actions: map[string]servicecontracts.NodeAction{node: {Kind: servicecontracts.NodeActionSkip, Reason: reason}}}
	}
	hold := func(node, reason string) servicecontracts.MigrationOptions {
		return servicecontracts.MigrationOptions{Actions: map[string]servicecontracts.NodeAction{node: {Kind: servicecontracts.NodeActionHold, Reason: reason}}}
	}
	pairs := []struct {
		name string
		a, b string
	}{
		{"a mapping's arrow in a step's id",
			migrationFingerprint(src, tgt, map[string]string{"a→b": "c"}, servicecontracts.MigrationOptions{}, nil),
			migrationFingerprint(src, tgt, map[string]string{"a": "b→c"}, servicecontracts.MigrationOptions{}, nil)},
		{"an action's kind in a step's id",
			migrationFingerprint(src, tgt, nil, skip("a", "hold:r"), nil),
			migrationFingerprint(src, tgt, nil, hold("a:skip", "r"), nil)},
		{"two mappings and one whose id holds the separator",
			migrationFingerprint(src, tgt, map[string]string{"a": "b", "c": "d"}, servicecontracts.MigrationOptions{}, nil),
			migrationFingerprint(src, tgt, map[string]string{"a": "b\x00c→d"}, servicecontracts.MigrationOptions{}, nil)},
		{"an acknowledged control and a dropped one",
			migrationFingerprint(src, tgt, nil, servicecontracts.MigrationOptions{Acknowledged: []string{"x"}}, nil),
			migrationFingerprint(src, tgt, nil, servicecontracts.MigrationOptions{}, []entities.ComplianceHold{{NodeID: "x"}})},
		{"two acknowledged controls and one whose id holds the separator",
			migrationFingerprint(src, tgt, nil, servicecontracts.MigrationOptions{Acknowledged: []string{"x", "y"}}, nil),
			migrationFingerprint(src, tgt, nil, servicecontracts.MigrationOptions{Acknowledged: []string{"x\x00y"}}, nil)},
	}
	for _, pair := range pairs {
		if pair.a == pair.b {
			t.Errorf("%s: two policies have one fingerprint", pair.name)
		}
	}
}
