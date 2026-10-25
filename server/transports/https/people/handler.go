// Package people routes the People API. Everything here is under
// /api/v1/people/, where the auth interceptor admits People tokens and nothing
// else; the sign-in alone needs none.
package people

import (
	"context"
	"encoding/json"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/endpoints/people"
	"github.com/gsoultan/metis/server/transports/https/common"
)

// maxSignInBytes bounds a sign-in body: a project id, a username and a
// password, read before anybody is known.
const maxSignInBytes = 16 << 10

func RegisterHandlers(m *http.ServeMux, eps people.Endpoints, options []httptransport.ServerOption) {
	m.Handle("POST /api/v1/people/login", httptransport.NewServer(
		eps.SignIn,
		decodeSignInRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/people/me", httptransport.NewServer(
		eps.Me,
		func(context.Context, *http.Request) (any, error) { return people.MeRequest{}, nil },
		common.EncodeResponse,
		options...,
	))
}

func decodeSignInRequest(_ context.Context, r *http.Request) (any, error) {
	var req people.SignInRequest
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxSignInBytes)).Decode(&req); err != nil {
		return nil, apierr.Invalidf("the sign-in is not valid JSON")
	}
	return req, nil
}
