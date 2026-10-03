package impl

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// The ledger keeps 255 characters of a step's name. A definition may name a
// step at greater length, and a row that then failed to insert would fail a
// migration at apply that its dry run called fine. The id identifies the step;
// the name is for reading, so the row keeps as much of it as fits.
func TestALedgerRowKeepsAsMuchOfALongStepNameAsFits(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("persetujuan — ", 30)
	if utf8.RuneCountInString(long) <= ledgerNodeNameLength {
		t.Fatalf("the name under test is only %d characters", utf8.RuneCountInString(long))
	}
	instance := models.ProcessInstanceModel{}
	instance.ID = models.UUID(uuid.New())

	decision := decisionDeviation(decisionRecord{
		instance: instance, nodeID: "opsApprove",
		action: servicecontracts.NodeAction{Kind: servicecontracts.NodeActionSkip, Reason: "policy"},
	}, long)
	losses := controlLossDeviations(instance, uuid.New(),
		[]entities.ComplianceHold{{NodeID: "opsApprove", Name: long}}, "dita", uuid.New(), uuid.New())

	for told, node := range map[string]*entities.Node{"a decision": decision.Node, "a control loss": losses[0].Node} {
		if node.ID != "opsApprove" {
			t.Errorf("%s names step %q, want its id untouched", told, node.ID)
		}
		if got := utf8.RuneCountInString(node.Name); got != ledgerNodeNameLength {
			t.Errorf("%s keeps %d characters of the name, want %d", told, got, ledgerNodeNameLength)
		}
		if !utf8.ValidString(node.Name) || !strings.HasPrefix(long, node.Name) {
			t.Errorf("%s keeps %q, which is not the start of the name cut between characters", told, node.Name)
		}
	}
}

func TestALedgerRowKeepsAShortStepNameWhole(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", "Operations approve", strings.Repeat("é", ledgerNodeNameLength)} {
		if got := ledgerNodeName(name); got != name {
			t.Errorf("a name of %d characters was changed to %q", utf8.RuneCountInString(name), got)
		}
	}
}

// The name a decision's row carries is the step's own, found anywhere in the
// version's graph, and the step's id where it has no name.
func TestADecisionsStepIsNamedFromTheGraph(t *testing.T) {
	t.Parallel()
	nodes := []models.FlowNode{
		{ID: "start"},
		{ID: "review", Name: "Review", Nodes: []models.FlowNode{
			{ID: "opsApprove", Name: "Operations approve"},
			{ID: "unnamed"},
		}},
	}
	for id, want := range map[string]string{
		"review": "Review", "opsApprove": "Operations approve", "unnamed": "unnamed", "gone": "gone",
	} {
		if got := nodeNameIn(nodes, id); got != want {
			t.Errorf("step %q is named %q, want %q", id, got, want)
		}
	}
}
