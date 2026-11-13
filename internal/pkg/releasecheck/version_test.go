package releasecheck

import "testing"

func TestParseTag(t *testing.T) {
	tests := []struct {
		tag string
		ok  bool
	}{
		{"v1.4.2", true},
		{"v1.4.2-rc.1", true},
		{"v2.0.0+build.5", true},
		{"1.4.2", false},
		{"dev", false},
		{"v1.4", false},
		{"v01.4.2", false},
		{"v1.4.2-3-gabc1234", false},
		{"v1.4.2-3-gabc1234-dirty", false},
	}
	for _, tt := range tests {
		if _, ok := parseTag(tt.tag); ok != tt.ok {
			t.Errorf("parseTag(%q) ok = %v, want %v", tt.tag, ok, tt.ok)
		}
	}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"v0.9.0", "v0.8.2", 1},
		{"v0.10.0", "v0.9.9", 1},
		{"v1.0.0", "v1.0.0", 0},
		{"v1.0.0-rc.1", "v1.0.0", -1},
		{"v1.0.0-rc.2", "v1.0.0-rc.10", -1},
		{"v1.0.0-alpha", "v1.0.0-1", 1},
		{"v1.0.0-rc", "v1.0.0-rc.1", -1},
		{"v1.0.0+a", "v1.0.0+b", 0},
	}
	for _, tt := range tests {
		a, _ := parseTag(tt.a)
		b, _ := parseTag(tt.b)
		if got := compare(a, b); got != tt.want {
			t.Errorf("compare(%s, %s) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}
