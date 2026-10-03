package contracts

// MaxHandOverReasonLength is the longest reason a hand-over may carry, in
// characters. The reason is kept in the audit trail for as long as the trail
// is, so what somebody may put there is bounded.
const MaxHandOverReasonLength = 1000

// HandOver says who is moving a task, to whom, and why.
type HandOver struct {
	// Actor is the signed-in caller making the change. An endpoint takes it
	// from the verified token, never from the request body.
	Actor string
	// Target is who receives the task. Empty for a release and a hand-back,
	// which name nobody.
	Target string
	// Reason is why. Required unless Actor holds the task: somebody handing
	// on their own work need not explain it, and an administrator overriding
	// whose work it is must.
	Reason string
}
