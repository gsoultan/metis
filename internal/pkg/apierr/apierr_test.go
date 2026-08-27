package apierr

import (
	"errors"
	"fmt"
	"testing"
)

func TestInvalidfIsRecognisableAsInvalidArgument(t *testing.T) {
	err := Invalidf("project id %q is not a UUID", "banana")
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("an Invalidf error is not recognisable as ErrInvalidArgument, so a transport would report it as a server failure")
	}
}

// The caller has to be told which field was wrong; "invalid argument" alone
// leaves them guessing.
func TestInvalidfKeepsTheDetail(t *testing.T) {
	err := Invalidf("project id %q is not a UUID", "banana")
	if got := err.Error(); got != `invalid argument: project id "banana" is not a UUID` {
		t.Fatalf("message = %q, which does not name the field or the value", got)
	}
}

// Wrapping must survive another layer, since endpoints add their own context
// before the transport sees it.
func TestInvalidArgumentSurvivesFurtherWrapping(t *testing.T) {
	wrapped := fmt.Errorf("listing instances: %w", Invalidf("bad id"))
	if !errors.Is(wrapped, ErrInvalidArgument) {
		t.Fatal("wrapping lost the classification")
	}
}

func TestDistinctKinds(t *testing.T) {
	if errors.Is(ErrNotFound, ErrInvalidArgument) {
		t.Fatal("not-found and invalid-argument must not be interchangeable")
	}
}
