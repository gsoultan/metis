package feel

// Node is one node of a parsed expression.
//
// The AST is the reason this package exists rather than another string matcher:
// once an expression is a tree, `[1..10]` and `"a,b"` and `not(x)` stop being
// special cases that a splitter has to guess at, and become ordinary shapes.
type Node interface{ node() }

// Literal is a constant: number, string, boolean or null.
type Literal struct{ Value Value }

// Name is a variable reference.
type Name struct{ Text string }

// Path is a property access, `applicant.income`.
type Path struct {
	Target Node
	Field  string
}

// Index is a list subscript, `items[1]`. FEEL indexes from 1, and negative
// indexes count from the end.
type Index struct {
	Target Node
	Index  Node
}

// Binary is an infix operation: arithmetic, comparison or logic.
type Binary struct {
	Op          string
	Left, Right Node
}

// Unary is a prefix operation, `-x` or `not(x)`.
type Unary struct {
	Op      string
	Operand Node
}

// RangeNode is an interval, `[1..10]` or `(date("2026-01-01")..date("2026-12-31"))`.
type RangeNode struct {
	Low, High         Node
	LowOpen, HighOpen bool
}

// ListNode is a list literal, `[1, 2, 3]`.
type ListNode struct{ Items []Node }

// ContextNode is a context literal, `{a: 1, b: 2}`.
type ContextNode struct {
	Keys   []string
	Values []Node
}

// Call is a function application, `date("2026-01-01")` or `contains(s, "x")`.
type Call struct {
	Name string
	Args []Node
}

// If is a conditional, `if x > 1 then "big" else "small"`.
type If struct {
	Cond, Then, Else Node
}

// InNode is a membership test, `x in [1,2,3]` or `x in [1..10]`.
type InNode struct {
	Value  Node
	Target Node
}

// UnaryTest is one item of a DMN decision-table cell: a bare comparison
// (`< 100`), a range, or a value to match for equality.
//
// It is a distinct node because a cell means something different from an
// expression: the input value is implicit. `< 100` is not an expression at all
// — there is nothing on the left — and treating it as one is what forced the
// previous evaluator into string matching.
type UnaryTest struct {
	// Op is the comparison operator, empty for an equality match.
	Op   string
	Expr Node
}

// UnaryTests is a comma-separated list of unary tests; the cell matches when
// any of them does. `-` parses to an empty list, which matches everything.
type UnaryTests struct {
	Tests []Node
	// Negated is true for `not(...)`, which inverts the whole list.
	Negated bool
	// MatchAll is true for `-`, the DMN "irrelevant" marker.
	MatchAll bool
}

func (*Literal) node()     {}
func (*Name) node()        {}
func (*Path) node()        {}
func (*Index) node()       {}
func (*Binary) node()      {}
func (*Unary) node()       {}
func (*RangeNode) node()   {}
func (*ListNode) node()    {}
func (*ContextNode) node() {}
func (*Call) node()        {}
func (*If) node()          {}
func (*InNode) node()      {}
func (*UnaryTest) node()   {}
func (*UnaryTests) node()  {}
