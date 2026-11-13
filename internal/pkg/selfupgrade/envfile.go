package selfupgrade

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
)

// ParseEnvFile reads the KEY=VALUE lines of a systemd EnvironmentFile, such
// as /etc/metis/metis.env, so the upgrade reads the database the service
// reads. Blank lines and # comments are skipped; one pair of matching quotes
// around a value is removed, as systemd removes it.
func ParseEnvFile(raw []byte) (map[string]string, error) {
	out := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for n := 1; scanner.Scan(); n++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" || strings.ContainsAny(key, " \t") {
			return nil, fmt.Errorf("line %d is not KEY=VALUE", n)
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		out[key] = value
	}
	return out, scanner.Err()
}
