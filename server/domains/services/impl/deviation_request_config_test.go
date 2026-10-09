package impl

import (
	"strconv"
	"strings"
	"testing"
	"time"
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

// The setting that lets a sole administrator approve their own request is an
// exception to a control, so it is on for a value that says true and for
// nothing else: absent, empty, false, or anything that has to be guessed at
// is off — and what could not be read is said, naming the setting and what
// was written, because whoever wrote it believes it is on.
func TestOnlyAValueThatReadsAsTrueTurnsOnSoleAdministratorSelfApproval(t *testing.T) {
	on := []string{"true", "TRUE", "True", "t", "T", "1"}
	off := []string{"", "false", "FALSE", "False", "f", "F", "0"}
	unreadable := []string{"yes", "yes please", "on", "enabled", "y", "2", "-1", "TrUe", "tRUE", " true", "true ", "true\n", "\"true\"",
		"'true'", "true,false", "truee", "１", "ｔｒｕｅ", "null", "allow"}
	for _, raw := range on {
		t.Setenv(EnvAllowSoleAdministratorSelfApproval, raw)
		if allowed, problem := AllowSoleAdministratorSelfApproval(); !allowed || problem != "" {
			t.Errorf("%q: allowed %v, problem %q; want it on with nothing to say", raw, allowed, problem)
		}
	}
	for _, raw := range off {
		t.Setenv(EnvAllowSoleAdministratorSelfApproval, raw)
		if allowed, problem := AllowSoleAdministratorSelfApproval(); allowed || problem != "" {
			t.Errorf("%q: allowed %v, problem %q; want it off with nothing to say", raw, allowed, problem)
		}
	}
	for _, raw := range unreadable {
		t.Setenv(EnvAllowSoleAdministratorSelfApproval, raw)
		allowed, problem := AllowSoleAdministratorSelfApproval()
		// Quoted, so that a space after "true" is seen by whoever reads it.
		if want := EnvAllowSoleAdministratorSelfApproval + "=" + strconv.Quote(raw) + " cannot be read as true or false; it is off"; allowed || problem != want {
			t.Errorf("%q: allowed %v, problem %q; want it off and %q", raw, allowed, problem, want)
		}
	}
	// What is echoed is bounded: a value nobody could have meant is not
	// written out whole.
	t.Setenv(EnvAllowSoleAdministratorSelfApproval, strings.Repeat("y", 500))
	if _, problem := AllowSoleAdministratorSelfApproval(); !strings.Contains(problem, `="`+strings.Repeat("y", 64)+`" cannot be read`) {
		t.Errorf("a value of 500 characters is echoed as %q, want its first 64", problem)
	}
	if EnvAllowSoleAdministratorSelfApproval != "METIS_ALLOW_SOLE_ADMINISTRATOR_SELF_APPROVAL" {
		t.Errorf("the setting is named %s", EnvAllowSoleAdministratorSelfApproval)
	}
}

// Whether a sole administrator may approve their own request is given to the
// service when it is built, and to nothing else: a service built without
// being told refuses, whatever the environment says then or later, and the
// approval of a waive asks the same rule the service was given.
func TestTheServiceAllowsASoleAdministratorOnlyWhenItIsBuiltTo(t *testing.T) {
	t.Setenv(EnvAllowSoleAdministratorSelfApproval, "true")
	built := func(options ...DeviationRequestOption) *deviationRequestService {
		service, ok := NewDeviationRequestService(nil, nil, options...).(*deviationRequestService)
		if !ok {
			t.Fatal("the service of requests is not the one this package builds")
		}
		return service
	}
	for name, c := range map[string]struct {
		service *deviationRequestService
		want    bool
	}{
		"built with nothing said, the environment saying true": {built(), false},
		"built to refuse":                    {built(WithSoleAdministratorSelfApproval(false)), false},
		"built to allow":                     {built(WithSoleAdministratorSelfApproval(true)), true},
		"built to allow, among other things": {built(WithSweepLockWait(time.Second), WithSoleAdministratorSelfApproval(true)), true},
	} {
		if c.service.rules.allowSole != c.want || c.service.waives.rules.allowSole != c.want {
			t.Errorf("%s: the service allows %v and its approval of a waive %v, want %v for both",
				name, c.service.rules.allowSole, c.service.waives.rules.allowSole, c.want)
		}
	}
	if asking, ok := NewInstanceDeviationService(nil, nil).(*instanceDeviationService); !ok || asking.rules.allowSole {
		t.Error("the service that asks for a waive was built allowing a sole administrator to approve; it approves nothing, and is told nothing")
	}
}
