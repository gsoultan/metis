package impl

import (
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// TestParseBPMN_SubProcessKeepsIDAndName guards a regression in which
// bpmnProcessNode embedded both bpmnNode and bpmnProcess, each declaring
// `id,attr` and `name,attr`. encoding/xml drops fields that are ambiguous at
// equal depth, so every imported sub-process parsed with an empty ID and Name,
// which in turn produced unreachable child nodes in the definition.
func TestParseBPMN_SubProcessKeepsIDAndName(t *testing.T) {
	const xml = `<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="http://www.omg.org/spec/BPMN/20100524/MODEL" id="defs">
  <process id="p1" name="Parent Process">
    <startEvent id="start" name="Start"/>
    <subProcess id="sub1" name="Review Sub-Process">
      <startEvent id="sub-start" name="Sub Start"/>
      <userTask id="sub-task" name="Sub Task"/>
      <endEvent id="sub-end" name="Sub End"/>
      <sequenceFlow id="sf1" sourceRef="sub-start" targetRef="sub-task"/>
      <sequenceFlow id="sf2" sourceRef="sub-task" targetRef="sub-end"/>
    </subProcess>
    <endEvent id="end" name="End"/>
  </process>
</definitions>`

	def, err := (&BPMNXMLParser{}).Parse(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}

	var sub *entities.Node
	for _, n := range def.Nodes {
		if n != nil && n.Type == entities.SubProcess {
			sub = n
			break
		}
	}
	if sub == nil {
		t.Fatal("no sub-process node was parsed from the definition")
	}

	if sub.ID != "sub1" {
		t.Errorf("sub-process ID = %q, want %q", sub.ID, "sub1")
	}
	if sub.Name != "Review Sub-Process" {
		t.Errorf("sub-process Name = %q, want %q", sub.Name, "Review Sub-Process")
	}
	if len(sub.Nodes) != 3 {
		t.Errorf("sub-process child node count = %d, want 3", len(sub.Nodes))
	}
	if len(sub.Flows) != 2 {
		t.Errorf("sub-process flow count = %d, want 2", len(sub.Flows))
	}
}
