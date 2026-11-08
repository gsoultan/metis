package impl

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"slices"
	"strings"

	"github.com/gsoultan/metis/server/domains/entities"
)

// approvalLineMarker is the documentation an approval line's start event
// carries. (The start event's, not the process's: a definition keeps each
// node's documentation and not its own.) It is how a definition is known to be one, so that its
// steps can be read back as a line and Decide accepts its tasks.
const approvalLineMarker = "metis:approval-line"

// Node ids an approval line's definition uses. A step's own id is prefixed so
// that a step can be called anything a line allows and still be a valid BPMN
// id that no other node of the definition uses.
const (
	approvalStepPrefix    = "step-"
	approvalGatePrefix    = "decided-"
	approvalReviseNode    = "revise"
	approvalRevisedGate   = "revised"
	approvalSubmittedNode = "submitted"
)

func approvalStepNode(stepID string) string { return approvalStepPrefix + stepID }

// approvalEnd is the end event a request reaches with each outcome.
var approvalEnd = map[string]string{
	entities.ApprovalApproved:  "approved",
	entities.ApprovalRejected:  "rejected",
	entities.ApprovalWithdrawn: "withdrawn",
}

// approvalLineBPMN writes the process an approval line is.
//
//	submitted → step 1 → decided? → step 2 → … → approved
//	                       ├ reject → rejected
//	                       └ revise → revise (the submitter) → revised?
//	                                     ├ resubmit → step 1
//	                                     └ withdraw → withdrawn
//
// Every gateway reads approvalDecision, which Decide sets with the task it
// completes; the trail of who decided what is kept beside it. The line is
// validated first, so every name written here is one Validate accepted, and
// the text that is free — names — is escaped.
func approvalLineBPMN(line entities.ApprovalLine) ([]byte, error) {
	if err := line.Validate(); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	esc := func(s string) string {
		var out bytes.Buffer
		// Writing to a buffer cannot fail; an empty name is safe if it did.
		if err := xml.EscapeText(&out, []byte(s)); err != nil {
			return ""
		}
		return out.String()
	}
	flow := func(id, from, to, condition string) {
		if condition == "" {
			fmt.Fprintf(&b, "    <sequenceFlow id=%q sourceRef=%q targetRef=%q/>\n", id, from, to)
			return
		}
		fmt.Fprintf(&b, "    <sequenceFlow id=%q sourceRef=%q targetRef=%q>\n      <conditionExpression>%s = %s</conditionExpression>\n    </sequenceFlow>\n",
			id, from, to, entities.ApprovalVarDecision, condition)
	}

	b.WriteString(xml.Header)
	fmt.Fprintf(&b, `<definitions xmlns="http://www.omg.org/spec/BPMN/20100524/MODEL"
             xmlns:camunda="http://camunda.org/schema/1.0/bpmn"
             id="approval-line-%s" targetNamespace="https://github.com/gsoultan/metis/approval-lines">
  <process id=%q name="%s" isExecutable="true">
    <startEvent id=%q name="Submitted">
      <documentation>%s</documentation>
    </startEvent>
`, line.Key, line.Key, esc(line.Name), approvalSubmittedNode, approvalLineMarker)

	for _, s := range line.Steps {
		attrs := ""
		if users := joinNames(s.Users); users != "" {
			attrs += fmt.Sprintf(" camunda:candidateUsers=%q", users)
		}
		if groups := joinNames(s.Groups); groups != "" {
			attrs += fmt.Sprintf(" camunda:candidateGroups=%q", groups)
		}
		fmt.Fprintf(&b, "    <userTask id=%q name=\"%s\"%s>\n", approvalStepNode(s.ID), esc(s.Name), attrs)
		decisions := []string{entities.ApprovalApprove, entities.ApprovalReject}
		if line.AllowRevise {
			decisions = append(decisions, entities.ApprovalRevise)
		}
		writeApprovalForm(&b, decisions, nil, esc)
		b.WriteString("    </userTask>\n")
		fmt.Fprintf(&b, "    <exclusiveGateway id=%q name=\"%s decided\"/>\n", approvalGatePrefix+s.ID, esc(s.Name))
	}
	if line.AllowRevise {
		// The submitter revises: their username is a variable Submit sets,
		// read when the task is made.
		fmt.Fprintf(&b, "    <userTask id=%q name=\"Revise the request\" camunda:assignee=\"${%s}\">\n", approvalReviseNode, entities.ApprovalVarSubmittedBy)
		writeApprovalForm(&b, []string{entities.ApprovalResubmit, entities.ApprovalWithdraw}, line.Fields, esc)
		b.WriteString("    </userTask>\n")
		fmt.Fprintf(&b, "    <exclusiveGateway id=%q name=\"Resubmitted?\"/>\n", approvalRevisedGate)
		fmt.Fprintf(&b, "    <endEvent id=%q name=\"Withdrawn\"/>\n", approvalEnd[entities.ApprovalWithdrawn])
	}
	fmt.Fprintf(&b, "    <endEvent id=%q name=\"Approved\"/>\n", approvalEnd[entities.ApprovalApproved])
	fmt.Fprintf(&b, "    <endEvent id=%q name=\"Rejected\"/>\n", approvalEnd[entities.ApprovalRejected])

	first := approvalStepNode(line.Steps[0].ID)
	flow("f-submitted", approvalSubmittedNode, first, "")
	for i, s := range line.Steps {
		gate := approvalGatePrefix + s.ID
		next := approvalEnd[entities.ApprovalApproved]
		if i+1 < len(line.Steps) {
			next = approvalStepNode(line.Steps[i+1].ID)
		}
		flow("f-"+s.ID, approvalStepNode(s.ID), gate, "")
		flow("f-"+s.ID+"-approve", gate, next, entities.ApprovalApprove)
		flow("f-"+s.ID+"-reject", gate, approvalEnd[entities.ApprovalRejected], entities.ApprovalReject)
		if line.AllowRevise {
			flow("f-"+s.ID+"-revise", gate, approvalReviseNode, entities.ApprovalRevise)
		}
	}
	if line.AllowRevise {
		flow("f-revise", approvalReviseNode, approvalRevisedGate, "")
		flow("f-resubmit", approvalRevisedGate, first, entities.ApprovalResubmit)
		flow("f-withdraw", approvalRevisedGate, approvalEnd[entities.ApprovalWithdrawn], entities.ApprovalWithdraw)
	}
	b.WriteString("  </process>\n</definitions>\n")
	return b.Bytes(), nil
}

// writeApprovalForm writes the form of a line's task: the decision, the
// trail Decide keeps (a task sets only what its form declares) and, on the
// revise step, the round and the fields the submitter may correct.
func writeApprovalForm(b *bytes.Buffer, decisions []string, fields []entities.ApprovalField, esc func(string) string) {
	b.WriteString("      <extensionElements>\n        <camunda:formData>\n")
	fmt.Fprintf(b, "          <camunda:formField id=%q label=\"Decision\" type=\"enum\">\n", entities.ApprovalVarDecision)
	for _, d := range decisions {
		fmt.Fprintf(b, "            <camunda:value id=%q name=%q/>\n", d, approvalDecisionLabel[d])
	}
	b.WriteString("          </camunda:formField>\n")
	fmt.Fprintf(b, "          <camunda:formField id=%q label=\"Decisions so far\" type=\"string\"/>\n", entities.ApprovalVarTrail)
	if fields != nil || slices.Contains(decisions, entities.ApprovalResubmit) {
		fmt.Fprintf(b, "          <camunda:formField id=%q label=\"Round\" type=\"long\"/>\n", entities.ApprovalVarRound)
	}
	for _, f := range fields {
		label := f.Label
		if strings.TrimSpace(label) == "" {
			label = f.ID
		}
		fmt.Fprintf(b, "          <camunda:formField id=%q label=\"%s\" type=%q/>\n", f.ID, esc(label), approvalCamundaType[f.Type])
	}
	b.WriteString("        </camunda:formData>\n      </extensionElements>\n")
}

var approvalDecisionLabel = map[string]string{
	entities.ApprovalApprove:  "Approve",
	entities.ApprovalReject:   "Reject",
	entities.ApprovalRevise:   "Send back to revise",
	entities.ApprovalResubmit: "Resubmit",
	entities.ApprovalWithdraw: "Withdraw",
}

var approvalCamundaType = map[string]string{"": "string", "text": "string", "number": "long", "boolean": "boolean", "date": "date"}

// approvalFieldsOf reads the fields back from the revise step's form.
func approvalFieldsOf(n *entities.Node) []entities.ApprovalField {
	raw := entities.PropertyText(n.Properties, formDefinitionProperty)
	var form []map[string]any
	if json.Unmarshal([]byte(raw), &form) != nil {
		return nil
	}
	types := map[string]string{"text": "text", "number": "number", "boolean": "boolean", "date": "date"}
	var out []entities.ApprovalField
	for _, f := range form {
		id := textOf(f, "id")
		switch id {
		case "", entities.ApprovalVarDecision, entities.ApprovalVarTrail, entities.ApprovalVarRound:
			continue
		}
		field := entities.ApprovalField{ID: id, Type: types[textOf(f, "type")]}
		if label := textOf(f, "label"); label != id {
			field.Label = label
		}
		out = append(out, field)
	}
	return out
}

// joinNames is a candidate list as the attribute writes it.
func joinNames(names []string) string {
	var kept []string
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			kept = append(kept, n)
		}
	}
	return strings.Join(kept, ",")
}

// approvalLineOf reads a line back from the definition it was deployed as,
// or reports that the definition is not one.
func approvalLineOf(def *entities.ProcessDefinition) (entities.ApprovalLine, bool) {
	if def == nil {
		return entities.ApprovalLine{}, false
	}
	marked := false
	for _, n := range def.Nodes {
		if n != nil && n.ID == approvalSubmittedNode && strings.TrimSpace(n.Documentation) == approvalLineMarker {
			marked = true
		}
	}
	if !marked {
		return entities.ApprovalLine{}, false
	}
	line := entities.ApprovalLine{Key: def.Key, Name: def.Name, Version: def.Version, DefinitionID: def.ID}
	next := map[string]string{}
	for _, f := range def.Flows {
		if f == nil {
			continue
		}
		if f.Condition == "" {
			if _, ok := next[f.SourceRef]; !ok {
				next[f.SourceRef] = f.TargetRef
			}
			continue
		}
		if strings.HasSuffix(strings.TrimSpace(f.Condition), " "+entities.ApprovalApprove) {
			next[f.SourceRef] = f.TargetRef
		}
	}
	nodes := map[string]*entities.Node{}
	for _, n := range def.Nodes {
		if n != nil {
			nodes[n.ID] = n
			if n.ID == approvalReviseNode {
				line.AllowRevise = true
				line.Fields = approvalFieldsOf(n)
			}
		}
	}
	// Walk submitted → step → its gate → the step its approval leads to.
	at := next[approvalSubmittedNode]
	for range entities.MaxApprovalSteps + 1 {
		stepID, isStep := strings.CutPrefix(at, approvalStepPrefix)
		n := nodes[at]
		if !isStep || n == nil {
			break
		}
		step := entities.ApprovalStep{ID: stepID, Name: n.Name}
		for _, u := range n.CandidateUsers {
			if u != nil {
				step.Users = append(step.Users, u.Username)
			}
		}
		for _, g := range n.CandidateGroups {
			if g != nil {
				step.Groups = append(step.Groups, approvalGroupName(g))
			}
		}
		line.Steps = append(line.Steps, step)
		at = next[next[at]]
	}
	if len(line.Steps) == 0 {
		return entities.ApprovalLine{}, false
	}
	return line, true
}

// approvalGroupName is the name a step offers its task to: the group's name,
// or its id when it names none.
func approvalGroupName(g *entities.Group) string {
	if name := strings.TrimSpace(g.Name); name != "" {
		return name
	}
	return g.ID.String()
}
