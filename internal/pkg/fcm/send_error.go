package fcm

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Firebase's own error codes, from google.firebase.fcm.v1.FcmError.
const (
	codeUnregistered     = "UNREGISTERED"
	codeSenderIDMismatch = "SENDER_ID_MISMATCH"
	codeInvalidArgument  = "INVALID_ARGUMENT"
	codeThirdPartyAuth   = "THIRD_PARTY_AUTH_ERROR"
)

type errorAnswer struct {
	Error struct {
		Message string `json:"message"`
		Details []struct {
			ErrorCode string `json:"errorCode"`
		} `json:"details"`
	} `json:"error"`
}

// sendError turns Firebase's refusal into one of the package's errors.
func sendError(status int, body []byte) error {
	var answer errorAnswer
	if err := json.Unmarshal(body, &answer); err != nil {
		// An unreadable body still has a status to go on.
		answer = errorAnswer{}
	}
	code := ""
	for _, detail := range answer.Error.Details {
		if detail.ErrorCode != "" {
			code = detail.ErrorCode
			break
		}
	}
	reason := fmt.Sprintf("%d %s", status, answer.Error.Message)
	switch {
	// A token from another Firebase project can never be sent to from this
	// one, which for the caller is the same as a token that is gone.
	case code == codeUnregistered || code == codeSenderIDMismatch:
		return fmt.Errorf("%w (%s)", ErrUnregistered, reason)
	case code == codeThirdPartyAuth || status == http.StatusUnauthorized || status == http.StatusForbidden:
		return fmt.Errorf("%w (%s)", ErrCredentials, reason)
	case code == codeInvalidArgument || status == http.StatusBadRequest:
		return fmt.Errorf("%w (%s)", ErrRejected, reason)
	default:
		return fmt.Errorf("fcm: Firebase did not take the message (%s)", reason)
	}
}
