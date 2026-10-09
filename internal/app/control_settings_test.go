package app

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
)

// How long a request for a second administrator waits is a setting. Written
// so that it cannot be used as it stands — too short, too long, or not a
// duration — the server starts all the same and requests get a deadline, so
// the startup log says which one, once, as a warning. A setting that is
// usable, or not given, is not mentioned.
func TestAnApprovalWindowThatIsNotUsedAsWrittenIsSaidAtBoot(t *testing.T) {
	for raw, warnings := range map[string]int{"": 0, "96h": 0, "30m": 1, "2000h": 1, "soon": 1, "-5h": 1} {
		t.Run("set to "+raw, func(t *testing.T) {
			logs := captureLogs(t)
			t.Setenv(serviceimpl.EnvDeviationApprovalTTL, raw)
			logFeatureConfiguration()
			said := logs.aboutSetting(serviceimpl.EnvDeviationApprovalTTL)
			if len(said) != warnings {
				t.Fatalf("with %s=%q the startup log named it at %v; want %d warning(s)", serviceimpl.EnvDeviationApprovalTTL, raw, said, warnings)
			}
			for _, level := range said {
				if level != "warn" {
					t.Fatalf("with %s=%q the startup log named it at %v; want a warning", serviceimpl.EnvDeviationApprovalTTL, raw, said)
				}
			}
		})
	}
}

// Naming an organization where its only administrator may approve their own
// request switches a control off there. Whoever inherits the installation
// reads the startup log, so it says so — once, as a warning that names the
// setting, how many organizations and which, what that permits, what it does
// not prevent, and when to take an organization off the list — and says
// nothing when none is named. An entry that is not an id names nobody, and
// that is said too, quoted: whoever wrote it believes it names somebody.
func TestTheSoleAdministratorOrganizationsAreAnnouncedAtBoot(t *testing.T) {
	const setting = "METIS_SOLE_ADMINISTRATOR_ORGANIZATIONS"
	if serviceimpl.EnvSoleAdministratorOrganizations != setting {
		t.Fatalf("the setting is named %s", serviceimpl.EnvSoleAdministratorOrganizations)
	}
	const a, b = "0199aaaa-0000-7000-8000-00000000000a", "0199bbbb-0000-7000-8000-00000000000b"
	const named = "In each organization this setting names, an administrator may approve their own request for a second " +
		"administrator while nobody else administers that organization. Each such approval needs a reason and is recorded " +
		"as approved by nobody else. It does not stop an administrator of a named organization who can change roles from " +
		"taking another administrator's role away, approving their own request and giving the role back, and a change of " +
		"roles is not recorded with who made it. Name an organization only while it has one administrator, and take it " +
		"off the list once it has a second."
	notAnID := func(position int, entry string) string {
		return "entry " + strconv.Itoa(position) + " of " + setting + ", " + entry + ", is not an organization id, so it names no organization and is ignored"
	}
	var sixty []string
	var sixty50 []any
	for i := range 60 {
		id := fmt.Sprintf("0199cccc-0000-7000-8000-%012d", i)
		sixty = append(sixty, id)
		if i < 50 {
			sixty50 = append(sixty50, id)
		}
	}
	for raw, want := range map[string]struct {
		organizations []any
		problems      []string
		// count is how many organizations are named in all, when the line
		// does not list every one of them.
		count int
	}{
		"":                         {},
		" , ":                      {},
		a:                          {organizations: []any{a}},
		a + "," + b:                {organizations: []any{a, b}},
		"true":                     {problems: []string{notAnID(1, `"true"`)}},
		"Acme Ltd, " + b + ",0199": {organizations: []any{b}, problems: []string{notAnID(1, `"Acme Ltd"`), notAnID(3, `"0199"`)}},
		// The line's count is of every organization named; it lists the
		// first fifty.
		strings.Join(sixty, ","): {organizations: sixty50, count: 60},
	} {
		t.Run(fmt.Sprintf("set to %.80s", raw), func(t *testing.T) {
			logs := captureLogs(t)
			t.Setenv(setting, raw)
			logFeatureConfiguration()
			var announced []map[string]any
			var problems []string
			for _, line := range logs.said("") {
				if line["setting"] != setting {
					continue
				}
				if line["level"] != "warn" {
					t.Fatalf("with %s=%q the startup log named it at %v: %v", setting, raw, line["level"], line)
				}
				if _, lists := line["organizations"]; lists {
					announced = append(announced, line)
					continue
				}
				message, _ := line["message"].(string)
				problems = append(problems, message)
			}
			if !slices.Equal(problems, want.problems) {
				t.Fatalf("with %s=%q the startup log said of its entries\n  %q\nwant\n  %q", setting, raw, problems, want.problems)
			}
			if len(want.organizations) == 0 {
				if len(announced) != 0 {
					t.Fatalf("with %s=%q, which names no organization, the startup log announced %v", setting, raw, announced)
				}
				return
			}
			count := max(want.count, len(want.organizations))
			if len(announced) != 1 || announced[0]["message"] != named || announced[0]["count"] != float64(count) ||
				!reflect.DeepEqual(announced[0]["organizations"], want.organizations) {
				t.Fatalf("with %s=%q the startup log announced %v\nwant one warning naming %v that reads\n  %s", setting, raw, announced, want.organizations, named)
			}
		})
	}
}

// The exception was, for a while, a switch for the whole installation under
// another name. That name is not a setting: set, it turns nothing on. An
// operator who set it expecting the exception is told so at start-up, and
// told what is read instead — once, as a warning, and not at all when it is
// not set.
func TestTheSwitchThatIsNoLongerASettingIsSaidAtBoot(t *testing.T) {
	const retired, setting = "METIS_ALLOW_SOLE_ADMINISTRATOR_SELF_APPROVAL", "METIS_SOLE_ADMINISTRATOR_ORGANIZATIONS"
	const want = retired + " is set, and nothing reads it: it turns nothing on. An organization's only administrator may " +
		"approve their own request only where " + setting + " names the organization, by id."
	for raw, warned := range map[string]bool{"": false, "true": true, "false": true, "1": true, "yes please": true} {
		t.Run("set to "+raw, func(t *testing.T) {
			logs := captureLogs(t)
			t.Setenv(setting, "")
			t.Setenv(retired, raw)
			logFeatureConfiguration()
			var said []map[string]any
			for _, line := range logs.said("") {
				if line["setting"] == retired || line["setting"] == setting {
					said = append(said, line)
				}
			}
			if !warned {
				if len(said) != 0 {
					t.Fatalf("with %s not set the startup log said %v", retired, said)
				}
				return
			}
			if len(said) != 1 || said[0]["level"] != "warn" || said[0]["setting"] != retired || said[0]["message"] != want {
				t.Fatalf("with %s=%q the startup log said %v\nwant one warning that reads\n  %s", retired, raw, said, want)
			}
		})
	}
}
