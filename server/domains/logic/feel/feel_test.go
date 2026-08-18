package feel

import (
	"strings"
	"testing"
	"time"
)

// evalString is the shorthand these tables use: evaluate and render.
func evalString(t *testing.T, expr string, vars map[string]any) string {
	t.Helper()
	value, err := Evaluate(expr, vars)
	if err != nil {
		t.Fatalf("Evaluate(%q): %v", expr, err)
	}
	return value.String()
}

func TestLiteralsAndArithmetic(t *testing.T) {
	tests := []struct {
		expr string
		want string
	}{
		{"1", "1"},
		{"-3", "-3"},
		{"1.5", "1.5"},
		{`"hello"`, "hello"},
		{"true", "true"},
		{"null", "null"},

		{"1 + 2", "3"},
		{"10 - 4", "6"},
		{"3 * 4", "12"},
		{"10 / 4", "2.5"},
		{"2 ** 10", "1024"},

		// Precedence and associativity — the things a string matcher cannot do
		// at all.
		{"1 + 2 * 3", "7"},
		{"(1 + 2) * 3", "9"},
		{"10 - 2 - 3", "5"},
		{"2 ** 3 * 2", "16"},
		{"-2 + 5", "3"},

		{`"a" + "b"`, "ab"},
	}

	for _, tc := range tests {
		t.Run(tc.expr, func(t *testing.T) {
			if got := evalString(t, tc.expr, nil); got != tc.want {
				t.Errorf("%s = %s, want %s", tc.expr, got, tc.want)
			}
		})
	}
}

func TestComparisonAndLogic(t *testing.T) {
	vars := map[string]any{"amount": 900, "status": "GOLD"}

	tests := []struct {
		expr string
		want string
	}{
		{"amount > 500", "true"},
		{"amount < 500", "false"},
		{"amount >= 900", "true"},
		{"amount = 900", "true"},
		{"amount != 900", "false"},
		{`status = "GOLD"`, "true"},

		// `and` / `or`, which the previous evaluator had no concept of.
		{"amount > 500 and status = \"GOLD\"", "true"},
		{"amount > 5000 and status = \"GOLD\"", "false"},
		{"amount > 5000 or status = \"GOLD\"", "true"},
		{"not(amount > 5000)", "true"},

		// Types do not coerce: "900" is not 900. The string matcher compared
		// printed forms and said these were equal.
		{`"900" = 900`, "false"},
		{`1 = true`, "false"},

		// An unset variable is null, and comparing with null is false rather
		// than an error — a decision table routinely tests inputs an instance
		// never set.
		{"missing > 5", "false"},
		{"missing = null", "true"},
	}

	for _, tc := range tests {
		t.Run(tc.expr, func(t *testing.T) {
			if got := evalString(t, tc.expr, vars); got != tc.want {
				t.Errorf("%s = %s, want %s", tc.expr, got, tc.want)
			}
		})
	}
}

func TestPathsAndLists(t *testing.T) {
	vars := map[string]any{
		"applicant": map[string]any{
			"income": 50000,
			"name":   "Ada",
			"address": map[string]any{
				"city": "London",
			},
		},
		"items": []any{
			map[string]any{"price": 10.0, "qty": 2.0},
			map[string]any{"price": 5.0, "qty": 1.0},
		},
		"tags": []any{"a", "b", "c"},
	}

	tests := []struct {
		expr string
		want string
	}{
		{"applicant.income", "50000"},
		{"applicant.name", "Ada"},
		{"applicant.address.city", "London"},
		{"applicant.missing", "null"},

		{"tags[1]", "a"},
		{"tags[3]", "c"},
		{"tags[-1]", "c"},    // negative indexes count from the end
		{"tags[99]", "null"}, // out of range is null, not a crash

		// Projection plus aggregate — how a decision totals a basket.
		{"sum(items.price)", "15"},
		{"count(items)", "2"},
		{"max(items.price)", "10"},

		{"[1, 2, 3]", "[1, 2, 3]"},
		{"count([1,2,3])", "3"},
		{"list contains(tags, \"b\")", "true"},
		{"list contains(tags, \"z\")", "false"},
	}

	for _, tc := range tests {
		t.Run(tc.expr, func(t *testing.T) {
			if got := evalString(t, tc.expr, vars); got != tc.want {
				t.Errorf("%s = %s, want %s", tc.expr, got, tc.want)
			}
		})
	}
}

func TestBuiltins(t *testing.T) {
	tests := []struct {
		expr string
		want string
	}{
		{`contains("invoice-2026", "2026")`, "true"},
		{`starts with("invoice-2026", "inv")`, "true"},
		{`ends with("invoice-2026", "26")`, "true"},
		{`string length("hello")`, "5"},
		{`upper case("abc")`, "ABC"},
		{`substring("abcdef", 2)`, "bcdef"},
		{`substring("abcdef", 2, 3)`, "bcd"},

		{"abs(-7)", "7"},
		{"ceiling(1.2)", "2"},
		{"floor(1.8)", "1"},
		{"round(3.14159, 2)", "3.14"},
		{"modulo(7, 3)", "1"},

		{"sum(1, 2, 3)", "6"},
		{"min([4, 2, 9])", "2"},
		{"max(4, 2, 9)", "9"},
		{"mean([2, 4])", "3"},
		{"all([true, true])", "true"},
		{"any([false, true])", "true"},

		{"number(\"42\")", "42"},
		{"string(42)", "42"},

		{`date("2026-03-15").year`, "2026"},
		{`date("2026-03-15").month`, "3"},
	}

	for _, tc := range tests {
		t.Run(tc.expr, func(t *testing.T) {
			if got := evalString(t, tc.expr, nil); got != tc.want {
				t.Errorf("%s = %s, want %s", tc.expr, got, tc.want)
			}
		})
	}
}

// TestTemporal covers dates and durations — the entire category the previous
// evaluator lacked, and the one that makes ISO-8601 timers expressible.
func TestTemporal(t *testing.T) {
	frozen := time.Date(2026, 3, 15, 10, 30, 0, 0, time.UTC)
	Now = func() time.Time { return frozen }
	t.Cleanup(func() { Now = time.Now })

	tests := []struct {
		expr string
		want string
	}{
		{`date("2026-03-15")`, "2026-03-15"},
		{`date("2026-03-15") = date("2026-03-15")`, "true"},
		{`date("2026-03-15") < date("2026-04-01")`, "true"},
		{"today()", "2026-03-15"},

		{`duration("PT5M")`, "PT5M"},
		{`duration("P2D")`, "P2D"},
		{`duration("P1Y6M")`, "P1Y6M"},

		// Date arithmetic.
		{`date("2026-03-15") + duration("P1D")`, "2026-03-16"},
		{`date("2026-03-15") - duration("P1D")`, "2026-03-14"},
		{`date("2026-03-15") + duration("P1M")`, "2026-04-15"},

		// A date range, which a DMN table uses for validity windows.
		{`date("2026-06-01") in [date("2026-01-01")..date("2026-12-31")]`, "true"},
		{`date("2027-06-01") in [date("2026-01-01")..date("2026-12-31")]`, "false"},
	}

	for _, tc := range tests {
		t.Run(tc.expr, func(t *testing.T) {
			if got := evalString(t, tc.expr, nil); got != tc.want {
				t.Errorf("%s = %s, want %s", tc.expr, got, tc.want)
			}
		})
	}
}

// TestYearMonthDurationsStayApart pins the refusal to mix duration flavours.
// A year has no fixed number of seconds, so P1YT1H cannot be reduced to a
// single elapsed time without knowing the date it applies to.
func TestYearMonthDurationsStayApart(t *testing.T) {
	if _, err := ParseDuration("P1Y2MT3H"); err == nil {
		t.Fatal("mixing years with hours was accepted; that duration has no fixed length")
	}
	if _, err := Evaluate(`duration("P1Y") + duration("PT1H")`, nil); err == nil {
		t.Fatal("adding a year-month duration to a day-time one was accepted")
	}
}

func TestRanges(t *testing.T) {
	tests := []struct {
		expr string
		want string
	}{
		{"5 in [1..10]", "true"},
		{"1 in [1..10]", "true"},   // closed low
		{"10 in [1..10]", "true"},  // closed high
		{"1 in (1..10]", "false"},  // open low
		{"10 in [1..10)", "false"}, // open high
		{"11 in [1..10]", "false"},
		{"5 between 1 and 10", "true"},

		// The DMN spelling of an open end: a reversed square bracket. It is what
		// Camunda exports and what this product's own cell menu offers, and the
		// parser used to reject both — `]1..10]` because `]` could not start an
		// expression, and `[1..10[` because the closing `[` was read as the
		// start of an index.
		{"1 in ]1..10]", "false"},
		{"2 in ]1..10]", "true"},
		{"10 in [1..10[", "false"},
		{"9 in [1..10[", "true"},
		{"1 in ]1..10[", "false"},
		{"5 in ]1..10[", "true"},

		// Indexing still works where it is not a range bound.
		{"[10, 20, 30][2]", "20"},
	}

	for _, tc := range tests {
		t.Run(tc.expr, func(t *testing.T) {
			if got := evalString(t, tc.expr, nil); got != tc.want {
				t.Errorf("%s = %s, want %s", tc.expr, got, tc.want)
			}
		})
	}
}

// TestUnaryTests is the DMN decision-table cell grammar: the form the table
// evaluator actually calls.
func TestUnaryTests(t *testing.T) {
	tests := []struct {
		name  string
		cell  string
		input any
		want  bool
	}{
		{"empty cell matches anything", "", 42, true},
		{"dash matches anything", "-", "whatever", true},

		{"bare equality", `"GOLD"`, "GOLD", true},
		{"bare equality misses", `"GOLD"`, "SILVER", false},
		{"numeric equality", "42", 42, true},

		{"greater than", "> 80", 90, true},
		{"greater than misses", "> 80", 70, false},
		{"at most", "<= 80", 80, true},
		{"not equal", "!= 3", 4, true},

		{"range", "[1..10]", 5, true},
		{"range excludes", "[1..10]", 11, false},
		{"open range", "(1..10]", 1, false},

		// A comma is a disjunction, and the old splitter could not tell it
		// from a comma inside a range or a string.
		{"disjunction", `"GOLD","SILVER"`, "SILVER", true},
		{"disjunction misses", `"GOLD","SILVER"`, "BRONZE", false},
		{"comparison disjunction", "< 10, > 100", 200, true},
		{"comma inside a range is not a separator", "[1..10]", 5, true},
		{"comma inside a string is not a separator", `"a,b"`, "a,b", true},

		{"negation", `not("GOLD")`, "SILVER", true},
		{"negation misses", `not("GOLD")`, "GOLD", false},
		{"negated range", "not([1..10])", 50, true},

		// Types stay apart in cells too.
		{"string input against numeric cell", "> 80", "abc", false},
		{"null input", "> 80", nil, false},

		// A cell can reference other variables, which is how a table compares
		// two inputs.
		{"expression cell", "> 80", 81, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EvaluateUnaryTests(tc.cell, tc.input, nil)
			if err != nil {
				t.Fatalf("EvaluateUnaryTests(%q, %v): %v", tc.cell, tc.input, err)
			}
			if got != tc.want {
				t.Errorf("cell %q against %v = %v, want %v", tc.cell, tc.input, got, tc.want)
			}
		})
	}
}

// TestUnaryTestsSeeOtherVariables covers a cell comparing the input against
// another input of the same row.
func TestUnaryTestsSeeOtherVariables(t *testing.T) {
	got, err := EvaluateUnaryTests("> threshold", 100, map[string]any{"threshold": 50})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !got {
		t.Error("a cell could not reference another variable in scope")
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want string
	}{
		{"unknown function", "nope(1)", "no function called"},
		{"unterminated string", `"abc`, "unterminated string"},
		{"unclosed parenthesis", "(1 + 2", "not closed"},
		{"unclosed list", "[1, 2", "closing bracket"},
		{"division by zero", "1 / 0", "division by zero"},
		{"type mismatch", `1 + "a"`, "cannot apply"},
		{"comparing unlike types", `1 < "a"`, "cannot compare"},
		{"wrong arity", "abs(1, 2)", "takes"},
		{"trailing junk", "1 2", "unexpected"},
		{"bad date", `date("nonsense")`, "not a date"},
		{"bad duration", `duration("nonsense")`, "not a duration"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Evaluate(tc.expr, nil)
			if err == nil {
				t.Fatalf("%s was accepted; it should be an error", tc.expr)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error for %s = %q, want it to mention %q", tc.expr, err, tc.want)
			}
		})
	}
}

// TestNoUnboundedComputation is the security property that motivated replacing
// the JavaScript evaluator: the language has no loop, no recursion and no
// allocation the author controls, so a hostile expression cannot hold a worker
// or exhaust memory. The measured JavaScript case ran 37 seconds past a 200ms
// budget; these all return immediately.
func TestNoUnboundedComputation(t *testing.T) {
	hostile := []string{
		strings.Repeat("not(", 100) + "true" + strings.Repeat(")", 100),
		strings.Repeat("(", 200) + "1" + strings.Repeat(")", 200),
		"[" + strings.Repeat("1,", 5000) + "1]",
		strings.Repeat("1+", 5000) + "1",
	}

	for i, expr := range hostile {
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = Evaluate(expr, nil)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("hostile expression %d did not finish; the language must be total", i)
		}
	}
}

// TestDeepNestingIsRefused pins the parser's depth bound. Without it, deeply
// nested input becomes deep recursion and exhausts the goroutine stack.
func TestDeepNestingIsRefused(t *testing.T) {
	deep := strings.Repeat("(", 5000) + "1" + strings.Repeat(")", 5000)
	if _, err := Evaluate(deep, nil); err == nil {
		t.Fatal("5000 levels of nesting was accepted; that is a stack overflow waiting to happen")
	}
}

func TestValueRoundTrip(t *testing.T) {
	original := map[string]any{
		"n": 42.0,
		"s": "text",
		"b": true,
		"l": []any{1.0, 2.0},
		"c": map[string]any{"inner": "x"},
	}

	for name, want := range original {
		value := FromAny(want)
		got := value.ToAny()

		switch name {
		case "n", "s", "b":
			if got != want {
				t.Errorf("%s round-tripped to %v, want %v", name, got, want)
			}
		default:
			// Lists and contexts compare structurally; rendering is enough to
			// show they survived.
			if FromAny(got).String() != value.String() {
				t.Errorf("%s round-tripped to %v", name, got)
			}
		}
	}
}

// TestCacheReturnsEquivalentResults guards the AST cache: a shared tree must
// not carry state between evaluations with different variables.
func TestCacheReturnsEquivalentResults(t *testing.T) {
	expr := "amount > threshold"

	first, err := Evaluate(expr, map[string]any{"amount": 100, "threshold": 50})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := Evaluate(expr, map[string]any{"amount": 10, "threshold": 50})
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	if !first.Truthy() || second.Truthy() {
		t.Fatalf("cached AST leaked state between evaluations: %v then %v", first, second)
	}
}

// TestSubsetIsDocumented keeps execution-plan.md §2.1 honest: every category it
// promises is exercised, and the exclusions it names really are excluded.
func TestSubsetIsDocumented(t *testing.T) {
	supported := map[string]string{
		"literals":    `1`,
		"comparison":  `1 < 2`,
		"ranges":      `5 in [1..10]`,
		"lists":       `list contains([1,2], 1)`,
		"arithmetic":  `1 + 1`,
		"logic":       `true and true`,
		"paths":       `{a: 1}.a`,
		"built-ins":   `abs(-1)`,
		"unary tests": `-`,
	}
	for category, expr := range supported {
		t.Run("supports "+category, func(t *testing.T) {
			if category == "unary tests" {
				if _, err := EvaluateUnaryTests(expr, 1, nil); err != nil {
					t.Errorf("%s: %v", category, err)
				}
				return
			}
			if _, err := Evaluate(expr, nil); err != nil {
				t.Errorf("%s: %v", category, err)
			}
		})
	}

	// Documented as out of scope for v1. They must fail rather than silently
	// half-work, which is the failure mode the plan warns against.
	excluded := map[string]string{
		"for/return":          `for x in [1,2] return x`,
		"some/every":          `some x in [1,2] satisfies x > 1`,
		"function definition": `function(x) x + 1`,
	}
	for category, expr := range excluded {
		t.Run("excludes "+category, func(t *testing.T) {
			if _, err := Evaluate(expr, nil); err == nil {
				t.Errorf("%s parsed, but the plan documents it as unsupported", category)
			}
		})
	}
}

// TestBareWordsInCellsAreText pins a deliberate deviation from strict FEEL.
//
// Strict FEEL reads a bare name as a variable reference, and Camunda requires
// decision cells to quote their strings. Real tables — including every one in
// this repository — write CLOSED rather than "CLOSED", because that is what a
// person writing a status column does. Enforcing the strict rule would turn
// those cells into null and stop them matching: not an error anyone would
// notice, just a table quietly returning the wrong answer.
//
// A variable of the same name still wins, so the FEEL meaning is available
// whenever there is something to resolve to.
func TestBareWordsInCellsAreText(t *testing.T) {
	tests := []struct {
		name  string
		cell  string
		input any
		vars  map[string]any
		want  bool
	}{
		{
			name: "a bare word matches the text",
			cell: "CLOSED", input: "CLOSED", want: true,
		},
		{
			name: "a bare word does not match other text",
			cell: "CLOSED", input: "OPEN", want: false,
		},
		{
			name: "a comma-separated list of bare words",
			cell: "OPEN, IN_PROGRESS", input: "IN_PROGRESS", want: true,
		},
		{
			name: "quoted strings keep working",
			cell: `"CLOSED"`, input: "CLOSED", want: true,
		},
		{
			// The ambiguity resolves toward the variable when one exists, so a
			// cell can still compare two inputs of the same row.
			name: "a variable in scope wins over the text reading",
			cell: "threshold", input: 50.0,
			vars: map[string]any{"threshold": 50.0},
			want: true,
		},
		{
			name: "and the variable reading is a real comparison, not a name match",
			cell: "threshold", input: "threshold",
			vars: map[string]any{"threshold": 50.0},
			want: false,
		},
		{
			// Operators always take the FEEL reading of their operand.
			name: "an operator cell resolves its variable",
			cell: "> threshold", input: 100.0,
			vars: map[string]any{"threshold": 50.0},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EvaluateUnaryTests(tc.cell, tc.input, tc.vars)
			if err != nil {
				t.Fatalf("cell %q: %v", tc.cell, err)
			}
			if got != tc.want {
				t.Errorf("cell %q against %v = %v, want %v", tc.cell, tc.input, got, tc.want)
			}
		})
	}
}

// TestBareWordsStayStrictInExpressions confirms the leniency is confined to
// decision cells: in an ordinary expression a bare name is still a variable,
// and an unknown one is still null.
func TestBareWordsStayStrictInExpressions(t *testing.T) {
	value, err := Evaluate("CLOSED", nil)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !value.IsNull() {
		t.Errorf("a bare name in an expression = %v, want null — leniency belongs to cells only", value)
	}
}

// TestSingleQuotedStrings pins the second compatibility affordance. FEEL
// defines only double-quoted strings, but tables deployed against the previous
// JavaScript-flavoured evaluator wrote 'VIP', and those decisions are live.
func TestSingleQuotedStrings(t *testing.T) {
	got, err := EvaluateUnaryTests(`'VIP'`, "VIP", nil)
	if err != nil {
		t.Fatalf("single-quoted cell: %v", err)
	}
	if !got {
		t.Error(`the cell 'VIP' did not match the input "VIP"`)
	}

	if value := evalString(t, `'a' + "b"`, nil); value != "ab" {
		t.Errorf("mixed quoting = %q, want ab", value)
	}
}
