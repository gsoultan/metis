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
