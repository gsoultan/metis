package bpmn_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

// A repeating timer written in milliseconds.
//
// A cycle is due again as soon as it has fired, so "R/PT0.001S" fired on
// every poll of the job worker for as long as the instance lived, each
// occurrence a job and a run of whatever the timer leads to. Deploy accepted
// it because nothing read the interval until an instance reached the timer.
func TestARepeatingTimerFasterThanOncePerSecondIsRefusedAtDeploy(t *testing.T) {
	h := newEngineHarness(t, "Timer Interval Project")

	def := timerDefinition("nag-constantly", "R/PT0.001S")
	def.Project = &entities.Project{ID: h.projID}
	_, err := h.svc.CreateDefinition(h.Ctx(), &def)
	if err == nil {
		t.Fatal("a timer repeating every millisecond deployed")
	}
	if !strings.Contains(err.Error(), "wait") || !strings.Contains(err.Error(), "R/PT0.001S") {
		t.Fatalf("the refusal does not name the step and the timer: %v", err)
	}
	if !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("the refusal is not marked as the caller's mistake: %v", err)
	}

	ok := timerDefinition("nag-every-second", "R3/PT1S")
	ok.Project = &entities.Project{ID: h.projID}
	if _, err := h.svc.CreateDefinition(h.Ctx(), &ok); err != nil {
		t.Errorf("a timer repeating once a second was refused: %v", err)
	}
}
