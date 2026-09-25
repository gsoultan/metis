package sqlconnector

// deployDialects is every server a step could run against, in a fixed order so
// the error reported is the same every time.
var deployDialects = []dialect{postgresDialect{}, mysqlDialect{}, sqlServerDialect{}}

// ValidateStep checks, when a definition is deployed, what can be checked
// before anyone knows which database the step will run against: that it names a
// variable for its answer, and that its query could be one read on at least one
// of the servers. A query that is a write, or two statements, fails on all of
// them and is refused now, where its author is, rather than as an incident.
//
// It is not the check that protects anything. The lookup checks the query again
// each time it runs, as the server it is actually running against reads it.
func ValidateStep(statement, resultVariable string) error {
	if err := validateResultVariable(resultVariable); err != nil {
		return err
	}
	var first error
	for _, d := range deployDialects {
		err := validateStatement(statement, d)
		if err == nil {
			return nil
		}
		if first == nil {
			first = err
		}
	}
	return first
}
