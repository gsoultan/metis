package impl

import (
	"context"
	"errors"
	"net/http"
	"strings"

	panmail "github.com/gsoultan/panmail-sdk"

	"github.com/gsoultan/metis/internal/pkg/httpclient"
	"github.com/gsoultan/metis/server/domains/services/impl/connectors"
)

// PanmailSettings is a Panmail gateway the installation's own mail —
// notifications and People's task links — is sent through instead of an SMTP
// server. It takes the same three things a panmail-email connection does,
// with the From address shared with SMTP.
type PanmailSettings struct {
	// URL is the gateway's origin, such as https://mail.example.com. Set, it
	// chooses Panmail.
	URL string
	// APIKey needs the email:send scope.
	APIKey string
	// ProviderID is the gateway's sending provider the mail goes out through.
	ProviderID string
}

// Chosen reports whether the installation sends its mail through Panmail.
func (p PanmailSettings) Chosen() bool { return strings.TrimSpace(p.URL) != "" }

// panmailMailRetries is how many times a send whose outcome is unknown is
// asked again. Every send carries a key, and the gateway answers a repeated
// key with the message it already accepted, so a repeat finds out what
// happened rather than mailing the person twice.
const panmailMailRetries = 2

// newPanmailTransport builds the client once, for every message: the gateway
// address and key are checked here, at start, rather than on the first task.
func newPanmailTransport(settings EmailSettings, client *http.Client) (mailTransport, error) {
	if client == nil {
		// The shared client, so the gateway is reached under the same egress
		// policy as every other outbound call.
		client = httpclient.Shared()
	}
	gateway, err := panmail.New(strings.TrimSpace(settings.Panmail.URL), settings.Panmail.APIKey,
		panmail.WithHTTPClient(client),
		panmail.WithTransportRetries(panmailMailRetries))
	if err != nil {
		return nil, err
	}
	providerID := settings.Panmail.ProviderID
	from := settings.From
	return func(ctx context.Context, m outgoingMail) error {
		// A line break in an address would reach a header. The person's
		// address comes from the directory, where it is checked, but the
		// gateway is not trusted to refuse one in every version of it.
		if strings.ContainsAny(m.to, "\r\n") || strings.ContainsAny(from, "\r\n") {
			return errAddressLineBreak
		}
		_, err := gateway.Send(ctx, panmail.Message{
			ProviderID:     providerID,
			From:           from,
			To:             []string{m.to},
			Subject:        m.subject,
			Text:           m.text,
			IdempotencyKey: m.key,
		})
		if err != nil {
			return connectors.ExplainPanmailError(err)
		}
		return nil
	}, nil
}

// errAddressLineBreak is a sender or recipient address holding a line break.
var errAddressLineBreak = errors.New("an address contains a line break")
