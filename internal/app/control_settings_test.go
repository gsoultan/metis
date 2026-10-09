package app

import (
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

// A setting that lets an administrator approve their own request is a control
// switched off for the organizations it applies to. Whoever inherits the
// installation reads the startup log, so it says so there — once, as a
// warning that names the setting and what it permits — and says nothing when
// it is off. Written so that it cannot be read it is off, and that is said
// too: whoever wrote it believes it is on.
func TestTheSoleAdministratorSettingIsAnnouncedAtBoot(t *testing.T) {
	const setting = "METIS_ALLOW_SOLE_ADMINISTRATOR_SELF_APPROVAL"
	if serviceimpl.EnvAllowSoleAdministratorSelfApproval != setting {
		t.Fatalf("the setting is named %s", serviceimpl.EnvAllowSoleAdministratorSelfApproval)
	}
	const on = "An administrator may approve their own request for a second administrator when nobody else administers " +
		"the organization, because this setting is on. Each such approval needs a reason and is recorded as approved by " +
		"nobody else. Appoint a second administrator, then turn it off."
	for raw, want := range map[string]string{
		"": "", "false": "", "0": "",
		"true": on, "1": on,
		"maybe":      setting + `="maybe" cannot be read as true or false; it is off`,
		"yes please": setting + `="yes please" cannot be read as true or false; it is off`,
		"true ":      setting + `="true " cannot be read as true or false; it is off`,
	} {
		t.Run("set to "+raw, func(t *testing.T) {
			logs := captureLogs(t)
			t.Setenv(setting, raw)
			logFeatureConfiguration()
			var said []map[string]any
			for _, line := range logs.said("") {
				if line["setting"] == setting {
					said = append(said, line)
				}
			}
			if want == "" {
				if len(said) != 0 {
					t.Fatalf("with %s=%q the startup log named it: %v", setting, raw, said)
				}
				return
			}
			if len(said) != 1 || said[0]["level"] != "warn" || said[0]["message"] != want {
				t.Fatalf("with %s=%q the startup log said %v; want one warning that reads\n  %s", setting, raw, said, want)
			}
		})
	}
}
