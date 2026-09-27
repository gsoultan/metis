package impl

// linkChange is what opening a brokerLink had to do to hand back an open
// channel, which says what its owner has to set up again.
type linkChange int

const (
	// linkUnchanged: the channel it already had is still open.
	linkUnchanged linkChange = iota
	// linkNewChannel: a new channel, on the connection it already had.
	linkNewChannel
	// linkReconnected: a new connection, and a new channel on it.
	linkReconnected
)
