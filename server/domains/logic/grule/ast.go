// Package grule reads rule sets written for the Grule rule engine
// (github.com/hyperjumptech/grule-rule-engine), in GRL or in Grule's JSON form,
// and translates the ones a decision table can answer exactly into metis
// decisions.
//
// A rule file is untrusted input, exactly as a process definition is: it is
// parsed by this package's own parser, within the limits in limits.go, into
// the tree below, and nothing in it is ever executed. FEEL is written from the
// tree, never by pasting rule text into an expression.
package grule

// Expr is one node of a parsed GRL expression.
type Expr interface{ grlExpr() }

// Literal is a constant: a string, a float64, a bool, or nil for `nil`.
type Literal struct{ Value any }

// Path is a reference to a fact or one of its fields: `Applicant.Age`.
type Path struct{ Parts []string }

// Index is `target[key]`, an element of a list or a map.
type Index struct {
	Target Expr
	Key    Expr
}

// Call is a function call, `Now()`, or with a receiver a method call,
// `Applicant.Name.Contains("x")`.
type Call struct {
	Receiver Expr
	Name     string
	Args     []Expr
}

// Unary is `!x` or `-x`.
type Unary struct {
	Op      string
	Operand Expr
}

// Binary is `left op right` for every GRL infix operator.
type Binary struct {
	Op          string
	Left, Right Expr
}

func (Literal) grlExpr() {}
func (Path) grlExpr()    {}
func (Index) grlExpr()   {}
func (Call) grlExpr()    {}
func (Unary) grlExpr()   {}
func (Binary) grlExpr()  {}
