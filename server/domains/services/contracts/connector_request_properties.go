package contracts

// The node properties a step names its own connector request by. The job reads
// them to build a ConnectorRequest; a connector that takes one lists them in
// its node schema so the designer knows what to ask for; the designer writes
// them. One list, so the three cannot disagree about a name.
const (
	// StatementProperty is the operation text the step wrote — for a database
	// lookup, its SQL.
	StatementProperty = "connector_statement"
	// ParamsProperty maps each parameter the statement names to the process
	// value it takes: a variable name or a FEEL expression.
	ParamsProperty = "connector_params"
	// ResultVariableProperty is the variable the answer is stored under. The
	// designer already writes it under this name for a script task.
	ResultVariableProperty = "result_variable"
)
