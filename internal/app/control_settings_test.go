package app

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/observers/impl"
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
		"taking another administrator's role away, approving their own request and giving the role back; each change of " +
		"roles is recorded in the server's log, with who made it, and nowhere else. Name an organization only while it " +
		"has one administrator, and take it off the list once it has a second."
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

// The list is read once when the server starts, and that one reading is both
// what the startup log announces and what the services are built with: an
// operator who reads the log knows where the exception applies, and nothing
// the environment says afterwards makes the log untrue.
//
// With real accounts: ana is the only administrator of the organization. Her
// approval of her own request is let through exactly where the log said it
// would be — and when the waive she approved cannot move the instance on, the
// server's log says the approval that came to nothing was the requester's
// own.
func TestWhatStartUpAnnouncesAboutSoleAdministratorsIsWhatTheServicesEnforce(t *testing.T) {
	const setting = "METIS_SOLE_ADMINISTRATOR_ORGANIZATIONS"
	w := askForAWaive(t, map[string]any{"verdict": "maybe-next-quarter"})
	organization := entities.ActingOrganization(w.tenant)
	if err := w.app.svc.CreateUser(entities.WithSystemContext(t.Context()), entities.User{
		ID: w.account("ana"), Username: "ana", Roles: []string{entities.RoleAdmin}, Organizations: []*entities.Organization{{ID: organization}},
	}, "start-up-test-password"); err != nil {
		t.Fatalf("create ana's account: %v", err)
	}
	elsewhere := uuid.Must(uuid.NewV7()).String()
	started := func(list string) (announced []any) {
		t.Helper()
		logs := captureLogs(t)
		t.Setenv(setting, list)
		w.app.control = logFeatureConfiguration()
		w.app.svc = w.app.newServices(impl.NewEventDispatcher(), "retention-deviation-test")
		// Whatever the environment says from here on, the server has started.
		t.Setenv(setting, strings.Join([]string{organization.String(), elsewhere}, ","))
		for _, line := range logs.said("In each organization this setting names") {
			listed, _ := line["organizations"].([]any)
			announced = append(announced, listed...)
		}
		return announced
	}

	// Started with the organization not named: nothing says it is, and ana's
	// own approval waits.
	if announced := started(elsewhere); !reflect.DeepEqual(announced, []any{elsewhere}) {
		t.Fatalf("started naming another organization, the log announced %v", announced)
	}
	_, err := w.app.svc.ApproveDeviationRequest(w.as("ana"), w.request, "nobody else is here")
	if !errors.Is(err, apierr.ErrForbidden) || !strings.Contains(err.Error(), "so it waits") {
		t.Fatalf("ana approving her own request where the log did not name her organization: %v, want it to wait", err)
	}

	// Started with it named, beside an entry that names nobody: the log
	// announces it and only it, and ana's own approval is let through.
	if announced := started("Acme Ltd, " + organization.String()); !reflect.DeepEqual(announced, []any{organization.String()}) {
		t.Fatalf("started naming the organization, the log announced %v", announced)
	}
	logs := captureLogs(t)
	_, err = w.app.svc.ApproveDeviationRequest(w.as("ana"), w.request, "nobody else is here")
	if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "The request is still waiting") {
		t.Fatalf("ana approving a waive its gateway cannot follow: %v, want it refused with the request still waiting", err)
	}
	lines := logs.said("could not be applied")
	if len(lines) != 1 || lines[0]["level"] != "warn" || lines[0]["own_request"] != true || lines[0]["actor"] != "ana" ||
		!strings.HasPrefix(fmt.Sprint(lines[0]["message"]), "A waive its requester approved themselves could not be applied:") {
		t.Fatalf("the log holds %v, want one warning that says the approval that could not be applied was the requester's own", lines)
	}
	_, followable := w.another(t, map[string]any{"verdict": "accept"})
	approved, err := w.app.svc.ApproveDeviationRequest(w.as("ana"), followable, "nobody else is here")
	if err != nil || !approved.Applied || !approved.Request.SelfApproved() {
		t.Fatalf("ana approving her own request where the log named her organization: %+v, %v", approved.Request, err)
	}
}

// A second administrator's approval that cannot be applied is not said to be
// anybody's own.
func TestAnApprovalThatCouldNotBeAppliedIsNotCalledTheRequestersOwnWhenItWasNot(t *testing.T) {
	w := askForAWaive(t, map[string]any{"verdict": "maybe-next-quarter"})
	logs := captureLogs(t)
	if _, err := w.app.svc.ApproveDeviationRequest(w.as("budi"), w.request, ""); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("budi approving a waive its gateway cannot follow: %v", err)
	}
	lines := logs.said("could not be applied")
	if _, own := lines[0]["own_request"]; len(lines) != 1 || own ||
		!strings.HasPrefix(fmt.Sprint(lines[0]["message"]), "An approved waive could not be applied:") {
		t.Fatalf("the log holds %v, want one warning that calls it nobody's own", lines)
	}
}

// An id that is well formed and names no organization of this installation
// does nothing, and whoever wrote it believes it does something. It cannot be
// told until the database is up; once it is, it is said, once, as a warning
// that lists those ids. Ids that all name organizations are not mentioned.
func TestAnIdThatNamesNoOrganizationHereIsSaidOnceTheDatabaseIsUp(t *testing.T) {
	const setting = "METIS_SOLE_ADMINISTRATOR_ORGANIZATIONS"
	w := askForAWaive(t, map[string]any{"verdict": "accept"})
	organization := entities.ActingOrganization(w.tenant).String()
	stranger, another := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	const want = "These ids name no organization of this installation, so naming them does nothing. Check them against the " +
		"organizations' ids and take them off the list."
	for list, unknown := range map[string][]any{
		"":                            nil,
		organization:                  nil,
		organization + "," + stranger: {stranger},
		stranger + ", " + organization + "," + another: {stranger, another},
	} {
		t.Run(fmt.Sprintf("set to %.80s", list), func(t *testing.T) {
			t.Setenv(setting, list)
			settings := logFeatureConfiguration()
			logs := captureLogs(t)
			settings.warnOfOrganizationsThatDoNotExist(entities.WithSystemContext(t.Context()), w.app.repo.User())
			var said []map[string]any
			for _, line := range logs.said("") {
				if line["setting"] == setting {
					said = append(said, line)
				}
			}
			if len(unknown) == 0 {
				if len(said) != 0 {
					t.Fatalf("with every id naming an organization, the check said %v", said)
				}
				return
			}
			if len(said) != 1 || said[0]["level"] != "warn" || said[0]["message"] != want || said[0]["count"] != float64(len(unknown)) ||
				!reflect.DeepEqual(said[0]["organizations"], unknown) {
				t.Fatalf("the check said %v\nwant one warning listing %v that reads\n  %s", said, unknown, want)
			}
		})
	}
}
