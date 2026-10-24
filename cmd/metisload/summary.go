package main

import "time"

// summary is one operation's line in the report.
type summary struct {
	operation string
	ok        int
	failed    int
	p50, p95  time.Duration
	p99, max  time.Duration
}
