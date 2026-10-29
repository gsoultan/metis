package tenant

import (
	"net/http"
	"testing"

	"github.com/gsoultan/metis/server/domains/services/impl"
	interceptortenant "github.com/gsoultan/metis/server/interceptors/tenant"
	"github.com/gsoultan/metis/server/transports/https/common"
)

// A caller the server knows, asking for an organization that is not theirs,
// is refused 403, not 401.
//
// A 401 means the token is no good. The UI signs out on one, and an
// integration signs in again and repeats the request — neither of which fixes
// the organization it named. An account in no organization at all wrapped no
// status and was a 500.
func TestAnOrganizationThatIsNotTheCallersIsForbidden(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"asked for an organization it is not a member of", interceptortenant.ErrNotAMember},
		{"named another organization in what it sent", impl.ErrForeignOrganization},
		{"belongs to no organization", interceptortenant.ErrNoTenantMembership},
	} {
		if got := common.CodeFrom(tc.err); got != http.StatusForbidden {
			t.Errorf("a caller who %s: got %d, want 403", tc.name, got)
		}
	}
}
