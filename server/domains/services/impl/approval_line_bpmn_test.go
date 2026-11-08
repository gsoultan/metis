package impl

import (
	"bytes"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// A line is written as BPMN the importer reads, and read back as the same line.
func TestAnApprovalLineReadsBackFromTheProcessItIs(t *testing.T) {
	line := entities.ApprovalLine{Key: "expense", Name: "Expense <approval>", AllowRevise: true, Fields: []entities.ApprovalField{{ID: "amount", Label: "Amount", Type: "number"}, {ID: "purpose"}}, Steps: []entities.ApprovalStep{
		{ID: "supervisor", Name: "Supervisor & co", Groups: []string{"supervisors"}},
		{ID: "unit-head", Name: "Unit head", Users: []string{"sari", "dewi"}, Groups: []string{"unit-heads"}},
	}}
	xml, err := approvalLineBPMN(line)
	if err != nil {
		t.Fatal(err)
	}
	def, err := (&BPMNXMLParser{}).Parse(bytes.NewReader(xml))
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, xml)
	}
	got, ok := approvalLineOf(def)
	if !ok {
		t.Fatalf("not read back as a line:\n%s", xml)
	}
	if got.Key != "expense" || got.Name != "Expense <approval>" || !got.AllowRevise || len(got.Steps) != 2 ||
		got.Steps[0].Name != "Supervisor & co" || got.Steps[1].ID != "unit-head" || len(got.Steps[1].Users) != 2 || got.Steps[1].Groups[0] != "unit-heads" ||
		len(got.Fields) != 2 || got.Fields[0] != (entities.ApprovalField{ID: "amount", Label: "Amount", Type: "number"}) || got.Fields[1] != (entities.ApprovalField{ID: "purpose", Type: "text"}) {
		t.Errorf("read back %+v", got)
	}
}
