package distribution

import "testing"

func TestMetisNamesWhatThisRepositoryPublishes(t *testing.T) {
	checks := map[string]string{
		Metis.ReleasesURL():            "https://api.github.com/repos/gsoultan/metis/releases?per_page=30",
		Metis.DownloadBase():           "https://github.com/gsoultan/metis/releases/download/",
		Metis.WorkflowIdentityPrefix(): "https://github.com/gsoultan/metis/.github/workflows/release.yml@refs/tags/",
		Metis.Binary():                 "/usr/bin/metis",
		Metis.SystemdUnit():            "/usr/lib/systemd/system/metis.service",
		Metis.OpenRCScript():           "/etc/init.d/metis",
		Metis.EnvFile():                "/etc/metis/metis.env",
		Metis.StateDir():               "/var/lib/metis",
		Metis.UpgradeTriggerEnabled():  "/etc/systemd/system/paths.target.wants/metis-upgrade.path",
	}
	for got, want := range checks {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	if err := Metis.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAnEmbeddingProgramGetsItsOwnNames(t *testing.T) {
	server := Distribution{Name: "metis-server", Repository: "gsoultan/metis-server"}
	if err := server.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := server.ReleasesURL(); got != "https://api.github.com/repos/gsoultan/metis-server/releases?per_page=30" {
		t.Errorf("ReleasesURL = %q", got)
	}
	if got := server.EnvFile(); got != "/etc/metis-server/metis-server.env" {
		t.Errorf("EnvFile = %q", got)
	}
}

func TestValidateRefusesWhatIsNotAName(t *testing.T) {
	for _, d := range []Distribution{
		{},
		{Name: "metis", Repository: "gsoultan"},
		{Name: "metis", Repository: "gsoultan/metis/extra"},
		{Name: "metis", Repository: "../evil/metis"},
		{Name: "metis", Repository: "gsoultan/metis?x=1"},
		{Name: "metis", Repository: "gsoultan/.."},
		{Name: "Metis", Repository: "gsoultan/metis"},
		{Name: "metis;rm", Repository: "gsoultan/metis"},
		{Name: "../metis", Repository: "gsoultan/metis"},
		{Name: "-metis", Repository: "gsoultan/metis"},
	} {
		if err := d.Validate(); err == nil {
			t.Errorf("Validate(%+v) = nil, want an error", d)
		}
	}
}
