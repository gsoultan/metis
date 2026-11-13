package upgrade

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

// Endpoints read what the Upgrades page shows.
type Endpoints struct {
	UpgradeStatus   endpoint.Endpoint
	CheckForUpgrade endpoint.Endpoint
	StartUpgrade    endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		// Closures rather than method values: a method value is taken when the
		// endpoints are built, and that dereferences the facade then.
		UpgradeStatus:   statusEndpoint(func(ctx context.Context) (contracts.UpgradeStatus, error) { return s.UpgradeStatus(ctx) }),
		CheckForUpgrade: statusEndpoint(func(ctx context.Context) (contracts.UpgradeStatus, error) { return s.CheckForUpgrade(ctx) }),
		StartUpgrade: func(ctx context.Context, request any) (any, error) {
			start, ok := request.(contracts.UpgradeStart)
			if !ok {
				return nil, fmt.Errorf("upgrade: expected an UpgradeStart, got %T", request)
			}
			status, err := s.StartUpgrade(ctx, start)
			if err != nil {
				return StatusResponse{Err: err}, nil
			}
			return StatusResponse{Status: &status}, nil
		},
	}
}

func statusEndpoint(read func(context.Context) (contracts.UpgradeStatus, error)) endpoint.Endpoint {
	return func(ctx context.Context, _ any) (any, error) {
		status, err := read(ctx)
		if err != nil {
			return StatusResponse{Err: err}, nil
		}
		return StatusResponse{Status: &status}, nil
	}
}
