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

// movedOntoARenamedControl answers the step an instance waiting at a control
// is moved onto, in the one shape where the record does not know the control
// to be lost: the mapping renames the control by ids (renames, from
// renamedByIDs — the new id is no step of the version being left, nothing
// else is mapped onto it, and the old id is gone from the new version), and
// the step under the new id is marked as a control.
//
// Such a control is still a hold, and its loss has to be acknowledged and
// approved: a rename that is not in place — the steps round it changed — is
// not counted as the control carried across, because nothing can tell the
// same control under a new id from a different one put in its place. But an
// instance waiting at it is moved onto a control the new version adds, and
// may go on to perform exactly the step it waited for. So the record says
// that: the control was not carried across as the same step, and the
// instance was moved onto the step named. It does not say the control will
// be performed, and it does not say it never will.
//
// Every other shape is a control certainly lost, and keeps the record of one:
// the old id is still a step of the new version (the instance is put past
// it); the step it is mapped onto was already a step (the instance is put on
// a control it had to perform anyway); two controls are mapped onto one; the
// step it is mapped onto is no control; or the new version has no such step.
// Saying of any of those that the instance "was moved onto" a control would
// be milder than what the second administrator was shown.
//
// Nor is an instance that was not waiting at the control moved by the
// mapping at all. instance is the row as locked, before the rewrite.
func movedOntoARenamedControl(
	instance models.ProcessInstanceModel,
	nodeID string,
	renames map[string]string,
	targetNodes map[string]models.FlowNode,
) (string, bool) {
	to, renamed := renames[nodeID]
	if !renamed || !holdsWork(instance, nodeID) {
		return "", false
	}
	landed, lands := targetNodes[to]
	if !lands || !boolProperty(landed.Properties, "compliance_relevant") {
		return "", false
	}
	return to, true
}
