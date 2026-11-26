package grule

// analysedRule is a rule the translator can reason about: each field it sets
// with the value it sets it to, what it reads, and how it stops firing.
type analysedRule struct {
	Rule
	order int // position across every file of the import

	// writes are the fields the rule sets, in the order it sets them, and
	// values what each is set to, earlier assignments already substituted.
	writes []string
	values map[string]Expr

	// reads are the fields its condition and its values read.
	reads map[string]bool

	// guarded is true when its condition tests a field it sets, so its own
	// action stops it firing again; retracts when it calls Retract on itself.
	guarded  bool
	retracts bool

	// conditions are its `when` written for a table, and feelValues each
	// value it sets written as FEEL, both made by prepare.
	conditions []condition
	feelValues map[string]string

	// notes are said about the rule in the report without stopping it being
	// imported.
	notes []string
}

// writesField reports whether the rule sets key.
func (r *analysedRule) writesField(key string) bool {
	_, ok := r.values[key]
	return ok
}
