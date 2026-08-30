package logging

import (
	"context"
	"time"

	"github.com/go-kit/kit/endpoint"
	"github.com/gsoultan/metis/server/interceptors/contracts"
	"github.com/rs/zerolog/log"
)

type failer interface {
	Failed() error
}

type loggingInterceptor struct {
	method string
}

func NewLoggingInterceptor(method string) contracts.EndpointInterceptor {
	return &loggingInterceptor{method: method}
}

func (i *loggingInterceptor) Intercept(next endpoint.Endpoint) endpoint.Endpoint {
	return func(ctx context.Context, request any) (response any, err error) {
		defer func(begin time.Time) {
			var endpointErr error
			if err != nil {
				endpointErr = err
			} else if f, ok := response.(failer); ok {
				endpointErr = f.Failed()
			}

			event := log.Info()
			if endpointErr != nil {
				event = log.Error().Err(endpointErr)
			}

			event.
				Str("method", i.method).
				Dur("took", time.Since(begin)).
				Msg("endpoint called")
		}(time.Now())
		return next(ctx, request)
	}
}
