// Package machines serves the Servers page: the processes running this
// installation, and the machines being added to it.
package machines

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/endpoints/machine"
	"github.com/gsoultan/metis/server/transports/https/common"
)

// maxRequestBytes bounds every request here: a role, or a code, a host name
// and a key. The join routes are public, so it is kept small.
const maxRequestBytes = 8 << 10

func RegisterHandlers(m *http.ServeMux, eps machine.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/machines", httptransport.NewServer(
		eps.ListMachines, noRequest, common.EncodeResponse, options...))
	m.Handle("DELETE /api/v1/machines/{id}", httptransport.NewServer(
		eps.ForgetMachine, decodeID, common.EncodeResponse, options...))

	m.Handle("GET /api/v1/machines/joins", httptransport.NewServer(
		eps.ListMachineJoins, noRequest, common.EncodeResponse, options...))
	m.Handle("POST /api/v1/machines/joins", httptransport.NewServer(
		eps.MakeMachineJoin, decodeMake, noStore, options...))
	m.Handle("POST /api/v1/machines/joins/{id}/accept", httptransport.NewServer(
		eps.AcceptMachineJoin, decodeID, common.EncodeResponse, options...))
	m.Handle("DELETE /api/v1/machines/joins/{id}", httptransport.NewServer(
		eps.RemoveMachineJoin, decodeID, common.EncodeResponse, options...))

	m.Handle("POST "+impl.MachineJoinPath, httptransport.NewServer(
		eps.RequestMachineJoin, decodeRequest, noStore, options...))
	m.Handle("POST "+impl.MachineJoinCollectPath, httptransport.NewServer(
		eps.CollectMachineJoin, decodeCollect, noStore, options...))
}

func noRequest(context.Context, *http.Request) (any, error) { return nil, nil }

func decodeID(_ context.Context, r *http.Request) (any, error) {
	return machine.IDRequest{ID: r.PathValue("id")}, nil
}

func decodeMake(_ context.Context, r *http.Request) (any, error) {
	var req machine.MakeJoinRequest
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeRequest(_ context.Context, r *http.Request) (any, error) {
	var req contracts.MachineJoinRequest
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeCollect(_ context.Context, r *http.Request) (any, error) {
	var req machine.CollectJoinRequest
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	return req, nil
}

// noStore writes a reply that holds a code or sealed settings: nothing on
// the way keeps a copy.
func noStore(ctx context.Context, w http.ResponseWriter, response any) error {
	w.Header().Set("Cache-Control", "no-store")
	return common.EncodeResponse(ctx, w, response)
}

// decodeBody reads one JSON object and refuses fields nobody declared.
func decodeBody(r *http.Request, into any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return apierr.Invalidf("this request could not be read: %v", err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return apierr.Invalidf("this request could not be read: something follows the JSON object")
	}
	return nil
}
