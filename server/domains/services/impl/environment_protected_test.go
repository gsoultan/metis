package impl

import "testing"

func TestTheMainRuntimeIsProtectedOnlyWhenSaidSo(t *testing.T) {
	cases := []struct {
		value     string
		protected bool
		fails     bool
	}{
		{"", false, false},
		{"false", false, false},
		{"true", true, false},
		{" TRUE ", true, false},
		{"yes please", false, true},
	}
	for _, c := range cases {
		t.Setenv(EnvProtectedRuntime, c.value)
		got, err := MainRuntimeProtected()
		if (err != nil) != c.fails {
			t.Fatalf("%q: error %v, want failure=%v", c.value, err, c.fails)
		}
		if got != c.protected {
			t.Fatalf("%q: protected=%v, want %v", c.value, got, c.protected)
		}
	}
}
