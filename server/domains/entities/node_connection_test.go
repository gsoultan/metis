package entities

import "testing"

func TestAStepNamingItsConnectionByNameRunsAConnector(t *testing.T) {
	n := &Node{Properties: map[string]any{ConnectionProperty: "payments-api"}}
	if !n.NamesConnector() || n.Implementation() != "connector" {
		t.Fatalf("a step naming its connection by name is %q", n.Implementation())
	}
	if (&Node{}).NamesConnector() {
		t.Fatal("a step naming nothing names a connector")
	}
}
