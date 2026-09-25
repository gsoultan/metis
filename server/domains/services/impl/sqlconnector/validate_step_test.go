package sqlconnector

import "testing"

func TestValidateStep(t *testing.T) {
	for name, tc := range map[string]struct {
		statement, resultVariable string
		ok                        bool
	}{
		"a read": {"SELECT tier FROM customers WHERE id = :id", "customer", true},
		// [drop zone] is one quoted name on SQL Server and two words, one of
		// them DROP, anywhere else. A deploy does not know which server the step
		// will run against, so a query that is a read on one of them passes.
		"a read only SQL Server can read": {"SELECT [drop zone] FROM depots", "depot", true},
		"a write":                         {"DELETE FROM customers", "customer", false},
		"no answer variable":              {"SELECT 1 AS one", "", false},
		"an answer variable with a space": {"SELECT 1 AS one", "the answer", false},
	} {
		if err := ValidateStep(tc.statement, tc.resultVariable); (err == nil) != tc.ok {
			t.Errorf("%s: got %v", name, err)
		}
	}
}
