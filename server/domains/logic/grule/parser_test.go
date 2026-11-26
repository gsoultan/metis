package grule

import (
	"reflect"
	"strings"
	"testing"
)

const speedUpGRL = `
// From Grule's own documentation.
rule SpeedUp "When testcar is speeding up we increase the speed." salience 10 {
    when
        TestCar.SpeedUp == true && TestCar.Speed < TestCar.MaxSpeed
    then
        TestCar.Speed = TestCar.Speed + TestCar.SpeedIncrement;
        DistanceRecord.TotalDistance = DistanceRecord.TotalDistance + TestCar.Speed;
        Log("Speed increased");
}`

const speedUpJSON = `{
    "name": "SpeedUp",
    "desc": "When testcar is speeding up we increase the speed.",
    "salience": 10,
    "when": {"and": [{"eq": [{"obj": "TestCar.SpeedUp"}, {"const": true}]}, {"lt": ["TestCar.Speed", "TestCar.MaxSpeed"]}]},
    "then": [
        {"set": ["TestCar.Speed", {"plus": ["TestCar.Speed", "TestCar.SpeedIncrement"]}]},
        {"set": [{"obj": "DistanceRecord.TotalDistance"}, {"plus": ["DistanceRecord.TotalDistance", "TestCar.Speed"]}]},
        {"call": ["Log", {"const": "Speed increased"}]}
    ]
}`

func TestParseGRL(t *testing.T) {
	rules, err := ParseGRL("speed.grl", speedUpGRL)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 {
		t.Fatalf("got %d rules", len(rules))
	}
	r := rules[0]
	if r.Name != "SpeedUp" || r.Salience != 10 || r.Position != "speed.grl:3" || len(r.Then) != 3 {
		t.Errorf("rule = %+v", r)
	}
	want := Binary{Op: "&&",
		Left:  Binary{Op: "==", Left: Path{Parts: []string{"TestCar", "SpeedUp"}}, Right: Literal{Value: true}},
		Right: Binary{Op: "<", Left: Path{Parts: []string{"TestCar", "Speed"}}, Right: Path{Parts: []string{"TestCar", "MaxSpeed"}}}}
	if !reflect.DeepEqual(r.When, want) {
		t.Errorf("when = %#v", r.When)
	}
}

// TestJSONReadsAsTheSameRule: Grule's JSON form of its own example is the
// same rule as the GRL.
func TestJSONReadsAsTheSameRule(t *testing.T) {
	fromGRL, err := ParseGRL("speed.grl", speedUpGRL)
	if err != nil {
		t.Fatal(err)
	}
	fromJSON, err := ParseJSON("speed.json", []byte("["+speedUpJSON+"]"))
	if err != nil {
		t.Fatal(err)
	}
	fromJSON[0].Position = fromGRL[0].Position
	if !reflect.DeepEqual(fromGRL, fromJSON) {
		t.Errorf("GRL:\n%#v\nJSON:\n%#v", fromGRL, fromJSON)
	}
	one, err := ParseJSON("one.json", []byte(speedUpJSON))
	if err != nil || len(one) != 1 {
		t.Errorf("a single JSON rule object: %v, %d rules", err, len(one))
	}
}

func TestParseGRLPrecedenceAndForms(t *testing.T) {
	rules, err := ParseGRL("f.grl", `RULE r "d" SALIENCE -3 { WHEN !A.B || A.C + 2 * 3 >= 7 && A.S.Contains('x') /* c */ THEN A.D += 1; Retract("r") }`)
	if err != nil {
		t.Fatal(err)
	}
	r := rules[0]
	if r.Salience != -3 {
		t.Errorf("salience = %d", r.Salience)
	}
	or, ok := r.When.(Binary)
	if !ok || or.Op != "||" {
		t.Fatalf("|| does not bind loosest: %#v", r.When)
	}
	and := or.Right.(Binary)
	ge := and.Left.(Binary)
	if and.Op != "&&" || ge.Op != ">=" || ge.Left.(Binary).Right.(Binary).Op != "*" {
		t.Errorf("precedence: %#v", or.Right)
	}
	if call := and.Right.(Call); call.Name != "Contains" || call.Args[0] != (Literal{Value: "x"}) {
		t.Errorf("method call: %#v", and.Right)
	}
	if a := r.Then[0].(Assign); a.Op != "+=" {
		t.Errorf("compound assignment: %#v", r.Then[0])
	}
}

func TestParseErrorsNameTheLine(t *testing.T) {
	cases := map[string]string{
		"rule A { when X > then Y = 1; }":         "line 1",
		"rule A {\n when X > 1\n Y = 1; }":        "line 3",
		`rule A { when X == "open then Y = 1; }`:  "never closed",
		"rule A { when X == 1 then Y = 1 Z = 2 }": "`;`",
		"rule A { when X == 1 then 5; }":          "assign a value or call",
		"/* never ends":                           "never closed",
		`rule A { when X == "\q" then Y = 1; }`:   `\q`,
	}
	for source, want := range cases {
		_, err := ParseGRL("bad.grl", source)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %v, want one mentioning %q", source, err, want)
		}
	}
}

// TestHostileInputIsRefusedWithinLimits: depth and size are bounded before the
// parser can be made to use unbounded stack or memory.
func TestHostileInputIsRefusedWithinLimits(t *testing.T) {
	deep := "rule A { when " + strings.Repeat("(", 10_000) + "1" + strings.Repeat(")", 10_000) + " then X = 1; }"
	if _, err := ParseGRL("deep.grl", deep); err == nil || !strings.Contains(err.Error(), "levels deep") {
		t.Errorf("deep nesting: %v", err)
	}
	unary := "rule A { when " + strings.Repeat("!", 10_000) + "X then Y = 1; }"
	if _, err := ParseGRL("unary.grl", unary); err == nil || !strings.Contains(err.Error(), "levels deep") {
		t.Errorf("deep unary: %v", err)
	}
	chain := "rule A { when X.N" + strings.Repeat(" + X.N", 10_000) + " > 1 then Y.M = 1; }"
	if _, err := ParseGRL("chain.grl", chain); err == nil || !strings.Contains(err.Error(), "levels deep") {
		t.Errorf("a long chain: %v", err)
	}
	calls := "rule A { when X.S" + strings.Repeat(".ToUpper()", 10_000) + " == \"A\" then Y.M = 1; }"
	if _, err := ParseGRL("calls.grl", calls); err == nil || !strings.Contains(err.Error(), "levels deep") {
		t.Errorf("a long chain of calls: %v", err)
	}
	wide := `{"name":"A","when":{"and":[` + strings.Repeat(`"X.N > 1",`, 10_000) + `"true"]},"then":["Y.M = 1"]}`
	if _, err := ParseJSON("wide.json", []byte(wide)); err == nil || !strings.Contains(err.Error(), "levels deep") {
		t.Errorf("a wide JSON and: %v", err)
	}
	big := strings.Repeat(" ", MaxSourceBytes+1)
	if _, err := ParseGRL("big.grl", big); err == nil {
		t.Error("a file over the size limit was parsed")
	}
	deepJSON := strings.Repeat(`{"not":[`, 200) + "1" + strings.Repeat(`,1]}`, 200)
	if _, err := ParseJSON("deep.json", []byte(`{"name":"A","when":`+deepJSON+`,"then":["X = 1"]}`)); err == nil || !strings.Contains(err.Error(), "levels deep") {
		t.Errorf("deep JSON: %v", err)
	}
	var many strings.Builder
	for i := 0; i <= MaxRules; i++ {
		many.WriteString("rule R")
		many.WriteString(strings.Repeat("x", i%7))
		many.WriteString(" { when true then X = 1; }\n")
	}
	if _, err := ParseGRL("many.grl", many.String()); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Errorf("too many rules: %v", err)
	}
}

func TestJSONActionTextCannotSmuggleARule(t *testing.T) {
	_, err := ParseJSON("x.json", []byte(`{"name":"A","when":"true","then":["X = 1; } rule B { when true then Y = 2"]}`))
	if err == nil {
		t.Error("an action holding a second rule was accepted")
	}
}
