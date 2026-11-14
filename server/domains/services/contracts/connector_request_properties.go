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

// StepFieldPrefix starts the node property a step stores one of its
// connector's message fields under: connector_field_text is the text a chat
// step sends, connector_field_to the address an email step writes to.
//
// Prefixed rather than bare, so a field named like another node property —
// "name", "type" — cannot be confused with it, and the designer can clear
// every field a step had when the step changes connector.
const StepFieldPrefix = "connector_field_"
