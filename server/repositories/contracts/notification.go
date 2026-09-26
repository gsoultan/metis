package contracts

// NotificationRepository keeps the notifications addressed to people: what
// records them and what they did with them, and what reads a person's own.
type NotificationRepository interface {
	NotificationWriter
	NotificationReader
}
