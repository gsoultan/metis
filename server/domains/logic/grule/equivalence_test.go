package grule_test

import (
	"maps"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic/grule"
	"github.com/gsoultan/metis/server/domains/logic/mapping"
	"github.com/gsoultan/metis/server/domains/services/impl"
)

// ruleSets are Grule rule sets of the shapes the importer translates. Each is
// run through the model of Grule's engine and through the imported process,
// on many facts, and the two must agree on every field the rules set.
var ruleSets = map[string]string{
	"the first matching rule sets the field": `
rule Gold salience 30 { when Customer.Tier == "gold" && Order.Discount == 0 then Order.Discount = 20; }
rule Silver salience 20 { when Customer.Tier == "silver" && Order.Discount == 0 then Order.Discount = 10; }
rule Big salience 10 { when Order.Total > 500 && Order.Discount == 0 then Order.Discount = 5; }`,

	"a guard on a field set to a computed value": `
rule Gold salience 30 { when Customer.Tier == "gold" && Order.Discount == 0 then Order.Discount = Order.Total * 0.2 + 1; }
rule Spend salience 20 { when Customer.Spend >= 1000 && Order.Discount == 0 then Order.Discount = Customer.Spend / 100; }`,

	"every matching rule fires and the last one sets the field": `
rule Base salience 30 { when Order.Total >= 0 then Order.Shipping = 15; Order.Note = "standard"; Retract("Base"); }
rule Free salience 20 { when Order.Total > 500 then Order.Shipping = 0; Retract("Free"); }
rule Remote salience 10 { when Customer.Region == "remote" then Order.Shipping = Order.Total * 0.05 + 20; Retract("Remote"); }`,

	"a rule reads what another rule sets": `
rule Vip salience 50 { when Customer.Spend > 1000 && Customer.Tier != "vip" then Customer.Tier = "vip"; }
rule VipDiscount salience 40 { when Customer.Tier == "vip" then Order.Discount = Order.Total * 0.1; Retract("VipDiscount"); }
rule Pay salience 10 { when Order.Total > 0 then Order.Due = Order.Total - Order.Discount; Retract("Pay"); }`,

	"strings, lists and other conditions": `
rule Flag salience 9 { when Customer.Name.Contains("Ltd") || Customer.Region.In("north", "east") then Order.Review = true; Order.Reason = "company or region " + Customer.Region.ToUpper(); Retract("Flag"); }
rule Large salience 5 { when Order.Total > Customer.Spend && !Customer.Trusted then Order.Hold = true; Retract("Large"); }`,
}

func TestImportedRulesDecideAsGruleDoes(t *testing.T) {
	for name, source := range ruleSets {
		t.Run(name, func(t *testing.T) {
			rules, err := grule.ParseGRL("rules.grl", source)
			if err != nil {
				t.Fatal(err)
			}
			imported, err := grule.Translate(rules, "test", "Test")
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range imported.Rules {
				if r.Decision == "" {
					t.Fatalf("%s was not imported: %s", r.Name, r.Reason)
				}
			}
			random := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // reproducible test data, not a secret
			for range 300 {
				facts := randomFacts(random)
				compare(t, rules, imported, facts)
			}
		})
	}
}

func randomFacts(random *rand.Rand) map[string]map[string]any {
	pick := func(values ...any) any { return values[random.IntN(len(values))] }
	return map[string]map[string]any{
		"Customer": {
			"Tier":    pick("gold", "silver", "bronze", "vip"),
			"Spend":   pick(0.0, 500.0, 1000.0, 1500.0),
			"Region":  pick("north", "south", "east", "remote"),
			"Name":    pick("Acme Ltd", "Jane", "Ltd Co", "Bob"),
			"Trusted": pick(true, false),
		},
		"Order": {
			"Total":    pick(0.0, 100.0, 500.0, 800.0, 2000.0),
			"Discount": 0.0, "Shipping": 0.0, "Due": 0.0,
			"Note": "", "Review": false, "Reason": "", "Hold": false,
		},
	}
}

func compare(t *testing.T, rules []grule.Rule, imported entities.GruleImport, facts map[string]map[string]any) {
	t.Helper()
	want, err := runGrule(rules, cloneFacts(facts))
	if err != nil {
		t.Fatalf("grule: %v", err)
	}
	got, err := runImported(t, imported, facts)
	if err != nil {
		t.Fatalf("metis: %v on %v", err, facts)
	}
	for field, variable := range imported.Results {
		fact, name, _ := strings.Cut(field, ".")
		wantValue := want[fact][name]
		gotValue, set := got[variable]
		if !set {
			gotValue = facts[fact][name]
		}
		if !reflect.DeepEqual(gotValue, wantValue) {
			t.Fatalf("%s: Grule %v, metis %v, on %v", field, wantValue, gotValue, facts)
		}
	}
}

// runImported runs the imported process's steps as the business rule task
// handler does: map the inputs, evaluate the table, write every answer.
func runImported(t *testing.T, imported entities.GruleImport, facts map[string]map[string]any) (map[string]any, error) {
	variables := map[string]any{}
	for fact, fields := range facts {
		variables[fact] = maps.Clone(fields)
	}
	evaluator := impl.NewDecisionTableEvaluator(impl.NewFEELEvaluator())
	for _, node := range imported.Process.Nodes {
		if node.Type != entities.BusinessRuleTask {
			continue
		}
		table := decisionByKey(imported, node.Properties["decision_key"].(string))
		inputs := mapping.Resolve(node.Properties["input_mapping"].(map[string]any), variables)
		result, err := evaluator.EvaluateTable(t.Context(), table, inputs)
		if err != nil {
			return nil, err
		}
		maps.Copy(variables, result.Values)
	}
	return variables, nil
}

func decisionByKey(imported entities.GruleImport, key string) entities.DecisionDefinition {
	for _, d := range imported.Decisions {
		if d.Key == key {
			return d
		}
	}
	panic("no decision " + key)
}

func cloneFacts(facts map[string]map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for fact, fields := range facts {
		out[fact] = maps.Clone(fields)
	}
	return out
}
