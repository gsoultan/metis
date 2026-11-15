package impl

import (
	"errors"
	"fmt"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/impl/connectors"
)

func TestTheMatcherCatchesAFailureByTheCodeItNames(t *testing.T) {
	boundary := func(code string) entities.Node {
		return entities.Node{Type: entities.BoundaryEvent, ErrorCode: code}
	}
	testCases := []struct {
		name     string
		err      error
		boundary entities.Node
		want     bool
	}{
		{"a manifest failure by its code", &connectors.ManifestError{Code: "CARD_DECLINED"}, boundary("CARD_DECLINED"), true},
		{"a wrapped manifest failure by its code",
			fmt.Errorf("connector execution failed: %w", &connectors.ManifestError{Code: "CARD_DECLINED"}), boundary("CARD_DECLINED"), true},
		{"a manifest failure under another code", &connectors.ManifestError{Code: "CARD_DECLINED"}, boundary("RATE_LIMITED"), false},
		{"a catchable error by its code", &entities.CatchableError{Code: "X"}, boundary("X"), true},
		{"an uncoded failure under a code", errors.New("boom"), boundary("X"), false},
		{"anything under a bare boundary", errors.New("boom"), boundary(""), true},
	}
	matcher := NewErrorBoundaryMatcher()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matcher.Matches(tc.err, tc.boundary); got != tc.want {
				t.Errorf("Matches = %v, want %v", got, tc.want)
			}
		})
	}
}
