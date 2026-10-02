package impl

// handOverKind is what differs between the ways of handing a task to
// somebody: the words a refusal uses.
type handOverKind struct {
	// done completes "it cannot be …".
	done string
	// doing completes "say why you are …".
	doing string
	// missingTarget is what a request naming nobody is told.
	missingTarget string
}

var (
	assigning = handOverKind{
		done:          "assigned",
		doing:         "assigning this task",
		missingTarget: "say who the task is assigned to",
	}
	delegating = handOverKind{
		done:          "delegated",
		doing:         "delegating this task",
		missingTarget: "say who the task is delegated to",
	}
)
