// Package fuzz holds the fuzz targets for everything that consumes untrusted
// input.
//
// AGENTS.md §0 states that process definitions are untrusted user input. Anyone
// who can reach the deploy endpoint chooses the bytes these parsers and
// evaluators see, so a panic in any of them is a denial of service against every
// tenant on the instance, not just the one who sent it.
//
// The bar these targets hold is narrow and deliberate: **never panic.** Rejecting
// malformed input with an error is correct; crashing the process is not. They do
// not assert that parsing succeeds, because for arbitrary bytes it should not.
package fuzz

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gsoultan/metis/internal/pkg/features"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic"
	"github.com/gsoultan/metis/server/domains/logic/feel"
	"github.com/gsoultan/metis/server/domains/services/impl"
)

// TestMain shortens the script budget for this package.
//
// The condition seeds include `while(true){}` and unbounded recursion, which are
// meant to be stopped by the sandbox interrupt. At the 5s production default
// each one costs 5s, and the seed corpus alone took 51s — long enough that it
// would push people to skip the gate.
//
// What these targets prove is that a hostile condition is *interrupted rather
// than crashing*, and that holds at any budget. That the default is 5s, and that
// the interrupt fires at all, is covered where it belongs, in
// server/domains/logic/sandbox_test.go.
func TestMain(m *testing.M) {
	if err := os.Setenv("GOBPM_SCRIPT_TIMEOUT", "200ms"); err != nil {
		panic("could not shorten the script budget for fuzzing: " + err.Error())
	}
	os.Exit(m.Run())
}

// FuzzBPMNXMLParse fuzzes the deploy path: bytes uploaded as a process
// definition, straight into the XML parser.
func FuzzBPMNXMLParse(f *testing.F) {
	seeds := []string{
		"",
		"<?xml version=\"1.0\"?>",
		`<definitions><process id="p"><startEvent id="s"/><endEvent id="e"/></process></definitions>`,
		`<definitions><process id="p"><startEvent id="s"/><sequenceFlow id="f" sourceRef="s" targetRef="e"/><endEvent id="e"/></process></definitions>`,
		`<definitions><process id="p"><exclusiveGateway id="g" default="f1"/></process></definitions>`,
		`<definitions><process id="p"><userTask id="u" name="Approve"><extensionElements/></userTask></process></definitions>`,
		`<definitions><process id="p"><subProcess id="sp" triggeredByEvent="true"><startEvent id="s2"/></subProcess></process></definitions>`,
		`<definitions><process id="p"><boundaryEvent id="b" attachedToRef="u" cancelActivity="false"><timerEventDefinition><timeDuration>PT5M</timeDuration></timerEventDefinition></boundaryEvent></process></definitions>`,
		// Deeply nested elements: the classic stack-exhaustion shape for a
		// recursive descent over XML.
		strings.Repeat("<a>", 200) + strings.Repeat("</a>", 200),
		// An entity reference, in case expansion is ever enabled.
		`<!DOCTYPE d [<!ENTITY e "x">]><definitions>&e;</definitions>`,
	}
	for _, seed := range seeds {
		f.Add([]byte(seed))
	}

	parser := &impl.BPMNXMLParser{}

	f.Fuzz(func(t *testing.T, data []byte) {
		// An error is a fine outcome. A panic is not: these bytes arrive over
		// the deploy endpoint.
		def, err := parser.Parse(strings.NewReader(string(data)))
		if err != nil {
			return
		}
		if def == nil {
			t.Fatal("Parse returned no definition and no error, so a caller would nil-dereference")
		}

		// Whatever came out must survive a round trip. Export walks the same
		// structure the engine walks, so a shape that crashes it is a shape
		// that crashes execution.
		if _, err := parser.Export(def); err != nil {
			return
		}
	})
}

// FuzzBPMNXMLRoundTrip pushes parsed definitions back through Export and Parse.
// A definition that survives the first parse is one the engine will execute, so
// the second pass is where a shape that parses but cannot be serialised shows
// up.
func FuzzBPMNXMLRoundTrip(f *testing.F) {
	f.Add(`<definitions><process id="p" name="n"><startEvent id="s"/><userTask id="u"/><endEvent id="e"/><sequenceFlow id="f1" sourceRef="s" targetRef="u"/><sequenceFlow id="f2" sourceRef="u" targetRef="e"/></process></definitions>`)
	f.Add(`<definitions><process id="p"><callActivity id="c" calledElement="other"/></process></definitions>`)

	parser := &impl.BPMNXMLParser{}

	f.Fuzz(func(t *testing.T, xml string) {
		def, err := parser.Parse(strings.NewReader(xml))
		if err != nil || def == nil {
			return
		}

		exported, err := parser.Export(def)
		if err != nil {
			return
		}

		again, err := parser.Parse(strings.NewReader(string(exported)))
		if err != nil {
			t.Fatalf("Export produced XML this parser rejects: %v", err)
		}
		if again == nil {
			t.Fatal("re-parsing exported XML returned no definition and no error")
		}
	})
}

// FuzzConditionEvaluator fuzzes gateway conditions. A condition string comes
// from a deployed definition, so it is untrusted, and the chain ends in a
// JavaScript evaluator — the highest-risk consumer of that string in the
// codebase.
func FuzzConditionEvaluator(f *testing.F) {
	seeds := []string{
		"", "-", "true", "false",
		"approved", "status=approved", "amount>100",
		"js:vars.amount > 100",
		"js:while(true){}",            // must be stopped by the sandbox, not hang
		"js:(function f(){f()})()",    // unbounded recursion
		"js:new Array(1e9).join('x')", // allocation bomb
		"js:this.constructor.constructor('return 1')()",
		strings.Repeat("(", 500),
		strings.Repeat("a=b&&", 200) + "c",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	chain := logic.GetConditionEvaluatorChain()

	// The sandbox is this fuzzer's highest-value target, and the
	// javascript-conditions flag ships off. Force it on here: with the flag
	// refusing `js:` at the door, the while(true) and allocation-bomb seeds
	// would stop exercising the interrupt they exist to regression-test.
	defer features.OverrideForTest(features.JavaScriptConditions, true)()

	f.Fuzz(func(t *testing.T, condition string) {
		// Bound the input: the fuzzer will otherwise spend its budget on
		// megabyte strings that only prove the sandbox's size limit works.
		if len(condition) > 4096 {
			return
		}

		vars := map[string]any{
			"amount":   100,
			"status":   "approved",
			"approved": true,
			"nested":   map[string]any{"value": 1},
		}

		// The contract is a bool and no panic. A condition that cannot be
		// understood is false, not a crash.
		_ = chain.Evaluate(condition, vars)
	})
}

// FuzzFEELEvaluator fuzzes DMN decision-table cells. Each cell is authored
// content from a deployed decision, evaluated once per rule per evaluation.
func FuzzFEELEvaluator(f *testing.F) {
	seeds := []string{
		"", "-", `"GOLD"`, `"GOLD","SILVER"`,
		"< 100", ">= 0", "!= 3",
		"[1..10]", "(1..10]", "[1..10)",
		"not(\"GOLD\")", "not(1..10)",
		"[1..", "..]", "[..]", "[,..,]",
		strings.Repeat("not(", 100) + "1" + strings.Repeat(")", 100),
		strings.Repeat(",", 1000),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	evaluator := impl.NewFEELEvaluator()

	f.Fuzz(func(t *testing.T, expression string) {
		if len(expression) > 4096 {
			return
		}

		for _, input := range []any{nil, 0, 1.5, "GOLD", true, []any{1, 2}} {
			vars := map[string]any{"_input": input}
			// Errors are expected for nonsense. Panics are not.
			_, _ = evaluator.EvaluateBool(t.Context(), expression, vars)
			_, _ = evaluator.Evaluate(t.Context(), expression, vars)
		}
	})
}

// FuzzNodeTypeMapping fuzzes the element-name to NodeType mapping that the
// parser applies to attacker-chosen element names.
func FuzzNodeTypeMapping(f *testing.F) {
	for _, seed := range []string{"userTask", "serviceTask", "", "UserTask", "task\x00"} {
		f.Add(seed)
	}

	parser := &impl.BPMNXMLParser{}

	f.Fuzz(func(t *testing.T, element string) {
		if len(element) > 1024 {
			return
		}
		xml := `<definitions><process id="p"><` + element + ` id="n"/></process></definitions>`
		def, err := parser.Parse(strings.NewReader(xml))
		if err != nil || def == nil {
			return
		}
		for _, node := range def.Nodes {
			if node == nil {
				t.Fatal("parser produced a nil node, which the engine will dereference")
			}
			if node.Type == entities.NodeType("") && node.ID != "" {
				// Not a failure — unknown elements legitimately have no type —
				// but the node must still be safe to walk.
				continue
			}
		}
	})
}

// FuzzFEELParser fuzzes the FEEL engine that replaced the string matcher.
//
// Gateway conditions and decision cells are authored in deployed definitions,
// so every byte here is attacker-chosen. The bar is the same as the other
// targets — never panic — plus one this language is uniquely able to promise:
// it must always terminate, because it has no loop, no recursion the author
// controls, and no allocation primitive. That is what makes it a safe
// replacement for the JavaScript path, where one expression held a worker for
// 37 seconds.
func FuzzFEELParser(f *testing.F) {
	seeds := []string{
		"", "-", "1", `"x"`, "true", "null",
		"1 + 2 * 3", "(1 + 2) * 3", "2 ** 10",
		"amount > 500 and status = \"GOLD\"",
		"not(amount > 5000)",
		"5 in [1..10]", "[1..", "..]", "[,..,]",
		`"GOLD","SILVER"`, "< 10, > 100",
		"applicant.address.city", "items[1]", "items[-1]",
		`date("2026-03-15") + duration("P1D")`,
		`duration("P1Y2MT3H")`, `duration("")`, "duration(",
		"sum(items.price)", "list contains([1,2], 1)",
		"if 1 > 0 then \"a\" else \"b\"",
		"{a: 1, b: {c: 2}}",
		strings.Repeat("(", 100) + "1" + strings.Repeat(")", 100),
		strings.Repeat("not(", 50) + "true" + strings.Repeat(")", 50),
		strings.Repeat("1+", 1000) + "1",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	vars := map[string]any{
		"amount": 900.0,
		"status": "GOLD",
		"items":  []any{map[string]any{"price": 10.0}},
	}

	f.Fuzz(func(t *testing.T, expr string) {
		if len(expr) > 4096 {
			return
		}

		// Both grammars: an expression and a decision-table cell parse
		// differently, and each is reachable from a deployed definition.
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = feel.Evaluate(expr, vars)
			_, _ = feel.EvaluateUnaryTests(expr, 42.0, vars)
		}()

		select {
		case <-done:
		case <-time.After(10 * time.Second):
			// Not a slow machine: this language cannot legitimately take ten
			// seconds on four kilobytes, so a timeout means a loop exists that
			// should not.
			t.Fatalf("evaluation did not terminate for %q", expr)
		}
	})
}
