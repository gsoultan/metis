package entities

import (
	"encoding/json"
	"testing"
)

// Every webhook endpoint and every browser on the event stream is sent an
// event as its JSON. Who else a withdrawal concerns is for the notifier: a
// subscriber's payload for a withdrawal is what it was.
func TestAWithdrawalsOwnerIsNotPartOfWhatSubscribersAreSent(t *testing.T) {
	event := ProcessEvent{Type: EventTaskCanceled, Timestamp: 1, Assignee: "dita"}
	want, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("encode the event: %v", err)
	}
	event.Owner = "ollie"
	got, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("encode the event with an owner: %v", err)
	}
	if string(got) != string(want) || string(got) != `{"type":"TaskCanceled","timestamp":1,"assignee":"dita"}` {
		t.Fatalf("a withdrawal with an owner is sent as %s; without one it is %s", got, want)
	}
}
