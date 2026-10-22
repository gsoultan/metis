package webhook_test

import (
	"strings"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/crypto"
)

// The secret is what authenticates a delivery, so the column must not hold it:
// a backup or a read replica would otherwise be enough to sign deliveries as
// the partner. The sealed value still verifies what the partner signs.
func TestAWebhookSecretIsSealedAtRest(t *testing.T) {
	h := newWebhookHarness(t)
	hook := h.register(t, "order.paid", "")

	var stored string
	if err := h.db.WithContext(t.Context()).Raw(
		`SELECT secret FROM webhooks WHERE id = ?`, hook.ID).Row().Scan(&stored); err != nil {
		t.Fatalf("read the stored secret: %v", err)
	}
	if !crypto.IsCiphertext(stored) || strings.Contains(stored, hook.Secret) {
		t.Fatalf("the webhook's secret is stored in the clear: %q", stored)
	}

	if _, err := h.deliver(t, hook, []byte(`{}`), "evt-1"); err != nil {
		t.Errorf("a delivery signed with the secret was refused once it was sealed: %v", err)
	}
}

// A replica from before sealing writes the secret in the clear while an
// upgrade rolls out. That webhook keeps working until migration 42 seals it.
func TestAWebhookSecretWrittenInTheClearStillVerifies(t *testing.T) {
	h := newWebhookHarness(t)
	hook := h.register(t, "order.paid", "")
	if err := h.db.WithContext(t.Context()).Exec(
		`UPDATE webhooks SET secret = ? WHERE id = ?`, hook.Secret, hook.ID).Error; err != nil {
		t.Fatalf("store the secret as an older release did: %v", err)
	}

	if _, err := h.deliver(t, hook, []byte(`{}`), "evt-1"); err != nil {
		t.Errorf("a webhook whose secret predates sealing refused a signed delivery: %v", err)
	}
}
