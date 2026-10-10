package entities

// SweptRequests is what one sweep of the requests for a second administrator
// wrote down, of the two things the clock decides.
//
// They are different facts and are counted apart. An expired request is one
// nobody decided. An interrupted one was approved, and its run never
// reported: somebody did decide, and something then went wrong.
type SweptRequests struct {
	// Expired is how many requests that still waited past their deadline
	// were closed as expired.
	Expired int64
	// Interrupted is how many approved requests whose run had not reported
	// when its window closed were closed as interrupted.
	Interrupted int64
}

// Closed is how many requests the sweep closed in all.
func (s SweptRequests) Closed() int64 { return s.Expired + s.Interrupted }
