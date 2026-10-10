package app

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestAPanickingGRPCCallFailsAlone: grpc-go does not recover a handler's
// panic, and the server was built with no interceptor that did, so one nil
// dereference behind the gRPC port ended the process.
func TestAPanickingGRPCCallFailsAlone(t *testing.T) {
	srv := grpc.NewServer(grpcRecovery()...)
	srv.RegisterService(&grpc.ServiceDesc{
		ServiceName: "test.Panics",
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "Now",
			Handler: func(_ any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
				in := new(emptypb.Empty)
				if err := dec(in); err != nil {
					return nil, err
				}
				handler := func(context.Context, any) (any, error) {
					var p *struct{ n int }
					return p.n, nil // nil dereference
				}
				if interceptor == nil {
					return handler(ctx, in)
				}
				return interceptor(ctx, in, &grpc.UnaryServerInfo{FullMethod: "/test.Panics/Now"}, handler)
			},
		}},
	}, struct{}{})

	lis := bufconn.Listen(1 << 20)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	for range 2 { // the second call proves the server is still up
		err = conn.Invoke(t.Context(), "/test.Panics/Now", &emptypb.Empty{}, &emptypb.Empty{})
		if status.Code(err) != codes.Internal {
			t.Fatalf("err = %v, want codes.Internal", err)
		}
	}
}
