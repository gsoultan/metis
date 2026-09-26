package entities

// DecisionCellScope is what one condition cell of a decision table sees when it
// is tested against a case.
//
// DMN tests a cell with its column's value as the implicit subject — `< 100`
// has nothing on its left — and the decision's variables in scope by name, so
// `< maximum` compares with another column and `> credit_limit` with a
// variable no column reads. A cell used to see its own column's value and
// nothing else, and every such comparison was made against null.
type DecisionCellScope struct {
	// Input is the value of the cell's own column: what every test in the
	// cell compares, and what `_input` names. No variable can shadow it.
	Input any

	// Variables are the variables the decision was evaluated with, answers of
	// the decisions it requires included — the same map the columns read, so
	// a column and a variable of the same name are one value. Only read.
	Variables map[string]any

	// Columns are the variables the table's condition columns read. They
	// decide what a lone word means: one naming a column is that column's
	// value, and any other is text, as a lone word in a cell always was.
	Columns []string
}
