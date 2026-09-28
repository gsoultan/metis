package impl

import (
	"fmt"
	"testing"
)

// While undeclared variables are allowed, each step completed with them is
// named once. What is remembered comes from user-authored definitions, so it
// is bounded, and a step pushed out is named again rather than never.
func TestEachStepIsNamedOnceAndWhatIsRememberedIsBounded(t *testing.T) {
	var reports undeclaredVariableReports
	if !reports.first("refund/approve") {
		t.Fatal("a step never named before was not named")
	}
	if reports.first("refund/approve") {
		t.Fatal("a step already named was named again")
	}

	for i := range undeclaredReportCapacity {
		reports.first(fmt.Sprintf("process-%d/step", i))
	}
	if held := reports.seen.Len(); held != undeclaredReportCapacity {
		t.Fatalf("remembering %d steps holds %d; want at most %d", undeclaredReportCapacity+1, held, undeclaredReportCapacity)
	}
	if !reports.first("refund/approve") {
		t.Fatal("the step seen least recently was pushed out and is not named again")
	}
}
