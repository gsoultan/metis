package entities

import (
	"fmt"

	"github.com/google/uuid"
)

// The kinds of gateway that fail when no outgoing flow can be selected, as
// the failure has always named them.
const (
	GatewayKindExclusive = "exclusive"
	GatewayKindInclusive = "inclusive"
)

// NoFlowSelectedError is a gateway failing because none of its outgoing
// flows' conditions held and it declares no default flow (BPMN 2.0.2
// §13.3.2: "an exception is thrown").
//
// It is a type so that whoever asked for the advance can tell this failure
// from every other — what was decided on fits no branch, which is something
// the person who supplied a value can put right — and can say which gateway
// it was, of which instance: an advance can run on into the process that
// called this one, and the gateway may be there.
//
// Its text is not its to change. The engine routes a failure to an error
// boundary event by that text: one that begins "BPMN_ERROR:" can be caught,
// and what follows the prefix is the code a boundary event names. Error
// returns exactly what the gateways returned before this was a type.
type NoFlowSelectedError struct {
	// GatewayKind is GatewayKindExclusive or GatewayKindInclusive.
	GatewayKind string
	// GatewayID is the gateway's id in its definition, and GatewayName its
	// name there, which may be empty.
	GatewayID, GatewayName string
	// InstanceID is the instance that was at the gateway.
	InstanceID uuid.UUID
}

func (e *NoFlowSelectedError) Error() string {
	return fmt.Sprintf(
		"BPMN_ERROR:no outgoing sequence flow could be selected at %s gateway %q: "+
			"no condition evaluated true and no default flow is declared", e.GatewayKind, e.GatewayID)
}
