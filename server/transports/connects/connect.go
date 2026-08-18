package connects

import (
	"encoding/json"
	"github.com/rs/zerolog/log"
	"net/http"

	"github.com/gsoultan/gobpm/api/proto/services/servicesconnect"
	"github.com/gsoultan/gobpm/server/endpoints"
	"github.com/gsoultan/gobpm/server/transports/connects/definitions"
	"github.com/gsoultan/gobpm/server/transports/connects/external_tasks"
	"github.com/gsoultan/gobpm/server/transports/connects/groups"
	"github.com/gsoultan/gobpm/server/transports/connects/organizations"
	"github.com/gsoultan/gobpm/server/transports/connects/processes"
	"github.com/gsoultan/gobpm/server/transports/connects/projects"
	"github.com/gsoultan/gobpm/server/transports/connects/signals"
	"github.com/gsoultan/gobpm/server/transports/connects/stats"
	"github.com/gsoultan/gobpm/server/transports/connects/tasks"
	"github.com/gsoultan/gobpm/server/transports/connects/users"
)

func NewConnectHandler(eps endpoints.Endpoints) (string, http.Handler) {
	mux := http.NewServeMux()

	mux.Handle(servicesconnect.NewOrganizationServiceHandler(organizations.NewHandler(eps.Organization)))
	mux.Handle(servicesconnect.NewProjectServiceHandler(projects.NewHandler(eps.Project)))
	mux.Handle(servicesconnect.NewProcessServiceHandler(processes.NewHandler(eps.Process)))
	mux.Handle(servicesconnect.NewTaskServiceHandler(tasks.NewHandler(eps.Task)))
	mux.Handle(servicesconnect.NewDefinitionServiceHandler(definitions.NewHandler(eps.Definition)))
	mux.Handle(servicesconnect.NewStatsServiceHandler(stats.NewHandler(eps.Process)))
	mux.Handle(servicesconnect.NewExternalTaskServiceHandler(external_tasks.NewHandler(eps.ExternalTask)))
	mux.Handle(servicesconnect.NewSignalServiceHandler(signals.NewHandler(eps.Process)))
	mux.Handle(servicesconnect.NewUserServiceHandler(users.NewHandler(eps.User)))
	mux.Handle(servicesconnect.NewGroupServiceHandler(groups.NewHandler(eps.Group)))

	// JSON 404 for unmatched Connect paths
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "not found"}); err != nil {
			log.Debug().Err(err).Str("path", r.URL.Path).Msg("Could not write the not-found reply")
		}
	})

	return "/", mux
}
