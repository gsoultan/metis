// Command metisload puts a running Metis server under concurrent load and
// reports what it cost: throughput, latency per call, the server's memory and
// CPU, and how much the database grew.
//
// It runs against a real server over HTTP, out of process, so what it measures
// is what a client sees — the interceptor chain, JSON, the connection pool, the
// job worker running alongside — and the server's resident memory is its own,
// not shared with the generator's.
//
//	go run ./cmd/metisload -users 1000 -duration 60s \
//	    -user admin -password admin \
//	    -dsn 'postgres://metis:metis@localhost:5432/metis?sslmode=disable'
//
// docs/performance.md, "A thousand concurrent clients", says what each number
// means and gives the last measured run.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"
)

type options struct {
	base, metrics, dsn string
	username, password string
	projectID          string
	users              int
	duration, timeout  time.Duration
	think              time.Duration
}

func parseOptions() options {
	var o options
	flag.StringVar(&o.base, "url", "http://localhost:8273", "the server's API address")
	flag.StringVar(&o.metrics, "metrics", "http://127.0.0.1:9464/metrics", "the server's metrics endpoint; empty to skip")
	flag.StringVar(&o.dsn, "dsn", "", "the server's PostgreSQL database, to measure storage growth; empty to skip")
	flag.StringVar(&o.username, "user", "admin", "who the virtual users sign in as")
	flag.StringVar(&o.password, "password", os.Getenv("METISLOAD_PASSWORD"), "their password (or METISLOAD_PASSWORD)")
	flag.StringVar(&o.projectID, "project", "", "the project to run in; the user's first when empty")
	flag.IntVar(&o.users, "users", 1000, "concurrent virtual users")
	flag.DurationVar(&o.duration, "duration", time.Minute, "how long to keep the load on")
	flag.DurationVar(&o.timeout, "timeout", 30*time.Second, "how long one call may take before it counts as failed")
	flag.DurationVar(&o.think, "think", 0, "how long a virtual user waits between its calls; 0 keeps every user's next call in flight at once")
	flag.Parse()
	return o
}

func main() {
	if err := run(parseOptions()); err != nil {
		fmt.Fprintln(os.Stderr, "metisload:", err)
		os.Exit(1)
	}
}

func run(o options) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	transport := newTransport(o.users)
	setup := &client{http: &http.Client{Transport: transport, Timeout: o.timeout}, base: o.base}
	if err := prepare(ctx, setup, &o); err != nil {
		return err
	}
	probe, err := openStorageProbe(ctx, o.dsn)
	if err != nil {
		return err
	}
	defer probe.close(context.Background())

	result, err := measure(ctx, o, setup.token, transport, probe)
	if err != nil {
		return err
	}
	result.print(os.Stdout)
	return nil
}

// prepare signs in, picks the project and files the load definition.
func prepare(ctx context.Context, setup *client, o *options) error {
	if err := setup.signIn(ctx, o.username, o.password); err != nil {
		return err
	}
	if o.projectID == "" {
		projectID, err := setup.firstProject(ctx)
		if err != nil {
			return err
		}
		o.projectID = projectID
	}
	return setup.importDefinition(ctx, o.projectID, o.username)
}
