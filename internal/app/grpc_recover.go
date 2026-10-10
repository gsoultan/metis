package app

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/gsoultan/metis/internal/pkg/redaction"
)

// grpcRecovery is the gRPC twin of https.RecoverPanics. grpc-go does not
// recover a handler's panic, so without this one nil dereference anywhere
// behind the gRPC port ended the process instead of failing one call.
func grpcRecovery() []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
			defer recoverGRPC(info.FullMethod, &err)
			return handler(ctx, req)
		}),
		grpc.ChainStreamInterceptor(func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
			defer recoverGRPC(info.FullMethod, &err)
			return handler(srv, ss)
		}),
	}
}

func recoverGRPC(method string, err *error) {
	recovered := recover()
	if recovered == nil {
		return
	}
	log.Error().
		Str("method", method).
		Str("panic", redaction.RedactText(fmt.Sprint(recovered))).
		Bytes("stack", debug.Stack()).
		Msg("Panic serving a gRPC call; returning Internal")
	*err = status.Error(codes.Internal, "internal error")
}
