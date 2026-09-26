package connects

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"connectrpc.com/connect"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/api/proto/services/servicesconnect"
	"github.com/gsoultan/metis/internal/pkg/redaction"
	"github.com/gsoultan/metis/server/endpoints"
	"github.com/gsoultan/metis/server/transports/connects/definitions"
	"github.com/gsoultan/metis/server/transports/connects/external_tasks"
	"github.com/gsoultan/metis/server/transports/connects/groups"
	"github.com/gsoultan/metis/server/transports/connects/organizations"
	"github.com/gsoultan/metis/server/transports/connects/processes"
	"github.com/gsoultan/metis/server/transports/connects/projects"
	"github.com/gsoultan/metis/server/transports/connects/signals"
	"github.com/gsoultan/metis/server/transports/connects/stats"
	"github.com/gsoultan/metis/server/transports/connects/tasks"
	"github.com/gsoultan/metis/server/transports/connects/users"
	"github.com/gsoultan/metis/server/transports/https/common"
)

func NewConnectHandler(eps endpoints.Endpoints) (string, http.Handler) {
	mux := http.NewServeMux()
	classified := connect.WithInterceptors(refusalCodes())

	mux.Handle(servicesconnect.NewOrganizationServiceHandler(organizations.NewHandler(eps.Organization), classified))
	mux.Handle(servicesconnect.NewProjectServiceHandler(projects.NewHandler(eps.Project), classified))
	mux.Handle(servicesconnect.NewProcessServiceHandler(processes.NewHandler(eps.Process), classified))
	mux.Handle(servicesconnect.NewTaskServiceHandler(tasks.NewHandler(eps.Task), classified))
	mux.Handle(servicesconnect.NewDefinitionServiceHandler(definitions.NewHandler(eps.Definition), classified))
	mux.Handle(servicesconnect.NewStatsServiceHandler(stats.NewHandler(eps.Process), classified))
	mux.Handle(servicesconnect.NewExternalTaskServiceHandler(external_tasks.NewHandler(eps.ExternalTask), classified))
	mux.Handle(servicesconnect.NewSignalServiceHandler(signals.NewHandler(eps.Process), classified))
	mux.Handle(servicesconnect.NewUserServiceHandler(users.NewHandler(eps.User), classified))
	mux.Handle(servicesconnect.NewGroupServiceHandler(groups.NewHandler(eps.Group), classified))

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

// refusalCodes gives an error the Connect code its REST twin answers with.
//
// The handlers return the endpoint chain's errors as they are — a refusal from
// the role check, the tenant resolver or the sign-in — and Connect encodes an
// error it did not make as `unknown`, sent as HTTP 500. A member asking for
// something only an administrator may do was answered as a server fault, and
// spent the error budget. Classified here the way REST classifies it
// (common.CodeFrom), and redacted the way REST redacts it. An error that is
// already a Connect error keeps its code.
func refusalCodes() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			res, err := next(ctx, req)
			if err == nil {
				return res, nil
			}
			var connectErr *connect.Error
			if errors.As(err, &connectErr) {
				return res, err
			}
			return res, connect.NewError(codeFor(common.CodeFrom(err)), errors.New(redaction.RedactError(err)))
		}
	}
}

// codeFor is the Connect code for the HTTP status REST answers an error with:
// the code the Connect protocol itself sends as that status.
func codeFor(status int) connect.Code {
	switch status {
	case http.StatusBadRequest:
		return connect.CodeInvalidArgument
	case http.StatusUnauthorized:
		return connect.CodeUnauthenticated
	case http.StatusForbidden:
		return connect.CodePermissionDenied
	case http.StatusNotFound:
		return connect.CodeNotFound
	default:
		return connect.CodeInternal
	}
}
