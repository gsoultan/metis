package platformadmins

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// installation is a fixed number of organizations, or one that cannot say how
// many it has.
type installation struct {
	n   int64
	err error
}

func (i installation) CountOrganizations(context.Context) (int64, error) { return i.n, i.err }

// The operator's list names accounts by id. Anything else in it names nobody,
// and the nil id in particular must not name every caller without a local
// account, whose id reads as nil.
func TestOnlyAccountIDsNamePlatformAdministrators(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	got := Parse(" " + first.String() + ",,alice, " + uuid.Nil.String() + ",\t" + second.String() + " ")

	if len(got) != 2 {
		t.Fatalf("named %d accounts, want the two ids: %v", len(got), got)
	}
	for _, id := range []uuid.UUID{first, second} {
		if _, ok := got[id]; !ok {
			t.Errorf("%s is not named", id)
		}
	}
}

func TestWhoTheGateAdmits(t *testing.T) {
	named, other := uuid.New(), uuid.New()
	errUnreachable := errors.New("database unreachable")
	cases := []struct {
		name         string
		installation installation
		account      uuid.UUID
		admitted     bool
		err          error
	}{
		{"a named account on a shared installation", installation{n: 3}, named, true, nil},
		{"anybody else on a shared installation", installation{n: 2}, other, false, nil},
		{"somebody with no account, on a shared installation", installation{n: 2}, uuid.Nil, false, nil},
		{"anybody on an installation of one organization", installation{n: 1}, other, true, nil},
		{"an installation that cannot say whether it is shared", installation{err: errUnreachable}, other, false, errUnreachable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(Env, named.String())
			admitted, err := New(tc.installation).Admits(t.Context(), tc.account)
			if !errors.Is(err, tc.err) {
				t.Fatalf("got error %v, want %v", err, tc.err)
			}
			if admitted != tc.admitted {
				t.Fatalf("admitted: %v, want %v", admitted, tc.admitted)
			}
		})
	}
}
