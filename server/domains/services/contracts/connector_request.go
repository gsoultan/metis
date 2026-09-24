package contracts

// ConnectorRequest is a connector call that carries what the step itself asked
// for, beside the process variables.
//
// ExecuteConnector hands an executor two things: the connection's settings and
// the process variables. Neither can say which query a step wants run or where
// its answer belongs — the settings are the connection's, shared by every step
// that uses it, and the variables are the process's data. A database lookup
// needs both said per step, so this carries them.
//
// Named fields rather than the node's property bag. What a step may contribute
// is listed here and nowhere else, so a property a step sets cannot reach the
// connection's settings — a DSN, a password — by being given the same name.
// There is no merge to get wrong.
type ConnectorRequest struct {
	// Statement is the operation text the step wrote: for a database lookup,
	// its SQL. It is authored in the designer, so an executor that reads it is
	// reading untrusted input.
	Statement string

	// Params are the step's named arguments, already resolved from the process
	// variables. An executor binds them; it never splices them into Statement.
	Params map[string]any

	// ResultVariable is the one process variable the answer is stored under.
	ResultVariable string

	// Variables is the whole variable set, exactly as ExecuteConnector passes
	// it, for an executor that is not a RequestExecutor.
	Variables map[string]any
}
