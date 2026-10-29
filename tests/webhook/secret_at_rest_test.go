package webhook_test

import (
	"strings"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/crypto"
	"github.com/gsoultan/metis/server/repositories/store/webhook"
	"github.com/gsoultan/metis/tests/testutils"
)

// The secret is what authenticates a delivery, so the column must not hold it:
// a backup or a read replica would otherwise be enough to sign deliveries as
// the partner. The sealed value still verifies what the partner signs.
func TestAWebhookSecretIsSealedAtRest(t *testing.T) {
	h := newWebhookHarness(t)
	hook := h.register(t, "order.paid", "")

	row, _, err := webhook.New().IDEq(hook.ID).One(t.Context(), testutils.Executor(h.db))
	if err != nil {
		t.Fatalf("read the stored secret: %v", err)
	}
	stored := row.Secret
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
	clear := webhook.MutateKey(hook.ID)
	clear.SetSecret(hook.Secret)
	if err := clear.Update(t.Context(), testutils.Executor(h.db)); err != nil {
		t.Fatalf("store the secret as an older release did: %v", err)
	}

	if _, err := h.deliver(t, hook, []byte(`{}`), "evt-1"); err != nil {
		t.Errorf("a webhook whose secret predates sealing refused a signed delivery: %v", err)
	}
}
