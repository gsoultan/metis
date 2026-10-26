// Package people routes the People API. Everything here is under
// /api/v1/people/, where the auth interceptor admits People tokens and nothing
// else; the sign-in alone needs none.
package people

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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
	m.Handle("GET /api/v1/people/tasks", httptransport.NewServer(
		eps.PersonTasks,
		func(_ context.Context, r *http.Request) (any, error) {
			page, size := common.PageParams(r)
			return people.PersonTasksRequest{Page: page, PageSize: size}, nil
		},
		common.EncodeResponse,
		options...,
	))
	m.Handle("GET /api/v1/people/tasks/{id}", httptransport.NewServer(
		eps.PersonTask, decodePersonTaskRequest, common.EncodeResponse, options...,
	))
	m.Handle("POST /api/v1/people/tasks/{id}/claim", httptransport.NewServer(
		eps.ClaimPersonTask, decodePersonTaskRequest, common.EncodeResponse, options...,
	))
	m.Handle("POST /api/v1/people/tasks/{id}/release", httptransport.NewServer(
		eps.ReleasePersonTask, decodePersonTaskRequest, common.EncodeResponse, options...,
	))
	m.Handle("POST /api/v1/people/tasks/{id}/complete", httptransport.NewServer(
		eps.CompletePersonTask, decodeCompletePersonTaskRequest, common.EncodeResponse, options...,
	))
}

func decodePersonTaskRequest(_ context.Context, r *http.Request) (any, error) {
	return people.PersonTaskRequest{ID: r.PathValue("id")}, nil
}

// decodeCompletePersonTaskRequest reads the submitted form. An empty body is a
// form with nothing on it, not an error: a step can ask only for a decision
// to be made.
func decodeCompletePersonTaskRequest(_ context.Context, r *http.Request) (any, error) {
	req := people.CompletePersonTaskRequest{ID: r.PathValue("id")}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		return nil, apierr.Invalidf("the form is not valid JSON")
	}
	req.ID = r.PathValue("id")
	return req, nil
}

func decodeSignInRequest(_ context.Context, r *http.Request) (any, error) {
	var req people.SignInRequest
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxSignInBytes)).Decode(&req); err != nil {
		return nil, apierr.Invalidf("the sign-in is not valid JSON")
	}
	return req, nil
}
