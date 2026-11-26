package grule

// Rule is one parsed Grule rule.
type Rule struct {
	Name        string
	Description string
	Salience    int
	When        Expr
	Then        []Statement

	// Position is where the rule starts, as `file:line`, so a report can say
	// which rule it means when two files define the same name.
	Position string
}
