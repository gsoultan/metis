// Package machine is the Servers page's endpoints: the processes running this
// installation.
package machine

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services"
)

// Endpoints list the machines running this installation and forget the ones
// that stopped.
type Endpoints struct {
	ListMachines  endpoint.Endpoint
	ForgetMachine endpoint.Endpoint
}

// IDRequest names one machine by the address.
type IDRequest struct {
	ID string `json:"-"`
}

// ListResponse is every machine, with the server's own time: how long ago one
// was heard from is reckoned against it, not the browser's clock.
type ListResponse struct {
	Machines []entities.Machine `json:"machines"`
	Now      time.Time          `json:"now"`
	Err      error              `json:"err,omitzero"`
}

func (r ListResponse) Failed() error { return r.Err }

// ForgetResponse says whether the machine was removed.
type ForgetResponse struct {
	Err error `json:"err,omitzero"`
}

func (r ForgetResponse) Failed() error { return r.Err }

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		ListMachines: func(ctx context.Context, _ any) (any, error) {
			machines, err := s.ListMachines(ctx)
			if err != nil {
				return ListResponse{Err: err}, nil
			}
			return ListResponse{Machines: machines, Now: time.Now().UTC()}, nil
		},
		ForgetMachine: func(ctx context.Context, request any) (any, error) {
			req, ok := request.(IDRequest)
			if !ok {
				return nil, fmt.Errorf("machine: expected an IDRequest, got %T", request)
			}
			id, err := uuid.Parse(req.ID)
			if err != nil {
				return ForgetResponse{Err: apierr.Invalidf("%q is not a machine id", req.ID)}, nil
			}
			return ForgetResponse{Err: s.ForgetMachine(ctx, id)}, nil
		},
	}
}
