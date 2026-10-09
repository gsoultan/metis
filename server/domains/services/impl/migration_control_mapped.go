package impl

import (
	"github.com/gsoultan/metis/server/repositories/models"
)

// detailMappedTo is where the ledger row of a control that was not carried
// across as the same step says which step its instance was moved onto.
const detailMappedTo = "mapped_to"

// dataControlsMappedTo is the same on the instance_migrated entry: each such
// control's id, and the id of the step the instance was moved onto.
const dataControlsMappedTo = "controls_mapped_to"

// movedOntoAControl answers the step an instance waiting at a control is
// moved onto by a mapping, when that step is itself marked as a control and
// is not the same step under the same id.
//
// Such a control is a hold, and its loss has to be acknowledged: nothing can
// tell a control renamed with its neighbours changed from one redirected onto
// a different control, so it is not counted as carried across. But what
// happens to an instance waiting at it is not what happens to one whose
// control was dropped. It is moved onto a control of the new version, and
// may go on to perform exactly the step it was waiting for. A record that
// said it never will would say more than happened — so the row says where
// it was moved, and the trail says that, and neither says "never".
//
// A control the new version drops, or leaves unmarked, is not this: whatever
// the mapping does with the work that waited there, nobody performs that
// control again. Nor is an instance that was not waiting at the control: it
// is not moved by the mapping at all.
//
// instance is the row as locked, before the rewrite re-points its tokens.
func movedOntoAControl(
	instance models.ProcessInstanceModel,
	nodeID string,
	nodeMapping map[string]string,
	targetNodes map[string]models.FlowNode,
) (string, bool) {
	to := mapNode(nodeMapping, nodeID)
	if to == nodeID || !holdsWork(instance, nodeID) {
		return "", false
	}
	landed, lands := targetNodes[to]
	if !lands || !boolProperty(landed.Properties, "compliance_relevant") {
		return "", false
	}
	return to, true
}

// landedOnAControl is movedOntoAControl asked after the rewrite, of what the
// rewrite moved (from → to, for the steps the instance held work on): the
// step a control's work was moved to, when the new version marks that step
// as a control.
func landedOnAControl(moved map[string]string, nodeID string, target models.ProcessDefinitionModel) (string, bool) {
	to, was := moved[nodeID]
	if !was || to == nodeID {
		return "", false
	}
	landed, lands := findFlowNode(target.Nodes, to)
	if !lands || !boolProperty(landed.Properties, "compliance_relevant") {
		return "", false
	}
	return to, true
}
