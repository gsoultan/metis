package main

import (
	"cmp"
	"fmt"
	"io"
	"maps"
	"slices"
	"text/tabwriter"
	"time"
)

const (
	mebibyte        = 1 << 20
	tablesInReport  = 6
	millisecondUnit = float64(time.Millisecond)
)

func (r *result) print(w io.Writer) {
	seconds := r.elapsed.Seconds()
	fmt.Fprintf(w, "\n%d virtual users for %s: %d requests approved end to end (%.0f/s)\n\n",
		r.users, r.elapsed.Round(time.Second), r.iterations, float64(r.iterations)/seconds)
	r.printLatency(w, seconds)
	r.printFailures(w)
	r.printServer(w)
	r.printStorage(w)
}

func (r *result) printLatency(w io.Writer, seconds float64) {
	table := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(table, "call\tok\tfailed\tper s\tp50 ms\tp95 ms\tp99 ms\tmax ms\t")
	for _, s := range r.summaries {
		fmt.Fprintf(table, "%s\t%d\t%d\t%.0f\t%.1f\t%.1f\t%.1f\t%.1f\t\n",
			s.operation, s.ok, s.failed, float64(s.ok)/seconds,
			ms(s.p50), ms(s.p95), ms(s.p99), ms(s.max))
	}
	_ = table.Flush()
}

func (r *result) printFailures(w io.Writer) {
	if len(r.failures) == 0 {
		fmt.Fprintln(w, "\nno call failed")
		return
	}
	fmt.Fprintln(w, "\nfailures:")
	for _, class := range slices.Sorted(maps.Keys(r.failures)) {
		fmt.Fprintf(w, "  %6d  %s\n", r.failures[class], class)
	}
}

func (r *result) printServer(w io.Writer) {
	if !r.hasServer {
		fmt.Fprintln(w, "\nserver: metrics not read")
		return
	}
	s := r.server
	fmt.Fprintf(w, "\nserver: RSS %.0f → peak %.0f → %.0f MiB, Go heap peak %.0f MiB, goroutines peak %.0f\n",
		s.rssStart/mebibyte, s.rssPeak/mebibyte, s.rssEnd/mebibyte, s.heapPeak/mebibyte, s.goroutinesPeak)
	fmt.Fprintf(w, "        CPU %.1f s = %.2f cores average; %.0f waits for a database connection, %.1f s waiting in all\n",
		s.cpuSeconds, s.cpuCores, s.poolWaits, s.poolWaitSec)
	if r.iterations > 0 {
		fmt.Fprintf(w, "        %.1f ms of server CPU per approved request\n", s.cpuSeconds*1000/float64(r.iterations))
	}
}

func (r *result) printStorage(w io.Writer) {
	if !r.hasStorage {
		fmt.Fprintln(w, "\nstorage: not measured (pass -dsn)")
		return
	}
	grew := r.after.database - r.before.database
	fmt.Fprintf(w, "\nstorage: database %.1f → %.1f MiB (+%.1f MiB)",
		float64(r.before.database)/mebibyte, float64(r.after.database)/mebibyte, float64(grew)/mebibyte)
	if r.iterations > 0 {
		fmt.Fprintf(w, ", %.1f KiB per approved request", float64(grew)/1024/float64(r.iterations))
	}
	fmt.Fprintln(w)
	for _, growth := range r.tableGrowth() {
		fmt.Fprintf(w, "  %+8.1f MiB  %s\n", float64(growth.bytes)/mebibyte, growth.table)
	}
}

func (r *result) tableGrowth() []tableGrowth {
	var out []tableGrowth
	for table, size := range r.after.tables {
		if grew := size - r.before.tables[table]; grew > 0 {
			out = append(out, tableGrowth{table: table, bytes: grew})
		}
	}
	slices.SortFunc(out, func(a, b tableGrowth) int { return cmp.Compare(b.bytes, a.bytes) })
	return out[:min(len(out), tablesInReport)]
}

func ms(d time.Duration) float64 {
	return float64(d) / millisecondUnit
}
