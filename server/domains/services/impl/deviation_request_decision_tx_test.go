package impl

import (
	"errors"
	"fmt"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// A write a decision makes under the request it holds keeps its cause for
// whoever asks what it was — "somebody decided this first" is still found —
// except "not found": the request was found, so that cause is kept as words
// and is not answered as a request that does not exist.
func TestAWriteThatFailsUnderAHeldRequestIsNeverAnsweredAsNotFound(t *testing.T) {
	t.Parallel()
	missing := writeFailed("recording the approval of request x", fmt.Errorf("%w: no such deviation", apierr.ErrNotFound))
	if errors.Is(missing, apierr.ErrNotFound) || missing.Error() != "recording the approval of request x: not found: no such deviation" {
		t.Fatalf("a row gone under a held request is answered %q, as not found = %v; want the server's failure, in words", missing, errors.Is(missing, apierr.ErrNotFound))
	}
	for name, cause := range map[string]error{
		"decided first":     repocontracts.ErrDeviationRequestDecided,
		"row decided first": repocontracts.ErrDeviationRowDecided,
		"anything else":     errors.New("the database is away"),
	} {
		if failed := writeFailed("closing request x as stale", cause); !errors.Is(failed, cause) {
			t.Errorf("%s: the cause is lost under %q", name, failed)
		}
	}
}
