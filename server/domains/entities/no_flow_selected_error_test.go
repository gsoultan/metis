package entities

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

// The engine routes a failure to an error boundary event by its text: one
// that begins BPMN_ERROR: is catchable, and the rest of it is its code. So
// what this error says is not its to reword — a definition may catch exactly
// these words.
func TestAGatewayWithNoWayOutSaysWhatItAlwaysSaid(t *testing.T) {
	for kind, want := range map[string]string{
		GatewayKindExclusive: `BPMN_ERROR:no outgoing sequence flow could be selected at exclusive gateway "decide": ` +
			"no condition evaluated true and no default flow is declared",
		GatewayKindInclusive: `BPMN_ERROR:no outgoing sequence flow could be selected at inclusive gateway "decide": ` +
			"no condition evaluated true and no default flow is declared",
	} {
		err := &NoFlowSelectedError{GatewayKind: kind, GatewayID: "decide", GatewayName: "Verdict?", InstanceID: uuid.Must(uuid.NewV7())}
		if got := err.Error(); got != want {
			t.Errorf("an %s gateway with no way out says\n  %s\nwant, byte for byte,\n  %s", kind, got, want)
		}
	}
	// An id is quoted as Go quotes a string, as it always was.
	odd := &NoFlowSelectedError{GatewayKind: GatewayKindExclusive, GatewayID: "a \"b\"\n"}
	if got, want := odd.Error(), fmt.Sprintf(
		"BPMN_ERROR:no outgoing sequence flow could be selected at exclusive gateway %q: "+
			"no condition evaluated true and no default flow is declared", "a \"b\"\n"); got != want {
		t.Errorf("an id that needs quoting: got %s, want %s", got, want)
	}
}

// Whoever asked for the advance finds the error under whatever was wrapped
// around it on the way up, and reads from it which gateway it was and which
// instance was at it.
func TestAGatewayWithNoWayOutIsFoundUnderWhatWrapsIt(t *testing.T) {
	instance := uuid.Must(uuid.NewV7())
	var raised error = &NoFlowSelectedError{GatewayKind: GatewayKindInclusive, GatewayID: "decide", GatewayName: "Verdict?", InstanceID: instance}
	wrapped := fmt.Errorf("waiving a step: %w", fmt.Errorf("advancing past it: %w", raised))

	var found *NoFlowSelectedError
	if !errors.As(wrapped, &found) {
		t.Fatal("the error is not found under what wraps it")
	}
	if found.GatewayID != "decide" || found.GatewayName != "Verdict?" || found.InstanceID != instance {
		t.Errorf("found %+v, want the gateway and the instance it was raised for", found)
	}
	if errors.As(errors.New("BPMN_ERROR:execution exceeded 1000 nodes"), &found) {
		t.Error("another error that begins BPMN_ERROR: is taken for a gateway with no way out")
	}
}
