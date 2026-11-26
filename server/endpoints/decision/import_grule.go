package decision

import (
	"context"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services"
)

// MakeImportGruleEndpoint turns Grule rules into decisions and a process.
func MakeImportGruleEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ImportGruleRequest)
		if !ok {
			return ImportGruleResponse{Err: errWrongDecisionRequest}, nil
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return ImportGruleResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier: %v", req.ProjectID, err)}, nil
		}
		imported, err := s.ImportGruleRules(ctx, entities.GruleImportRequest{
			ProjectID: projectID, Key: req.Key, Name: req.Name, Files: req.Files,
			Preview: req.Preview, Deploy: req.Deploy,
		})
		return ImportGruleResponse{Import: imported, Err: err}, nil
	}
}
