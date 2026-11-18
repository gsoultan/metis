package release

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
)

func TestTheViewShowsWhatAReleasePassedWhereItWasCut(t *testing.T) {
	at := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	checked := ViewOf(entities.Release{Bundle: entities.ReleaseBundle{
		Checked: &entities.ReleaseChecks{At: at, DecisionTests: 3, SimulationCases: 2},
	}})
	body, err := json.Marshal(checked)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"checked":{"at":"2026-11-17T09:00:00Z","decision_tests":3,"simulation_cases":2}`) {
		t.Fatalf("the view does not show the checks: %s", body)
	}

	body, err = json.Marshal(ViewOf(entities.Release{}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"checked"`) {
		t.Fatalf("a release cut before releases were checked shows checks: %s", body)
	}
}
