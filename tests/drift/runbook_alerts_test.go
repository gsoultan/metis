package drift_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// docs/runbooks.md promises that every rule in deploy/kubernetes/alerts.yaml
// has an entry there, and alerts send whoever is paged to a named section of
// it. Both were kept by hand. An alert whose runbook is missing, or whose
// pointer names a section that was renamed, is found out at 3am.
func TestEveryAlertLeadsToARunbookThatExists(t *testing.T) {
	repo := os.DirFS(filepath.Join("..", ".."))
	rulesFile, err := fs.ReadFile(repo, "deploy/kubernetes/alerts.yaml")
	if err != nil {
		t.Fatalf("read the alerts: %v", err)
	}
	var rules struct {
		Spec struct {
			Groups []struct {
				Rules []struct {
					Alert       string            `yaml:"alert"`
					Annotations map[string]string `yaml:"annotations"`
				} `yaml:"rules"`
			} `yaml:"groups"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(rulesFile, &rules); err != nil {
		t.Fatalf("parse the alerts: %v", err)
	}
	runbookFile, err := fs.ReadFile(repo, "docs/runbooks.md")
	if err != nil {
		t.Fatalf("read the runbooks: %v", err)
	}
	runbooks := string(runbookFile)

	headings := map[string]bool{}
	for line := range strings.Lines(runbooks) {
		if strings.HasPrefix(line, "#") {
			headings[strings.ReplaceAll(strings.TrimSpace(strings.TrimLeft(line, "#")), "`", "")] = true
		}
	}
	pointer := regexp.MustCompile(`docs/runbooks\.md, "([^"]+)"`)

	alerts := 0
	for _, group := range rules.Spec.Groups {
		for _, rule := range group.Rules {
			if rule.Alert == "" {
				continue // a recording rule
			}
			alerts++
			if !strings.Contains(runbooks, "| `"+rule.Alert+"` |") {
				t.Errorf("%s has no row in the runbooks' alert table", rule.Alert)
			}
			for _, text := range rule.Annotations {
				for _, match := range pointer.FindAllStringSubmatch(strings.Join(strings.Fields(text), " "), -1) {
					if !headings[match[1]] {
						t.Errorf("%s sends the reader to %q, which is not a section of docs/runbooks.md", rule.Alert, match[1])
					}
				}
			}
		}
	}
	if alerts == 0 {
		t.Fatal("no alerts were read; the file's shape has changed and this test checks nothing")
	}
}
