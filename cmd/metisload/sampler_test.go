package main

import "testing"

func TestParseSeries(t *testing.T) {
	for _, tc := range []struct {
		line  string
		name  string
		value float64
		ok    bool
	}{
		{"process_resident_memory_bytes 1.2e+08", "process_resident_memory_bytes", 1.2e8, true},
		{`metis_db_pool_acquire_waits_total{pool="storm"} 15`, "metis_db_pool_acquire_waits_total", 15, true},
		// A label value may hold a space; the value is what follows the braces.
		{`metis_http_requests_total{route="a b"} 3`, "metis_http_requests_total", 3, true},
		{"# HELP go_goroutines Number of goroutines", "", 0, false},
		{"go_goroutines not-a-number", "go_goroutines", 0, false},
		{"", "", 0, false},
	} {
		name, value, ok := parseSeries(tc.line)
		if ok != tc.ok || (ok && (name != tc.name || value != tc.value)) {
			t.Errorf("parseSeries(%q) = %q, %v, %v; want %q, %v, %v", tc.line, name, value, ok, tc.name, tc.value, tc.ok)
		}
	}
}
