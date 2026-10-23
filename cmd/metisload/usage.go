package main

// usage is what the run cost the server, from the first sample to the last.
type usage struct {
	samples                int
	rssStart, rssPeak      float64
	rssEnd, heapPeak       float64
	goroutinesPeak         float64
	cpuSeconds, cpuCores   float64
	poolWaits, poolWaitSec float64
}
