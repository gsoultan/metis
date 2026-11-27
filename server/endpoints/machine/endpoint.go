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
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

// Endpoints list the machines running this installation and forget the ones
// that stopped.
type Endpoints struct {
	ListMachines      endpoint.Endpoint
	ForgetMachine     endpoint.Endpoint
	ChangeMachineRole endpoint.Endpoint

	// Adding a machine with a one-time code: an administrator's side.
	MakeMachineJoin   endpoint.Endpoint
	ListMachineJoins  endpoint.Endpoint
	AcceptMachineJoin endpoint.Endpoint
	RemoveMachineJoin endpoint.Endpoint
	// The new machine's side, admitted by the code.
	RequestMachineJoin endpoint.Endpoint
	CollectMachineJoin endpoint.Endpoint
}

// RoleRequest is the role a running machine is asked to take.
type RoleRequest struct {
	ID   string `json:"-"`
	Role string `json:"role"`
}

// RoleResponse says whether the machine was asked.
type RoleResponse struct {
	Err error `json:"err,omitzero"`
}

func (r RoleResponse) Failed() error { return r.Err }

// MakeJoinRequest is the role the new machine will run.
type MakeJoinRequest struct {
	Role string `json:"role"`
}

// MakeJoinResponse is the code, shown once.
type MakeJoinResponse struct {
	contracts.IssuedMachineJoin
	Err error `json:"err,omitzero"`
}

func (r MakeJoinResponse) Failed() error { return r.Err }

// JoinsResponse is every machine being added, with the server's own time.
type JoinsResponse struct {
	Joins []entities.MachineJoin `json:"joins"`
	Now   time.Time              `json:"now"`
	Err   error                  `json:"err,omitzero"`
}

func (r JoinsResponse) Failed() error { return r.Err }

// JoinResponse is one machine being added, as it is now.
type JoinResponse struct {
	Join entities.MachineJoin `json:"join"`
	Err  error                `json:"err,omitzero"`
}

func (r JoinResponse) Failed() error { return r.Err }

// RequestJoinResponse is what the new machine is told: the words to show.
type RequestJoinResponse struct {
	contracts.MachineJoinReply
	Err error `json:"err,omitzero"`
}

func (r RequestJoinResponse) Failed() error { return r.Err }

// CollectJoinRequest is the new machine's code.
type CollectJoinRequest struct {
	Format string `json:"format"`
	Code   string `json:"code"`
}

// CollectJoinResponse is still waiting, or the sealed settings.
type CollectJoinResponse struct {
	contracts.MachineJoinCollection
	Err error `json:"err,omitzero"`
}

func (r CollectJoinResponse) Failed() error { return r.Err }

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
		ChangeMachineRole: func(ctx context.Context, request any) (any, error) {
			req, ok := request.(RoleRequest)
			if !ok {
				return nil, fmt.Errorf("machine: expected a RoleRequest, got %T", request)
			}
			id, err := uuid.Parse(req.ID)
			if err != nil {
				return RoleResponse{Err: apierr.Invalidf("%q is not a machine id", req.ID)}, nil
			}
			return RoleResponse{Err: s.ChangeMachineRole(ctx, id, req.Role)}, nil
		},
		MakeMachineJoin: func(ctx context.Context, request any) (any, error) {
			req, ok := request.(MakeJoinRequest)
			if !ok {
				return nil, fmt.Errorf("machine: expected a MakeJoinRequest, got %T", request)
			}
			issued, err := s.MakeMachineJoin(ctx, req.Role)
			return MakeJoinResponse{IssuedMachineJoin: issued, Err: err}, nil
		},
		ListMachineJoins: func(ctx context.Context, _ any) (any, error) {
			joins, err := s.ListMachineJoins(ctx)
			if err != nil {
				return JoinsResponse{Err: err}, nil
			}
			return JoinsResponse{Joins: joins, Now: time.Now().UTC()}, nil
		},
		AcceptMachineJoin: func(ctx context.Context, request any) (any, error) {
			id, err := joinID(request)
			if err != nil {
				return JoinResponse{Err: err}, nil
			}
			join, err := s.AcceptMachineJoin(ctx, id)
			return JoinResponse{Join: join, Err: err}, nil
		},
		RemoveMachineJoin: func(ctx context.Context, request any) (any, error) {
			id, err := joinID(request)
			if err != nil {
				return ForgetResponse{Err: err}, nil
			}
			return ForgetResponse{Err: s.RemoveMachineJoin(ctx, id)}, nil
		},
		RequestMachineJoin: func(ctx context.Context, request any) (any, error) {
			req, ok := request.(contracts.MachineJoinRequest)
			if !ok {
				return nil, fmt.Errorf("machine: expected a MachineJoinRequest, got %T", request)
			}
			reply, err := s.RequestMachineJoin(ctx, req)
			return RequestJoinResponse{MachineJoinReply: reply, Err: err}, nil
		},
		CollectMachineJoin: func(ctx context.Context, request any) (any, error) {
			req, ok := request.(CollectJoinRequest)
			if !ok {
				return nil, fmt.Errorf("machine: expected a CollectJoinRequest, got %T", request)
			}
			if req.Format != contracts.MachineJoinFormat {
				return CollectJoinResponse{Err: apierr.Invalidf("this server joins machines with %s, and was sent %q; run the same release of Metis on both", contracts.MachineJoinFormat, req.Format)}, nil
			}
			collection, err := s.CollectMachineJoin(ctx, req.Code)
			return CollectJoinResponse{MachineJoinCollection: collection, Err: err}, nil
		},
	}
}

func joinID(request any) (uuid.UUID, error) {
	req, ok := request.(IDRequest)
	if !ok {
		return uuid.Nil, fmt.Errorf("machine: expected an IDRequest, got %T", request)
	}
	id, err := uuid.Parse(req.ID)
	if err != nil {
		return uuid.Nil, apierr.Invalidf("%q is not the id of a machine being added", req.ID)
	}
	return id, nil
}
