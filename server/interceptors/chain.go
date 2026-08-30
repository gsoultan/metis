package interceptors

import (
	"github.com/go-kit/kit/endpoint"
	"github.com/gsoultan/metis/server/interceptors/contracts"
)

// InterceptorChain manages a collection of interceptors and applies them to an endpoint.
type InterceptorChain struct {
	interceptors []contracts.EndpointInterceptor
}

func NewInterceptorChain(interceptors ...contracts.EndpointInterceptor) *InterceptorChain {
	return &InterceptorChain{interceptors: interceptors}
}

func (c *InterceptorChain) Apply(e endpoint.Endpoint) endpoint.Endpoint {
	for i := len(c.interceptors) - 1; i >= 0; i-- {
		e = c.interceptors[i].Intercept(e)
	}
	return e
}
