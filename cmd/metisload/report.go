package main

import (
	"cmp"
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

func (r *result) print(out io.Writer) error {
	w := &reportWriter{w: out}
	seconds := r.elapsed.Seconds()
	w.printf("\n%d virtual users for %s: %d requests approved end to end (%.0f/s)\n\n",
		r.users, r.elapsed.Round(time.Second), r.iterations, float64(r.iterations)/seconds)
	r.printLatency(w, seconds)
	r.printFailures(w)
	r.printServer(w)
	r.printStorage(w)
	return w.err
}

func (r *result) printLatency(w *reportWriter, seconds float64) {
	tabs := tabwriter.NewWriter(w.w, 0, 0, 2, ' ', tabwriter.AlignRight)
	table := &reportWriter{w: tabs}
	table.println("call\tok\tfailed\tper s\tp50 ms\tp95 ms\tp99 ms\tmax ms\t")
	for _, s := range r.summaries {
		table.printf("%s\t%d\t%d\t%.0f\t%.1f\t%.1f\t%.1f\t%.1f\t\n",
			s.operation, s.ok, s.failed, float64(s.ok)/seconds,
			ms(s.p50), ms(s.p95), ms(s.p99), ms(s.max))
	}
	if table.err == nil {
		table.err = tabs.Flush()
	}
	if w.err == nil {
		w.err = table.err
	}
}

func (r *result) printFailures(w *reportWriter) {
	if len(r.failures) == 0 {
		w.println("\nno call failed")
		return
	}
	w.println("\nfailures:")
	for _, class := range slices.Sorted(maps.Keys(r.failures)) {
		w.printf("  %6d  %s\n", r.failures[class], class)
	}
}

func (r *result) printServer(w *reportWriter) {
	if !r.hasServer {
		w.println("\nserver: metrics not read")
		return
	}
	s := r.server
	w.printf("\nserver: RSS %.0f → peak %.0f → %.0f MiB, Go heap peak %.0f MiB, goroutines peak %.0f\n",
		s.rssStart/mebibyte, s.rssPeak/mebibyte, s.rssEnd/mebibyte, s.heapPeak/mebibyte, s.goroutinesPeak)
	w.printf("        CPU %.1f s = %.2f cores average; %.0f waits for a database connection, %.1f s waiting in all\n",
		s.cpuSeconds, s.cpuCores, s.poolWaits, s.poolWaitSec)
	if r.iterations > 0 {
		w.printf("        %.1f ms of server CPU per approved request\n", s.cpuSeconds*1000/float64(r.iterations))
	}
}

func (r *result) printStorage(w *reportWriter) {
	if !r.hasStorage {
		w.println("\nstorage: not measured (pass -dsn)")
		return
	}
	grew := r.after.database - r.before.database
	w.printf("\nstorage: database %.1f → %.1f MiB (+%.1f MiB)",
		float64(r.before.database)/mebibyte, float64(r.after.database)/mebibyte, float64(grew)/mebibyte)
	if r.iterations > 0 {
		w.printf(", %.1f KiB per approved request", float64(grew)/1024/float64(r.iterations))
	}
	w.println("")
	for _, growth := range r.tableGrowth() {
		w.printf("  %+8.1f MiB  %s\n", float64(growth.bytes)/mebibyte, growth.table)
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
