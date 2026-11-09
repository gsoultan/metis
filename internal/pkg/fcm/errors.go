package fcm

import (
	"errors"
	"net/http"

	"github.com/rs/zerolog/log"
)

var (
	// ErrUnregistered says the device token is no longer valid: the app was
	// uninstalled, or the token was rotated. The token should be forgotten;
	// sending to it again will never work.
	ErrUnregistered = errors.New("fcm: the device is no longer registered")
	// ErrCredentials says Google refused the service account, or the account
	// may not send for this Firebase project.
	ErrCredentials = errors.New("fcm: Firebase refused the service account")
	// ErrRejected says Firebase refused this one message as malformed.
	ErrRejected = errors.New("fcm: Firebase refused the message")
)

func closeBody(resp *http.Response) {
	if err := resp.Body.Close(); err != nil {
		log.Debug().Err(err).Msg("fcm: could not close a response body")
	}
}
