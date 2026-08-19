package grpcs

import (
	"github.com/gsoultan/gobpm/api/proto/services"
	"github.com/gsoultan/gobpm/server/endpoints"
	"github.com/gsoultan/gobpm/server/transports/grpcs/definitions"
	"github.com/gsoultan/gobpm/server/transports/grpcs/external_tasks"
	"github.com/gsoultan/gobpm/server/transports/grpcs/organizations"
	"github.com/gsoultan/gobpm/server/transports/grpcs/processes"
	"github.com/gsoultan/gobpm/server/transports/grpcs/projects"
	"github.com/gsoultan/gobpm/server/transports/grpcs/signals"
	"github.com/gsoultan/gobpm/server/transports/grpcs/stats"
	"github.com/gsoultan/gobpm/server/transports/grpcs/tasks"
)

// Server is every gRPC service in one value.
//
// It is exported and concrete rather than returned as `any` so that "this
// implements all eight service interfaces" is a fact the compiler checks. It
// used to be `any`, which pushed the question to eight runtime type assertions
// at registration — a mis-wire would have panicked during startup rather than
// failing to build.
type Server = grpcServer

type grpcServer struct {
	services.OrganizationServiceServer
	services.ProjectServiceServer
	services.ProcessServiceServer
	services.TaskServiceServer
	services.DefinitionServiceServer
	services.StatsServiceServer
	services.ExternalTaskServiceServer
	services.SignalServiceServer
}

func NewGRPCServer(eps endpoints.Endpoints) *Server {
	return &grpcServer{
		OrganizationServiceServer: organizations.NewServer(eps.Organization),
		ProjectServiceServer:      projects.NewServer(eps.Project),
		ProcessServiceServer:      processes.NewServer(eps.Process),
		TaskServiceServer:         tasks.NewServer(eps.Task),
		DefinitionServiceServer:   definitions.NewServer(eps.Definition),
		StatsServiceServer:        stats.NewServer(eps.Process),
		ExternalTaskServiceServer: external_tasks.NewServer(eps.ExternalTask),
		SignalServiceServer:       signals.NewServer(eps.Process),
	}
}
