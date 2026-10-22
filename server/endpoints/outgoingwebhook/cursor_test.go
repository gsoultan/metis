package outgoingwebhook

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

// A page of the log ends where the next one starts, to the nanosecond: two
// deliveries a microsecond apart are not the same place.
func TestACursorComesBackAsTheDeliveryItNamed(t *testing.T) {
	at := time.Date(2026, 10, 22, 3, 1, 14, 123456789, time.UTC)
	want := entities.OutgoingWebhookLogCursor{CreatedAt: at, ID: uuid.New()}
	got, err := decodeCursor(encodeCursor(want))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || got.ID != want.ID {
		t.Errorf("cursor came back as %+v, want %+v", got, want)
	}
}

func TestACursorThatIsNotOneIsRefused(t *testing.T) {
	for _, token := range []string{"x", "bm90IGEgY3Vyc29y", encodeCursor(entities.OutgoingWebhookLogCursor{})[:4]} {
		if _, err := decodeCursor(token); !errors.Is(err, apierr.ErrInvalidArgument) {
			t.Errorf("decodeCursor(%q) = %v, want it refused", token, err)
		}
	}
}

func TestAPageIsBounded(t *testing.T) {
	for asked, want := range map[int]int{0: defaultLogPage, -3: defaultLogPage, 10: 10, 10_000: maxLogPage} {
		if got := logPage(asked); got != want {
			t.Errorf("logPage(%d) = %d, want %d", asked, got, want)
		}
	}
}
