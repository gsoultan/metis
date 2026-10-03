package deviation

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// The reply's shape does not depend on what a row happens to hold: before,
// after and details are objects even when the row has none, and whether the
// server acted is said either way.
func TestAViewAlwaysCarriesItsObjectsAndWhoActed(t *testing.T) {
	t.Parallel()
	body, err := json.Marshal(ViewOf(entities.Deviation{Actor: "System"}))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	for _, want := range []string{`"before":{}`, `"after":{}`, `"details":{}`, `"actor_is_server":true`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the view has no %s: %s", want, body)
		}
	}
}

// The server is the actor exactly when the row names no account. The name
// decides nothing: an account may be called System.
func TestAViewSaysTheServerActedOnlyWhenNoAccountDid(t *testing.T) {
	t.Parallel()
	account := uuid.Must(uuid.NewV7())
	cases := []struct {
		name string
		row  entities.Deviation
		want bool
	}{
		{"nobody signed in", entities.Deviation{Actor: "System"}, true},
		{"an account named System", entities.Deviation{Actor: "System", ActorID: account}, false},
		{"an account", entities.Deviation{Actor: "dita", ActorID: account}, false},
	}
	for _, c := range cases {
		if got := ViewOf(c.row).ActorIsServer; got != c.want {
			t.Errorf("%s: actor_is_server = %v, want %v", c.name, got, c.want)
		}
	}
}
