package process

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/services"
)

// messagingFacade records which way a message went. Every other method of the
// facade is nil and panics if reached.
type messagingFacade struct {
	services.ServiceFacade
	sent      []string
	started   []string
	starts    int
	startsErr error
}

func (f *messagingFacade) SendMessage(_ context.Context, _ uuid.UUID, name, key string, _ map[string]any) error {
	f.sent = append(f.sent, name+"/"+key)
	return nil
}

func (f *messagingFacade) StartFromMessage(_ context.Context, _ uuid.UUID, name string, _ map[string]any) (int, error) {
	f.started = append(f.started, name)
	return f.starts, f.startsErr
}

func sendMessage(t *testing.T, f *messagingFacade, key string) error {
	t.Helper()
	out, err := MakeSendMessageEndpoint(f)(t.Context(), SendMessageRequest{
		ProjectID:      uuid.NewString(),
		MessageName:    "payment-received",
		CorrelationKey: key,
		Variables:      map[string]any{"approved": true},
	})
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	return out.(SendMessageResponse).Err
}

func TestAMessageWithNoCorrelationKeyReachesNoWaitingInstance(t *testing.T) {
	for _, key := range []string{"", "   "} {
		f := &messagingFacade{starts: 1}
		if err := sendMessage(t, f, key); err != nil {
			t.Fatalf("key %q: %v", key, err)
		}
		if len(f.sent) != 0 {
			t.Fatalf("key %q: an uncorrelated message was delivered to waiting instances: %v", key, f.sent)
		}
		if len(f.started) != 1 {
			t.Fatalf("key %q: the processes that start on the message were not started", key)
		}
	}
}

func TestAnUncorrelatedMessageNothingStartsOnIsRefused(t *testing.T) {
	f := &messagingFacade{starts: 0}
	err := sendMessage(t, f, "")
	if !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("err %v, want invalid argument", err)
	}
	if len(f.sent) != 0 {
		t.Fatalf("delivered to waiting instances: %v", f.sent)
	}
}

func TestAStartThatFailsIsReported(t *testing.T) {
	boom := errors.New("boom")
	f := &messagingFacade{startsErr: boom}
	if err := sendMessage(t, f, ""); !errors.Is(err, boom) {
		t.Fatalf("err %v, want the start's failure", err)
	}
}

func TestACorrelatedMessageReachesItsInstance(t *testing.T) {
	f := &messagingFacade{}
	if err := sendMessage(t, f, "order-42"); err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 1 || f.sent[0] != "payment-received/order-42" || len(f.started) != 0 {
		t.Fatalf("sent %v started %v", f.sent, f.started)
	}
}
