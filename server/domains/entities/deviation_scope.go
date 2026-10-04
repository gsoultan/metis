package entities

// DeviationScope is how much of the instance a deviation reaches.
type DeviationScope string

const (
	DeviationScopeTask      DeviationScope = "task"
	DeviationScopeIteration DeviationScope = "iteration"
	DeviationScopeInstance  DeviationScope = "instance"
)

// Valid reports whether s is one of the scopes the ledger records.
func (s DeviationScope) Valid() bool {
	switch s {
	case DeviationScopeTask, DeviationScopeIteration, DeviationScopeInstance:
		return true
	}
	return false
}
