package fcm

import (
	"maps"
	"strings"
)

// linkDataKey is the data key an app reads the tap target from.
const linkDataKey = "link"

// envelope is the HTTP v1 request body.
type envelope struct {
	Message wireMessage `json:"message"`
}

type wireMessage struct {
	Token        string            `json:"token"`
	Notification wireNotification  `json:"notification"`
	Data         map[string]string `json:"data,omitempty"`
	Webpush      *wireWebpush      `json:"webpush,omitempty"`
}

type wireNotification struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}

type wireWebpush struct {
	FCMOptions wireFCMOptions `json:"fcm_options"`
}

type wireFCMOptions struct {
	Link string `json:"link"`
}

func envelopeFor(msg Message) envelope {
	data := maps.Clone(msg.Data)
	if msg.Link != "" {
		if data == nil {
			data = map[string]string{}
		}
		data[linkDataKey] = msg.Link
	}
	wire := wireMessage{
		Token:        msg.Token,
		Notification: wireNotification{Title: msg.Title, Body: msg.Body},
		Data:         data,
	}
	// Firebase refuses a web push link that is not https, which would fail
	// the whole message over a field only browsers read.
	if strings.HasPrefix(msg.Link, "https://") {
		wire.Webpush = &wireWebpush{FCMOptions: wireFCMOptions{Link: msg.Link}}
	}
	return envelope{Message: wire}
}
