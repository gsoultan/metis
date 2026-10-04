package entities

// DeviationOrigin is the road a deviation came by.
type DeviationOrigin string

const (
	// DeviationOriginInPlace: done to a running instance directly.
	DeviationOriginInPlace DeviationOrigin = "in_place"
	// DeviationOriginMigration: decided in a migration of the instance to a
	// newer definition.
	DeviationOriginMigration DeviationOrigin = "migration"
	// DeviationOriginTask: a hand-over or edit of a task.
	DeviationOriginTask DeviationOrigin = "task"
	// DeviationOriginAdHoc: a step activated inside an ad-hoc sub-process.
	DeviationOriginAdHoc DeviationOrigin = "adhoc"
)

// Valid reports whether o is one of the origins the ledger records.
func (o DeviationOrigin) Valid() bool {
	switch o {
	case DeviationOriginInPlace, DeviationOriginMigration, DeviationOriginTask, DeviationOriginAdHoc:
		return true
	}
	return false
}
