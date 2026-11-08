package connectors

import (
	"errors"
	"fmt"

	panmail "github.com/gsoultan/panmail-sdk"
)

// panmailCodeInvalidArgument is the gateway's refusal of a request it could
// not act on, which for a send is nearly always the provider or the sender.
const panmailCodeInvalidArgument = "invalid_argument"

// explainPanmailError adds what to do about the refusals somebody can fix,
// because the job's incident is where an administrator reads it, and
// "unauthenticated" alone does not say which setting is wrong.
func explainPanmailError(err error) error {
	var auth *panmail.AuthError
	var suppressed *panmail.SuppressedRecipientError
	var backlog *panmail.BacklogFullError
	var apiErr *panmail.APIError
	switch {
	case errors.As(err, &auth):
		return fmt.Errorf("%w (check the API key, and that it has the email:send scope)", err)
	case errors.As(err, &suppressed):
		return fmt.Errorf("%w (lift the suppression in Panmail, or remove the address from the recipients)", err)
	case errors.As(err, &backlog):
		return fmt.Errorf("%w (Panmail is behind on this tenant's mail; retry once its queue drains)", err)
	case errors.As(err, &apiErr) && apiErr.Code == panmailCodeInvalidArgument:
		return fmt.Errorf("%w (check the provider id, and that the From address is an allowed sender for that provider)", err)
	}
	return err
}
