package impl

import "context"

// publishConfirmation is the broker's answer to one publish, once it comes:
// true when the broker took the message, false when it refused it.
type publishConfirmation interface {
	WaitContext(ctx context.Context) (bool, error)
}
