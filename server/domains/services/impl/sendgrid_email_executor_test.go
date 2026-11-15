package impl

import (
	"strings"
	"testing"
)

// A send with no key or no recipient used to go to SendGrid, fail there, and
// be retried as if SendGrid had been at fault.
func TestSendGridSaysWhatIsMissingBeforeCallingSendGrid(t *testing.T) {
	_, err := (&SendGridEmailExecutor{}).Execute(t.Context(),
		map[string]any{"from_email": "noreply@example.com"},
		map[string]any{"subject": "PO-17", "content": "Approve it"})
	if err == nil {
		t.Fatal("a send with no API key and no recipient was attempted")
	}
	for _, want := range []string{"the API key", "the To email"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to name %s", err, want)
		}
	}
	if strings.Contains(err.Error(), "sendgrid.com") {
		t.Errorf("SendGrid was called: %v", err)
	}
}
