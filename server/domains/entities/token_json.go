package entities

import "encoding/json"

// MarshalJSON is a token with its instance named by ID alone.
//
// A token the engine has just added points back at the live instance that
// holds it (NewToken), and that instance lists the token, so encoding either
// one walked the loop until the encoder gave up a thousand levels down with
// "encountered a cycle". Every event raised about an instance the engine had
// in hand — a step reached, a process started, a task created — failed to
// encode that way: never delivered to a browser or a webhook, and the most
// expensive thing a request did. A token read back from the database already
// carries its instance as a shell holding only the ID (InstanceEntityAdapter),
// so that shape is the one written for both.
func (t Token) MarshalJSON() ([]byte, error) {
	// Token's fields without its methods, so encoding it does not come back
	// here.
	type fields Token
	if t.Instance != nil {
		t.Instance = &ProcessInstance{ID: t.Instance.ID}
	}
	return json.Marshal(fields(t))
}
