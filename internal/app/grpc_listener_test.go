package app

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/endpoints"
	"github.com/gsoultan/metis/server/transports/grpcs"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
)

// gRPC applies none of the HTTP chain — no authentication, rate or body
// limit — and it used to listen on :8081 unless told otherwise, published by
// the image, compose and the Kubernetes manifest. Unset now means no listener.
func TestTheGRPCListenerIsOffUnlessAnAddressIsGiven(t *testing.T) {
	t.Setenv(envGRPCAddress, "")

	g, ctx := errgroup.WithContext(t.Context())
	(&App{}).serveGRPC(ctx, g, nil)

	waited := make(chan error, 1)
	go func() { waited <- g.Wait() }()
	select {
	case err := <-waited:
		if err != nil {
			t.Fatalf("nothing should have been started, and nothing should have failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("a gRPC listener is running although no address was given")
	}
}

// Asked for, it still serves — the opt-in is a real switch, not a removal.
func TestTheGRPCListenerServesWhereAsked(t *testing.T) {
	var listenConfig net.ListenConfig
	probe, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find a free port: %v", err)
	}
	address := probe.Addr().String()
	if err := probe.Close(); err != nil {
		t.Fatalf("free the port: %v", err)
	}
	t.Setenv(envGRPCAddress, address)

	ctx, cancel := context.WithCancel(t.Context())
	g, groupCtx := errgroup.WithContext(ctx)
	(&App{}).serveGRPC(groupCtx, g, grpcs.NewGRPCServer(endpoints.Endpoints{}))

	dialer := net.Dialer{Timeout: 100 * time.Millisecond}
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, dialErr := dialer.DialContext(t.Context(), "tcp", address)
		if dialErr == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("nothing is listening on %s although it was asked for: %v", address, dialErr)
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	if err := g.Wait(); err != nil {
		t.Fatalf("the listener did not stop cleanly: %v", err)
	}
}

// A shutdown that arrives after the listener is bound but before the server
// starts serving is still a shutdown. Serve answers it with ErrServerStopped,
// which the server reported as a failure of the gRPC listener: the race step
// of the gate caught it once in a few hundred runs of the test above.
func TestAGRPCServerStoppedBeforeItServesStopsCleanly(t *testing.T) {
	var listenConfig net.ListenConfig
	lis, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := grpc.NewServer()
	server.GracefulStop()

	if err := serveGRPCUntilStopped(server, lis); err != nil {
		t.Fatalf("a server stopped before it served reported %v", err)
	}
}
