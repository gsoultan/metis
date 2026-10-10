package impl

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The approval window is a setting, not a constant (spec, Slice 3b), and a
// value outside 1h–720h or one that cannot be read is said, never swallowed.
func TestTheApprovalWindowIsReadClampedAndSaid(t *testing.T) {
	cases := []struct {
		raw     string
		want    time.Duration
		problem string
	}{
		{"", 72 * time.Hour, ""},
		{"96h", 96 * time.Hour, ""},
		{"1h", time.Hour, ""},
		{"720h", 720 * time.Hour, ""},
		{"30m", time.Hour, "shorter than 1h"},
		{"2000h", 720 * time.Hour, "longer than 720h"},
		{"soon", 72 * time.Hour, "cannot be read"},
		{"-5h", 72 * time.Hour, "cannot be read"},
		{"0", 72 * time.Hour, "cannot be read"},
	}
	for _, c := range cases {
		t.Setenv(EnvDeviationApprovalTTL, c.raw)
		got, problem := DeviationApprovalTTL()
		if got != c.want || (c.problem == "") != (problem == "") || !strings.Contains(problem, c.problem) {
			t.Errorf("%q: got %v %q, want %v containing %q", c.raw, got, problem, c.want, c.problem)
		}
		if problem != "" && !strings.Contains(problem, EnvDeviationApprovalTTL) {
			t.Errorf("%q: the problem %q does not name the setting", c.raw, problem)
		}
		if problem != "" && !strings.Contains(problem, "requests wait "+hoursOf(c.want)) {
			t.Errorf("%q: the problem %q does not say how long a request then waits", c.raw, problem)
		}
	}
}

// The organizations where an only administrator may approve their own request
// are named by id, in a list, by whoever operates the installation — and by
// nothing else. Unset or empty names none. An entry that is not an id names
// none, is said — quoted, so that whoever wrote it can find it — and takes
// nothing from the entries beside it. "true" is not an id: the exception has
// no switch that turns it on everywhere.
func TestOnlyOrganizationsNamedByIdMayHaveASoleAdministratorApprove(t *testing.T) {
	a, b := uuid.MustParse("0199aaaa-0000-7000-8000-00000000000a"), uuid.MustParse("0199bbbb-0000-7000-8000-00000000000b")
	notAnID := func(position int, entry string) string {
		// What is echoed of an entry is its first 64 characters.
		if len(entry) > 64 {
			entry = entry[:64]
		}
		return fmt.Sprintf("entry %d of %s, %s, is not an organization id, so it names no organization and is ignored",
			position, EnvSoleAdministratorOrganizations, strconv.Quote(entry))
	}
	cases := []struct {
		raw      string
		named    []uuid.UUID
		problems []string
	}{
		{raw: ""},
		{raw: "   "},
		{raw: " , ,, "},
		{raw: a.String(), named: []uuid.UUID{a}},
		{raw: "  " + a.String() + " ,\t" + b.String() + "\n", named: []uuid.UUID{a, b}},
		{raw: strings.ToUpper(a.String()), named: []uuid.UUID{a}},
		{raw: a.String() + "," + a.String() + "," + b.String() + "," + a.String(), named: []uuid.UUID{a, b}},
		{raw: "true", problems: []string{notAnID(1, "true")}},
		{raw: "1", problems: []string{notAnID(1, "1")}},
		{raw: "*", problems: []string{notAnID(1, "*")}},
		{raw: "all", problems: []string{notAnID(1, "all")}},
		{raw: "Acme Ltd," + a.String(), named: []uuid.UUID{a}, problems: []string{notAnID(1, "Acme Ltd")}},
		{raw: a.String() + ", acme ,," + b.String() + ",0199", named: []uuid.UUID{a, b},
			problems: []string{notAnID(2, "acme"), notAnID(5, "0199")}},
		// The nil id is no organization: named, it would name every request
		// that is for none.
		{raw: uuid.Nil.String() + "," + b.String(), named: []uuid.UUID{b}, problems: []string{notAnID(1, uuid.Nil.String())}},
		// Two ids run together by a space, or a semicolon, are one entry and
		// not an id.
		{raw: a.String() + " " + b.String(), problems: []string{notAnID(1, a.String()+" "+b.String())}},
		{raw: a.String() + ";" + b.String(), problems: []string{notAnID(1, (a.String() + ";" + b.String()))}},
	}
	for _, c := range cases {
		t.Setenv(EnvSoleAdministratorOrganizations, c.raw)
		named, problems := SoleAdministratorOrganizations()
		if !slices.Equal(named, c.named) || !slices.Equal(problems, c.problems) {
			t.Errorf("%q:\n  named %v\n  said  %q\nwant\n  named %v\n  said  %q", c.raw, named, problems, c.named, c.problems)
		}
	}
	// What is echoed is bounded: an entry nobody could have meant is not
	// written out whole.
	t.Setenv(EnvSoleAdministratorOrganizations, strings.Repeat("y", 500))
	if _, problems := SoleAdministratorOrganizations(); len(problems) != 1 || !strings.Contains(problems[0], `, "`+strings.Repeat("y", 64)+`", is not`) {
		t.Errorf("an entry of 500 characters is echoed as %q, want its first 64", problems)
	}
	if EnvSoleAdministratorOrganizations != "METIS_SOLE_ADMINISTRATOR_ORGANIZATIONS" {
		t.Errorf("the setting is named %s", EnvSoleAdministratorOrganizations)
	}
}

// A setting that weakens a control has one name. The spelling from before the
// rename, which nearly every other METIS_* setting still answers to, names
// nothing here; nor does anything that merely resembles the name.
func TestTheSoleAdministratorOrganizationsHaveOneName(t *testing.T) {
	organization := uuid.Must(uuid.NewV7()).String()
	for _, other := range []string{"GOBPM_SOLE_ADMINISTRATOR_ORGANIZATIONS", "METIS_SOLE_ADMINISTRATOR_ORGANIZATION",
		"SOLE_ADMINISTRATOR_ORGANIZATIONS", "metis_sole_administrator_organizations"} {
		for _, value := range []string{organization, "true"} {
			// Not set at all — set to nothing would hide a fallback, which
			// is taken only for a name that is absent. (Setenv first, so
			// that the test puts back what was there.)
			t.Setenv(EnvSoleAdministratorOrganizations, "")
			if err := os.Unsetenv(EnvSoleAdministratorOrganizations); err != nil {
				t.Fatalf("unset the setting: %v", err)
			}
			t.Setenv(other, value)
			if named, problems := SoleAdministratorOrganizations(); len(named) != 0 || len(problems) != 0 {
				t.Errorf("%s=%s: named %v, said %q; want nothing named and nothing said — it is not this setting", other, value, named, problems)
			}
			t.Setenv(other, "")
		}
	}
}

// Where a sole administrator may approve their own request is given to the
// service when it is built, and to nothing else: a service built without
// being told allows it nowhere, whatever the environment says then or later,
// and the approval of a waive asks the same rule the service was given.
func TestTheServiceAllowsASoleAdministratorOnlyWhereItIsBuiltTo(t *testing.T) {
	a, b, c := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	t.Setenv(EnvSoleAdministratorOrganizations, a.String()+","+b.String()+","+c.String())
	built := func(options ...DeviationRequestOption) *deviationRequestService {
		service, ok := NewDeviationRequestService(nil, nil, options...).(*deviationRequestService)
		if !ok {
			t.Fatal("the service of requests is not the one this package builds")
		}
		return service
	}
	for name, tc := range map[string]struct {
		service *deviationRequestService
		allowed []uuid.UUID
	}{
		"built with nothing said, the environment naming three": {built(), nil},
		"built naming none":                    {built(WithSoleAdministratorOrganizations(nil)), nil},
		"built naming one":                     {built(WithSoleAdministratorOrganizations([]uuid.UUID{a})), []uuid.UUID{a}},
		"built naming two, among other things": {built(WithSweepLockWait(time.Second), WithSoleAdministratorOrganizations([]uuid.UUID{a, b})), []uuid.UUID{a, b}},
		// No organization is not an organization somebody can be the only
		// administrator of.
		"built naming no organization": {built(WithSoleAdministratorOrganizations([]uuid.UUID{uuid.Nil})), nil},
	} {
		for _, rules := range []approvalRules{tc.service.rules, tc.service.waives.rules} {
			for _, organization := range []uuid.UUID{a, b, c, uuid.Nil} {
				if got, want := rules.allowsSoleAdministratorOf(organization), slices.Contains(tc.allowed, organization); got != want {
					t.Errorf("%s: a sole administrator of %s may approve: %v, want %v", name, organization, got, want)
				}
			}
		}
	}
	// The list a service was given is its own: changing the caller's slice
	// afterwards names nobody new.
	given := []uuid.UUID{a}
	service := built(WithSoleAdministratorOrganizations(given))
	given[0] = b
	if service.rules.allowsSoleAdministratorOf(b) || !service.waives.rules.allowsSoleAdministratorOf(a) {
		t.Error("the service's list changed with its caller's slice")
	}
	if asking, ok := NewInstanceDeviationService(nil, nil).(*instanceDeviationService); !ok || asking.rules.allowsSoleAdministratorOf(a) {
		t.Error("the service that asks for a waive was built allowing a sole administrator to approve; it approves nothing, and is told nothing")
	}
}
