package serversetting

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

// Endpoints read and change the server settings on the Settings page.
type Endpoints struct {
	ListServerSettings   endpoint.Endpoint
	ChangeServerSetting  endpoint.Endpoint
	ServerSettingHistory endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		ListServerSettings:   makeListEndpoint(s),
		ChangeServerSetting:  makeChangeEndpoint(s),
		ServerSettingHistory: makeHistoryEndpoint(s),
	}
}

func makeListEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, _ any) (any, error) {
		current, err := s.ServerSettings(ctx)
		if err != nil {
			return ListResponse{Err: err}, nil
		}
		return listResponseOf(current), nil
	}
}

func makeChangeEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ChangeRequest)
		if !ok {
			return nil, fmt.Errorf("server setting: expected a ChangeRequest, got %T", request)
		}
		changed, err := s.ChangeServerSetting(ctx, contracts.ServerSettingChange{Name: req.Name, Value: req.Value, Reason: req.Reason})
		if err != nil {
			return ChangeResponse{Err: err}, nil
		}
		view := settingViewOf(changed)
		return ChangeResponse{Setting: &view}, nil
	}
}

func makeHistoryEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(HistoryRequest)
		if !ok {
			return nil, fmt.Errorf("server setting: expected a HistoryRequest, got %T", request)
		}
		history, err := s.ServerSettingHistory(ctx, req.Name)
		if err != nil {
			return HistoryResponse{Err: err}, nil
		}
		return HistoryResponse{Changes: changeViewsOf(history)}, nil
	}
}
