package main

import "time"

// serverSample is one reading of the server's gauges.
type serverSample struct {
	at     time.Time
	values map[string]float64
}
