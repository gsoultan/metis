package contracts

// NotificationService sends people notifications and serves their notification
// centre: what writes them and what reads them, composed.
type NotificationService interface {
	NotificationWriter
	NotificationReader
}
