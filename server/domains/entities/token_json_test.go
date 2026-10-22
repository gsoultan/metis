package entities

import (
	"encoding/json"
	"strings"
	"testing"
)

// liveInstance is an instance as the engine holds it mid-step: a token just
// added points back at the very instance that lists it.
func liveInstance() *ProcessInstance {
	instance := &ProcessInstance{ID: mustID(), Status: "active", Variables: map[string]any{"amount": 120}}
	instance.AddToken(&Node{ID: "review", Type: UserTask})
	return instance
}

// An event about an instance the engine has in hand must encode. It did not:
// the token's back-pointer made a loop the encoder walked until it gave up, so
// every step reached, process started and task created was dropped by the
// event stream and the webhooks alike.
func TestAnEventAboutALiveInstanceEncodes(t *testing.T) {
	instance := liveInstance()
	event := ProcessEvent{Type: EventNodeReached, Instance: instance, Timestamp: 1}

	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("an event about a live instance does not encode: %v", err)
	}
	var decoded struct {
		Instance struct {
			ID     string `json:"id"`
			Tokens []struct {
				Instance map[string]any `json:"instance"`
				Node     struct {
					ID string `json:"id"`
				} `json:"node"`
			} `json:"tokens"`
		} `json:"instance"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode %s: %v", payload, err)
	}
	if len(decoded.Instance.Tokens) != 1 {
		t.Fatalf("want the one token, got %s", payload)
	}
	token := decoded.Instance.Tokens[0]
	if token.Instance["id"] != instance.ID.String() || token.Node.ID != "review" {
		t.Fatalf("the token does not name its instance and node: %s", payload)
	}
	if _, nested := token.Instance["tokens"]; nested {
		t.Fatalf("the token carries its whole instance again: %s", payload)
	}
}

// A live token is written the way one read back from the database already is
// (InstanceEntityAdapter): its instance as a shell holding only the ID. A
// subscriber sees one shape whichever the engine happened to hold.
func TestALiveTokenEncodesLikeAStoredOne(t *testing.T) {
	instance := liveInstance()
	live := instance.Tokens[0]
	stored := live
	stored.Instance = &ProcessInstance{ID: instance.ID}

	got, err := json.Marshal(live)
	if err != nil {
		t.Fatalf("encode the live token: %v", err)
	}
	want, err := json.Marshal(stored)
	if err != nil {
		t.Fatalf("encode the stored token: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("live token encodes as\n%s\nstored one as\n%s", got, want)
	}
	if !strings.Contains(string(got), `"instance":{"id":"`+instance.ID.String()+`"`) {
		t.Fatalf("the token does not name its instance: %s", got)
	}
}

// Encoding must not touch the token itself: the engine goes on to persist and
// advance it, and it needs its back-pointer for that.
func TestEncodingATokenLeavesItsInstanceInPlace(t *testing.T) {
	instance := liveInstance()
	if _, err := json.Marshal(instance); err != nil {
		t.Fatalf("encode the instance: %v", err)
	}
	if instance.Tokens[0].Instance != instance {
		t.Fatal("encoding replaced the token's instance")
	}
}

// BenchmarkEncodeEventAboutLiveInstance is what the event stream and every
// webhook pay per event about a running instance.
func BenchmarkEncodeEventAboutLiveInstance(b *testing.B) {
	event := ProcessEvent{Type: EventNodeReached, Instance: liveInstance(), Timestamp: 1}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = json.Marshal(event)
	}
}
