// Package releases exposes a project's releases on the REST surface: cut one
// here, import one from another environment, make it live, roll back.
package releases

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/endpoints/release"
	"github.com/gsoultan/metis/server/transports/https/common"
)

// maxReleaseRequestBytes bounds a request body. It names models, not carries
// them.
const maxReleaseRequestBytes = 64 << 10

func RegisterHandlers(m *http.ServeMux, eps release.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/projects/{id}/releases", httptransport.NewServer(
		eps.ListReleases, decodeProject, common.EncodeResponse, options...,
	))
	m.Handle("POST /api/v1/projects/{id}/releases", httptransport.NewServer(
		eps.CutRelease, decodeCut, common.EncodeResponse, options...,
	))
	m.Handle("POST /api/v1/projects/{id}/releases/import", httptransport.NewServer(
		eps.ImportRelease, decodeImport, common.EncodeResponse, options...,
	))
	m.Handle("POST /api/v1/projects/{id}/releases/rollback", httptransport.NewServer(
		eps.RollBackRelease, decodeProject, common.EncodeResponse, options...,
	))
	m.Handle("GET /api/v1/projects/{id}/releases/{name}", httptransport.NewServer(
		eps.GetRelease, decodeNamed, common.EncodeResponse, options...,
	))
	m.Handle("POST /api/v1/projects/{id}/releases/{name}/go-live", httptransport.NewServer(
		eps.GoLiveRelease, decodeNamed, common.EncodeResponse, options...,
	))
}

func decodeProject(_ context.Context, r *http.Request) (any, error) {
	return release.ProjectRequest{ProjectID: r.PathValue("id")}, nil
}

func decodeNamed(_ context.Context, r *http.Request) (any, error) {
	return release.NamedRequest{ProjectID: r.PathValue("id"), Name: r.PathValue("name")}, nil
}

func decodeCut(_ context.Context, r *http.Request) (any, error) {
	var req release.CutRequest
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	req.ProjectID = r.PathValue("id")
	return req, nil
}

func decodeImport(_ context.Context, r *http.Request) (any, error) {
	var req release.ImportRequest
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	req.ProjectID = r.PathValue("id")
	return req, nil
}

// decodeBody reads one JSON object and refuses fields nobody declared: a
// misspelt "processes" would otherwise cut a release of nothing.
func decodeBody(r *http.Request, into any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxReleaseRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return apierr.Invalidf("this request could not be read: %v", err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return apierr.Invalidf("this request could not be read: something follows the JSON object")
	}
	return nil
}
