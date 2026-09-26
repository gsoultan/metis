package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"testing"
)

// The setting that brings back the old rule for tasks nobody was named for —
// anybody signed in may claim and complete one — reopens what the rule closed.
// So the startup log says when it is on: once, at boot, where whoever inherits
// the installation will read it, and not at all when it is off.
func TestTheLegacyRuleForTasksNobodyWasNamedForIsAnnouncedAtBoot(t *testing.T) {
	const setting = "METIS_ALLOW_UNASSIGNED_TASK_CLAIMS"
	logs := captureLogs(t)

	t.Setenv(setting, "")
	logFeatureConfiguration()
	if said := logs.aboutSetting(setting); len(said) != 0 {
		t.Fatalf("with %s off, the startup log named it at %v", setting, said)
	}

	t.Setenv(setting, "true")
	logFeatureConfiguration()
	if said := logs.aboutSetting(setting); len(said) != 1 || said[0] != "warn" {
		t.Fatalf("with %s on, the startup log named it at %v; want one warning", setting, said)
	}
}

// aboutSetting returns the level of each kept line that names the setting.
func (l *logTap) aboutSetting(setting string) []string {
	l.mu.Lock()
	kept := bytes.Clone(l.kept.Bytes())
	l.mu.Unlock()

	var levels []string
	scanner := bufio.NewScanner(bytes.NewReader(kept))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var line struct {
			Level   string `json:"level"`
			Setting string `json:"setting"`
		}
		if json.Unmarshal(scanner.Bytes(), &line) == nil && line.Setting == setting {
			levels = append(levels, line.Level)
		}
	}
	return levels
}
