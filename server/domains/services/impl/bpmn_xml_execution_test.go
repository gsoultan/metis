package impl

import (
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// What a business rule task decides with and what a service task calls are
// part of the step, as much as who does a user task. Import dropped both, so a
// decision step deployed from a file failed on its first token with no table
// to evaluate, and a call step ran as a no-op: the process moved on as though
// the API had answered.
func TestWhatAStepRunsSurvivesTheRoundTrip(t *testing.T) {
	parser := &BPMNXMLParser{}
	src := `<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="http://www.omg.org/spec/BPMN/20100524/MODEL"
             xmlns:camunda="http://camunda.org/schema/1.0/bpmn"
             xmlns:metis="https://github.com/gsoultan/metis/schema/bpmn">
  <process id="p" isExecutable="true">
    <businessRuleTask id="route" camunda:decisionRef="expense-route" camunda:decisionRefVersion="3"/>
    <businessRuleTask id="live" camunda:decisionRef="expense-route"/>
    <serviceTask id="budget" metis:httpUrl="https://budget.example/api/check" metis:httpMethod="post"/>
    <serviceTask id="worker" camunda:type="external" camunda:topic="pay-out" metis:httpUrl="https://ignored.example"/>
    <userTask id="approve" camunda:decisionRef="not-for-a-user-task"/>
  </process>
</definitions>`

	def, err := parser.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	checkWhatItRuns(t, def, "after import")

	out, err := parser.Export(def)
	if err != nil {
		t.Fatalf("Export returned an error: %v", err)
	}
	for _, want := range []string{
		`camunda:decisionRef="expense-route"`,
		`camunda:decisionRefVersion="3"`,
		`metis:httpUrl="https://budget.example/api/check"`,
		`metis:httpMethod="POST"`,
		`xmlns:metis="https://github.com/gsoultan/metis/schema/bpmn"`,
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the exported file does not carry %s:\n%s", want, out)
		}
	}
	again, err := parser.Parse(strings.NewReader(string(out)))
	if err != nil {
		t.Fatalf("re-parsing the exported file failed: %v", err)
	}
	checkWhatItRuns(t, again, "after the round trip")
}

func checkWhatItRuns(t *testing.T, def *entities.ProcessDefinition, stage string) {
	t.Helper()
	route := nodeByID(t, def, "route")
	if got := route.GetStringProperty(entities.DecisionKeyProperty); got != "expense-route" {
		t.Errorf("%s: the business rule task decides with %q, want expense-route", stage, got)
	}
	if got := pinnedDecisionVersion(route.Properties["decision_version"]); got != 3 {
		t.Errorf("%s: the business rule task pins version %d, want 3", stage, got)
	}
	live := nodeByID(t, def, "live")
	if _, pinned := live.Properties["decision_version"]; pinned {
		t.Errorf("%s: a decision named without a version was pinned to %v", stage, live.Properties["decision_version"])
	}

	budget := nodeByID(t, def, "budget")
	if got := budget.HTTPURL(); got != "https://budget.example/api/check" {
		t.Errorf("%s: the service task calls %q, want the budget API", stage, got)
	}
	if got := budget.GetStringProperty("http_method"); got != "POST" {
		t.Errorf("%s: the service task calls with %q, want POST", stage, got)
	}

	// A step that waits for a worker is not also a call: the topic decides.
	worker := nodeByID(t, def, "worker")
	if worker.WorkerTopic() != "pay-out" || worker.HTTPURL() != "" {
		t.Errorf("%s: the worker step waits under %q and calls %q", stage, worker.WorkerTopic(), worker.HTTPURL())
	}
	approve := nodeByID(t, def, "approve")
	if got := approve.GetStringProperty(entities.DecisionKeyProperty); got != "" {
		t.Errorf("%s: a user task took decision %q from an attribute only a business rule task reads", stage, got)
	}
}

// A task may set only the variables its form declares, so a user task that
// lost its form on import could be completed with nothing: the approval's own
// answer was refused.
func TestATasksFormSurvivesTheRoundTrip(t *testing.T) {
	parser := &BPMNXMLParser{}
	src := `<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="http://www.omg.org/spec/BPMN/20100524/MODEL"
             xmlns:camunda="http://camunda.org/schema/1.0/bpmn">
  <process id="p" isExecutable="true">
    <userTask id="approve" camunda:candidateGroups="finance">
      <extensionElements>
        <camunda:formData>
          <camunda:formField id="decision" label="Decision" type="enum">
            <camunda:value id="approve" name="Approve"/>
            <camunda:value id="reject" name="Reject"/>
          </camunda:formField>
          <camunda:formField id="comment" type="string"/>
          <camunda:formField id="amount" label="Amount" type="long" defaultValue="0"/>
        </camunda:formData>
      </extensionElements>
    </userTask>
    <serviceTask id="call">
      <extensionElements>
        <camunda:formData><camunda:formField id="ignored" type="string"/></camunda:formData>
      </extensionElements>
    </serviceTask>
  </process>
</definitions>`

	def, err := parser.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	checkTheForm(t, def, "after import")

	out, err := parser.Export(def)
	if err != nil {
		t.Fatalf("Export returned an error: %v", err)
	}
	for _, want := range []string{
		`<camunda:formField id="decision" label="Decision" type="enum">`,
		`<camunda:value id="reject" name="Reject"></camunda:value>`,
		`<camunda:formField id="amount" label="Amount" type="long" defaultValue="0">`,
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the exported file does not carry %s:\n%s", want, out)
		}
	}
	again, err := parser.Parse(strings.NewReader(string(out)))
	if err != nil {
		t.Fatalf("re-parsing the exported file failed: %v", err)
	}
	checkTheForm(t, again, "after the round trip")
}

func checkTheForm(t *testing.T, def *entities.ProcessDefinition, stage string) {
	t.Helper()
	approve := nodeByID(t, def, "approve")
	declared := map[string]struct{}{}
	addFieldIDs(declared, inlineForm(approve.GetTextProperty(formDefinitionProperty)))
	for _, id := range []string{"decision", "comment", "amount"} {
		if _, ok := declared[id]; !ok {
			t.Errorf("%s: the user task's form does not declare %s; it declares %v", stage, id, declared)
		}
	}
	if !strings.Contains(approve.GetTextProperty(formDefinitionProperty), `"options":[{"label":"Approve","value":"approve"}`) {
		t.Errorf("%s: the decision field lost its choices: %s", stage, approve.GetTextProperty(formDefinitionProperty))
	}
	if got := nodeByID(t, def, "call").GetTextProperty(formDefinitionProperty); got != "" {
		t.Errorf("%s: a service task took a form it never shows anyone: %s", stage, got)
	}
}
