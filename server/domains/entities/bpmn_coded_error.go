package entities

// BPMNCodedError is a failure that names the BPMN errorCode an error boundary
// event catches it by.
//
// An interface rather than CatchableError alone: a connector manifest names its
// failures in its own package, and an error boundary that only recognised
// CatchableError — which nothing on the execution path built — caught those
// failures only when it caught everything. A boundary waiting for
// CARD_DECLINED never fired, the job was retried and then raised an incident,
// and the path the modeller drew for a declined card was never taken.
type BPMNCodedError interface {
	error
	BPMNErrorCode() string
}
