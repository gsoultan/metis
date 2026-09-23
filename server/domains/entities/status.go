package entities

import "slices"

// ProcessStatus defines the current state of a process instance.
type ProcessStatus string

const (
	ProcessActive    ProcessStatus = "active"
	ProcessCompleted ProcessStatus = "completed"
	ProcessSuspended ProcessStatus = "suspended"
	ProcessFailed    ProcessStatus = "failed"
	// ProcessCancelled is an instance somebody ended deliberately, as distinct
	// from one that ran to its end event.
	//
	// Reusing "completed" for this would make an instance that was called off
	// read, in every list and every count, exactly like one that succeeded —
	// and for a quotation that was voided mid-approval those are not the same
	// fact. "failed" is no better: nothing went wrong, somebody decided.
	ProcessCancelled ProcessStatus = "cancelled"
)

var validProcessTransitions = map[ProcessStatus][]ProcessStatus{
	ProcessActive:    {ProcessCompleted, ProcessSuspended, ProcessFailed, ProcessCancelled},
	ProcessSuspended: {ProcessActive, ProcessFailed, ProcessCancelled},
	ProcessCompleted: {},
	ProcessFailed:    {},
	ProcessCancelled: {},
}

func (s ProcessStatus) CanTransitionTo(target ProcessStatus) bool {
	return slices.Contains(validProcessTransitions[s], target)
}

// TaskStatus defines the current state of a task.
type TaskStatus string

const (
	TaskUnclaimed TaskStatus = "unclaimed" // same as pending
	TaskClaimed   TaskStatus = "claimed"
	TaskCompleted TaskStatus = "completed"
	TaskCanceled  TaskStatus = "canceled"
	TaskDelegated TaskStatus = "delegated"
	TaskEscalated TaskStatus = "escalated"
)

var validTaskTransitions = map[TaskStatus][]TaskStatus{
	TaskUnclaimed: {TaskClaimed, TaskCompleted, TaskCanceled, TaskEscalated},
	TaskClaimed:   {TaskUnclaimed, TaskCompleted, TaskCanceled, TaskDelegated, TaskEscalated},
	TaskDelegated: {TaskClaimed, TaskCompleted, TaskCanceled},
	TaskEscalated: {TaskClaimed, TaskCompleted, TaskCanceled},
	TaskCompleted: {},
	TaskCanceled:  {},
}

func (s TaskStatus) CanTransitionTo(target TaskStatus) bool {
	return slices.Contains(validTaskTransitions[s], target)
}
