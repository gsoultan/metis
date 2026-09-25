package entities

import "fmt"

// CatchableError is a domain error that carries a BPMN errorCode so that
// Error Boundary Events can match and reroute the process flow.
// Service tasks and external tasks should wrap failures in CatchableError
// when the errorCode is known, allowing declarative fault handling in the diagram.
type CatchableError struct {
	// Code is the BPMN errorCode matched against boundary event errorCodeVariable.
	Code string
	// Message is the human-readable error description.
	Message string
}

func (e *CatchableError) Error() string {
	return fmt.Sprintf("bpmn error [%s]: %s", e.Code, e.Message)
}
