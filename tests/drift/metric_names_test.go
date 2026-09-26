package drift_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// An alert or a dashboard panel on a metric nothing exports parses, passes its
// promtool test — which feeds the rule series of whatever name the rule asks
// for — and never shows anything. So every name the rules and the dashboard
// read has to be one the code declares, or one a recording rule produces.
func TestEveryMetricTheAlertsAndTheDashboardReadIsExported(t *testing.T) {
	repo := os.DirFS(filepath.Join("..", ".."))

	declared := map[string]bool{}
	literal := regexp.MustCompile(`"(metis_[a-z0-9_]+)"`)
	for _, root := range []string{"cmd", "internal", "server"} {
		err := fs.WalkDir(repo, root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			source, err := fs.ReadFile(repo, path)
			if err != nil {
				return err
			}
			for _, match := range literal.FindAllStringSubmatch(string(source), -1) {
				declared[match[1]] = true
			}
			return nil
		})
		if err != nil {
			t.Fatalf("read %s: %v", root, err)
		}
	}

	rulesFile, err := fs.ReadFile(repo, "deploy/kubernetes/alerts.yaml")
	if err != nil {
		t.Fatalf("read the alerts: %v", err)
	}
	var rules struct {
		Spec struct {
			Groups []struct {
				Rules []struct {
					Record string `yaml:"record"`
					Alert  string `yaml:"alert"`
					Expr   string `yaml:"expr"`
				} `yaml:"rules"`
			} `yaml:"groups"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(rulesFile, &rules); err != nil {
		t.Fatalf("parse the alerts: %v", err)
	}
	recorded := map[string]bool{}
	reads := map[string][]string{} // expression → where it is
	for _, group := range rules.Spec.Groups {
		for _, rule := range group.Rules {
			if rule.Record != "" {
				recorded[rule.Record] = true
			}
			reads[rule.Expr] = append(reads[rule.Expr], "alerts.yaml "+rule.Record+rule.Alert)
		}
	}

	dashboardFile, err := fs.ReadFile(repo, "deploy/grafana/metis-slo.json")
	if err != nil {
		t.Fatalf("read the dashboard: %v", err)
	}
	var dashboard struct {
		Panels []struct {
			Title   string `json:"title"`
			Targets []struct {
				Expr string `json:"expr"`
			} `json:"targets"`
		} `json:"panels"`
	}
	if err := json.Unmarshal(dashboardFile, &dashboard); err != nil {
		t.Fatalf("parse the dashboard: %v", err)
	}
	for _, panel := range dashboard.Panels {
		for _, target := range panel.Targets {
			reads[target.Expr] = append(reads[target.Expr], "dashboard panel "+panel.Title)
		}
	}

	name := regexp.MustCompile(`metis[_:][a-z0-9_:]+`)
	checked := 0
	for expr, where := range reads {
		for _, metric := range name.FindAllString(expr, -1) {
			checked++
			if strings.Contains(metric, ":") {
				if !recorded[metric] {
					t.Errorf("%v reads %s, which no recording rule produces", where, metric)
				}
				continue
			}
			base := metric
			for _, suffix := range []string{"_bucket", "_sum", "_count"} {
				base = strings.TrimSuffix(base, suffix)
			}
			if !declared[base] {
				t.Errorf("%v reads %s, which no code exports", where, metric)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no metric names were read; the files' shape has changed and this test checks nothing")
	}
}
