package pg

import "testing"

func TestContainsPatternEscapesWhatLikeTreatsSpecially(t *testing.T) {
	cases := []struct {
		search  string
		pattern string
		ok      bool
	}{
		{search: "", ok: false},
		{search: "   ", ok: false},
		{search: "Discount", pattern: "%Discount%", ok: true},
		{search: "  gold  ", pattern: "%gold%", ok: true},
		{search: "50%", pattern: `%50\%%`, ok: true},
		{search: "risk_band", pattern: `%risk\_band%`, ok: true},
		{search: `a\b`, pattern: `%a\\b%`, ok: true},
	}
	for _, c := range cases {
		pattern, ok := containsPattern(c.search)
		if ok != c.ok || pattern != c.pattern {
			t.Errorf("containsPattern(%q) = %q, %v; want %q, %v", c.search, pattern, ok, c.pattern, c.ok)
		}
	}
}
