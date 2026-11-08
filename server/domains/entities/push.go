package entities

// PushContent is what one push shows, already filled in.
type PushContent struct {
	Title string
	Body  string
	// Link is where tapping it goes, such as the task in the person's inbox.
	Link string
	// Data is extra text values for the app.
	Data map[string]string
}

// PushResult says how a push to one person went.
//
// Delivered == 0 with no error means there was nobody to reach: the person
// has no devices, every device was gone, or the project has no push
// connection. That is an answer, not a failure: the caller falls back to
// email, or to nothing.
type PushResult struct {
	// Devices is how many devices the person had when the push was sent.
	Devices int `json:"devices"`
	// Delivered is how many Firebase accepted it for.
	Delivered int `json:"delivered"`
	// Forgotten is how many devices Firebase said were gone, and were removed.
	Forgotten int `json:"forgotten"`
}
