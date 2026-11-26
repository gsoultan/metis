package grule_test

import (
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic/grule"
)

func translate(t *testing.T, source string) entities.GruleImport {
	t.Helper()
	rules, err := grule.ParseGRL("rules.grl", source)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := grule.Translate(rules, "test", "")
	if err != nil {
		t.Fatal(err)
	}
	return imported
}

func outcome(imported entities.GruleImport, name string) entities.GruleRuleOutcome {
	for _, r := range imported.Rules {
		if r.Name == name {
			return r
		}
	}
	return entities.GruleRuleOutcome{}
}

// TestWhatCannotBeTranslatedIsReportedNotGuessed: each rule a decision cannot
// answer exactly is left out, with the reason, and so is every rule whose
// answer depended on it.
func TestWhatCannotBeTranslatedIsReportedNotGuessed(t *testing.T) {
	cases := []struct {
		name, source, rule, reason string
	}{
		{"a rule that never stops", `rule A { when X.N > 1 then X.M = 2; }`, "A", "cycle limit"},
		{"a loop of two rules", `
rule A { when X.B == 1 then X.A = 1; Retract("A"); }
rule B { when X.A == 1 then X.B = 1; Retract("B"); }`, "A", "loop"},
		{"a function metis does not have", `rule A { when X.T.Trim() == "a" then X.M = 2; Retract("A"); }`, "A", ".Trim()"},
		{"another rule's retraction", `rule A { when X.N > 1 then X.M = 2; Retract("B"); }`, "A", "retracts another rule"},
		{"an engine call", `rule A { when X.N > 1 then X.M = 2; Complete(); }`, "A", "Complete()"},
		{"a method with an effect", `rule A { when X.N > 1 then X.Apply(); X.M = 1; Retract("A"); }`, "A", "Apply()"},
		{"a list element", `rule A { when X.L[0] == 1 then X.M = 2; Retract("A"); }`, "A", "element of a list"},
		{"a bitwise operator", `rule A { when (X.N & 1) == 1 then X.M = 2; Retract("A"); }`, "A", "no FEEL equivalent"},
		{"a reader of a rule left out", `
rule A { when X.N > 1 then X.M = Unknown(1); Retract("A"); }
rule B { when X.M == 2 then X.Q = 1; Retract("B"); }`, "B", "could not be imported"},
		{"guarded rules setting different fields", `
rule A { when X.R == "" then X.R = "a"; X.S = 1; }
rule B { when X.R == "" then X.R = "b"; }`, "B", "same fields"},
		{"guarded and retracting rules mixed", `
rule A { when X.R == "" then X.R = "a"; }
rule B { when X.N > 1 then X.R = "b"; Retract("B"); }`, "A", "mix"},
		{"a reader of a field set beside it", `
rule A { when X.N > 1 then X.R = 1; Retract("A"); }
rule B { when X.R > 0 then X.R = 2; Retract("B"); }`, "B", "same pass"},
		{"a condition that is never true", `rule A { when false then X.M = 2; Retract("A"); }`, "A", "never be true"},
		{"a rule that sets nothing", `rule A { when X.N > 1 then Retract("A"); }`, "A", "sets no field"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := outcome(translate(t, tc.source), tc.rule)
			if got.Decision != "" || !strings.Contains(got.Reason, tc.reason) {
				t.Errorf("%s: decision %q, reason %q; want it left out because of %q", tc.rule, got.Decision, got.Reason, tc.reason)
			}
		})
	}
}

func TestAnImportedRuleSaysWhatItShares(t *testing.T) {
	imported := translate(t, `
rule A salience 2 { when X.N > 1 then X.R = Unknown(); Retract("A"); }
rule B salience 1 { when X.N > 5 then X.R = 2; Retract("B"); }
rule C salience 1 { when X.N > 9 then X.Q = X.N / 2; Retract("C"); }`)
	b := outcome(imported, "B")
	if b.Decision == "" || len(b.Notes) == 0 || !strings.Contains(b.Notes[0], "A also sets X.R") {
		t.Errorf("B: %+v", b)
	}
	if c := outcome(imported, "C"); len(c.Notes) == 0 || !strings.Contains(c.Notes[0], "remainder") {
		t.Errorf("C: %+v", c)
	}
}

func TestEqualSalienceIsWarnedAbout(t *testing.T) {
	imported := translate(t, `
rule A { when X.N > 1 then X.R = 1; Retract("A"); }
rule B { when X.N > 2 then X.R = 2; Retract("B"); }`)
	if len(imported.Warnings) != 1 || !strings.Contains(imported.Warnings[0], "A, B (salience 0)") {
		t.Errorf("warnings = %v", imported.Warnings)
	}
}

// TestTextFromARuleStaysText: a string in a rule is written into FEEL quoted,
// so whatever it holds stays a string and never becomes part of the
// expression around it.
func TestTextFromARuleStaysText(t *testing.T) {
	imported := translate(t, `rule A { when X.Name == "a\" or true or \"" then X.R = "x\" + Y + \"" + X.Name; Retract("A"); }`)
	table := imported.Decisions[0]
	if cell := table.Rules[0].Inputs[0]; cell != `"a\" or true or \""` {
		t.Errorf("cell = %s", cell)
	}
	if out := table.Rules[0].Outputs[0]; out != `("x\" + Y + \"" + X_Name)` {
		t.Errorf("output = %v", out)
	}
}

func TestTheProcessConsultsTheTablesInOrder(t *testing.T) {
	imported := translate(t, ruleSetChain)
	var keys []string
	for _, node := range imported.Process.Nodes {
		if node.Type == entities.BusinessRuleTask {
			keys = append(keys, node.Properties["decision_key"].(string))
		}
	}
	want := []string{"test-customer-tier", "test-order-discount"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Errorf("steps = %v, want %v", keys, want)
	}
	if len(imported.Process.Flows) != 3 {
		t.Errorf("flows = %d", len(imported.Process.Flows))
	}
	mapping := imported.Process.Nodes[2].Properties["input_mapping"].(map[string]any)
	if mapping["Customer_Tier"] != "if Customer_Tier != null then Customer_Tier else Customer.Tier" {
		t.Errorf("the later step reads the tier as %v", mapping["Customer_Tier"])
	}
}

const ruleSetChain = `
rule Vip salience 50 { when Customer.Spend > 1000 && Customer.Tier != "vip" then Customer.Tier = "vip"; }
rule VipDiscount salience 40 { when Customer.Tier == "vip" then Order.Discount = 10; Retract("VipDiscount"); }`

func TestTranslateRefusesABadKeyAndDuplicateNames(t *testing.T) {
	rules, _ := grule.ParseGRL("r.grl", `rule A { when true then X.R = 1; Retract("A"); } rule A { when true then X.R = 1; Retract("A"); }`)
	if _, err := grule.Translate(rules[:1], "Bad Key", ""); err == nil {
		t.Error("a key with spaces was accepted")
	}
	if _, err := grule.Translate(rules, "ok", ""); err == nil || !strings.Contains(err.Error(), "two rules are named A") {
		t.Errorf("duplicate names: %v", err)
	}
}
