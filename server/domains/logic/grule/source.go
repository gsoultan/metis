package grule

import (
	"fmt"
	"path"
	"strings"
)

// Source is one rule file handed to an import.
type Source struct {
	Name    string
	Content []byte
}

// ParseSources parses every file of an import, GRL or Grule JSON by its
// extension, or by its first character when the extension says neither.
func ParseSources(sources []Source) ([]Rule, error) {
	if len(sources) == 0 {
		return nil, fmt.Errorf("no rule files were given")
	}
	if len(sources) > MaxFiles {
		return nil, fmt.Errorf("%d files were given; an import may hold at most %d", len(sources), MaxFiles)
	}
	total := 0
	var rules []Rule
	for _, source := range sources {
		total += len(source.Content)
		if total > MaxTotalBytes {
			return nil, fmt.Errorf("the files come to more than %d bytes, the most one import may hold", MaxTotalBytes)
		}
		parsed, err := parseSource(source)
		if err != nil {
			return nil, err
		}
		rules = append(rules, parsed...)
		if len(rules) > MaxRules {
			return nil, fmt.Errorf("the files hold more than %d rules, the most one import may hold", MaxRules)
		}
	}
	return rules, nil
}

func parseSource(source Source) ([]Rule, error) {
	switch strings.ToLower(path.Ext(source.Name)) {
	case ".json":
		return ParseJSON(source.Name, source.Content)
	case ".grl":
		return ParseGRL(source.Name, string(source.Content))
	}
	trimmed := strings.TrimSpace(string(source.Content))
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return ParseJSON(source.Name, source.Content)
	}
	return ParseGRL(source.Name, string(source.Content))
}
