package grule

// Statement is one action in a rule's `then`.
type Statement interface{ grlStatement() }

// Assign is `target = value`, or a compound `target += value`.
type Assign struct {
	Target Expr
	Op     string
	Value  Expr
}

// CallStatement is a call made for its effect: `Retract("Name")`.
type CallStatement struct{ Call Call }

func (Assign) grlStatement()        {}
func (CallStatement) grlStatement() {}
