package settings

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/features"
)

// variableName matches the string literals the server reads its environment
// by.
var variableName = regexp.MustCompile(`"((?:METIS|NOTIFICATION|OIDC)_[A-Z0-9_]+|DATABASE_URL|ENCRYPTION_KEY(?:_PREVIOUS)?|JWT_SECRET|WEBHOOK_ENDPOINTS)"`)

// notSettings are literals that look like variables and are not read as one:
// prefixes the readers build names from.
var notSettings = map[string]bool{"METIS_FEATURE_": true}

// Every variable the server reads is either changeable from the Settings page
// or listed as environment-only with a reason. A new variable that is neither
// fails here, so whether an administrator may change it is decided when it is
// added rather than by whoever notices the page does not show it.
func TestEveryVariableTheServerReadsIsClassified(t *testing.T) {
	known := map[string]string{}
	for _, d := range All() {
		known[d.Name] = "catalog"
	}
	for _, f := range EnvironmentOnly() {
		if where, twice := known[f.Name]; twice {
			t.Errorf("%s is both in the %s and environment-only", f.Name, where)
		}
		known[f.Name] = "environment-only"
	}
	for _, flag := range []features.Flag{features.StrictTenantScope, features.JavaScriptConditions} {
		if _, ok := known[features.EnvName(flag)]; !ok {
			t.Errorf("feature flag %s is not classified; feature flags are guardrails", features.EnvName(flag))
		}
	}

	root := filepath.Join("..", "..", "..")
	for _, dir := range []string{"internal", "server", "cmd/metis"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			source, err := os.ReadFile(path) // #nosec G304 -- the repository's own source
			if err != nil {
				return err
			}
			for _, match := range variableName.FindAllStringSubmatch(string(source), -1) {
				if name := match[1]; !notSettings[name] && known[name] == "" {
					t.Errorf("%s reads %s, which is neither in the catalog nor environment-only", path, name)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}

func TestEveryDefinitionIsComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range All() {
		if seen[d.Name] {
			t.Errorf("%s is defined twice", d.Name)
		}
		seen[d.Name] = true
		if d.Label == "" || d.Help == "" || d.Group == "" || d.Kind == "" || d.Applies == "" {
			t.Errorf("%s is missing its label, help, group, kind or when it applies", d.Name)
		}
		if (d.Kind == KindInteger || d.Kind == KindDays) && d.Min > d.Max {
			t.Errorf("%s has no room between its bounds", d.Name)
		}
		if d.Kind == KindDuration && d.MaxDuration <= d.MinDuration {
			t.Errorf("%s has no room between its duration bounds", d.Name)
		}
	}
	for _, f := range EnvironmentOnly() {
		if f.Reason == "" || f.Class == "" {
			t.Errorf("%s is environment-only without saying why", f.Name)
		}
	}
}

func TestValidate(t *testing.T) {
	port, _ := Find("NOTIFICATION_SMTP_PORT")
	lease, _ := Find("METIS_JOB_LEASE")
	issuer, _ := Find("OIDC_ISSUER")
	ratio, _ := Find("METIS_OTLP_SAMPLE_RATIO")
	collector, _ := Find("METIS_OTLP_ENDPOINT")
	days, _ := Find("METIS_RETENTION_FINISHED_INSTANCES_DAYS")
	host, _ := Find("NOTIFICATION_SMTP_HOST")

	cases := []struct {
		name  string
		def   Definition
		value string
		ok    bool
	}{
		{"a port", port, "587", true},
		{"a port out of range", port, "70000", false},
		{"not a number", port, "five", false},
		{"a lease", lease, "10m", true},
		// The server raises a shorter lease to 2m; the page refuses it, so
		// what is saved is what runs.
		{"a lease shorter than a call", lease, "30s", false},
		{"an issuer", issuer, "https://id.example.com/realms/metis", true},
		{"an issuer with credentials", issuer, "https://user:pw@id.example.com", false},
		{"an issuer that is not http", issuer, "ftp://id.example.com", false},
		{"a ratio", ratio, "0.25", true},
		{"a ratio over one", ratio, "1.5", false},
		{"a collector", collector, "collector:4318", true},
		{"a collector with no port", collector, "collector", false},
		{"retention off", days, "0", true},
		{"retention beyond the bound", days, "99999", false},
		{"a host", host, "smtp.example.com", true},
		{"empty", host, "", false},
		{"spaces around", host, " smtp.example.com", false},
		{"two lines", host, "smtp.example.com\nX-Injected: 1", false},
	}
	for _, c := range cases {
		err := c.def.Validate(c.value)
		if (err == nil) != c.ok {
			t.Errorf("%s: Validate(%q) = %v, want ok=%v", c.name, c.value, err, c.ok)
		}
	}
}
