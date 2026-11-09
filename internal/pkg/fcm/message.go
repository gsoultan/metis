package fcm

// Message is one push to one device.
type Message struct {
	// Token is the device's registration token, from the Firebase SDK in the app.
	Token string
	Title string
	Body  string
	// Link is where tapping the notification should go. It travels in Data as
	// "link" for an app to read, and as the web push click target when it is
	// an https address.
	Link string
	// Data is extra key/value pairs for the app. FCM accepts only strings.
	Data map[string]string
}
