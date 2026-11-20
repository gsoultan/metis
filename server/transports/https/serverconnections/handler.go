// Package serverconnections serves connecting this server to the ones
// releases come from and go to.
package serverconnections

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	httptransport "github.com/go-kit/kit/transport/http"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/endpoints/serverconnection"
	"github.com/gsoultan/metis/server/transports/https/common"
)

// maxRequestBytes bounds every request here: an address and a code, or a
// server's description. The handshake is public, so it is kept small.
const maxRequestBytes = 8 << 10

func RegisterHandlers(m *http.ServeMux, eps serverconnection.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/server-connections", httptransport.NewServer(
		eps.ListServerConnections, noRequest, common.EncodeResponse, options...))
	m.Handle("POST /api/v1/server-connections/codes", httptransport.NewServer(
		eps.MakeServerConnectionCode, noRequest, common.EncodeResponse, options...))
	m.Handle("POST /api/v1/server-connections", httptransport.NewServer(
		eps.CreateServerConnection, decodeConnect, common.EncodeResponse, options...))
	m.Handle("POST /api/v1/server-connections/{id}/accept", httptransport.NewServer(
		eps.AcceptServerConnection, decodeID, common.EncodeResponse, options...))
	m.Handle("DELETE /api/v1/server-connections/{id}", httptransport.NewServer(
		eps.RemoveServerConnection, decodeID, common.EncodeResponse, options...))
	m.Handle("POST "+impl.ServerHandshakePath, httptransport.NewServer(
		eps.AnswerServerHandshake, decodeHandshake, encodeHandshake, options...))

	m.Handle("POST /api/v1/projects/{id}/releases/{name}/send", httptransport.NewServer(
		eps.SendRelease, decodeSend, common.EncodeResponse, options...))
	m.Handle("GET /api/v1/projects/{id}/release-deliveries", httptransport.NewServer(
		eps.ListReleaseDeliveries, decodeProject, common.EncodeResponse, options...))
	m.Handle("POST /api/v1/server-connections/{id}/collect", httptransport.NewServer(
		eps.CollectReleases, decodeID, common.EncodeResponse, options...))
	m.Handle("POST "+impl.ReleaseCollectPath, httptransport.NewServer(
		eps.AnswerReleaseCollection, decodeCollect, encodeCollection, options...))
	m.Handle("POST "+impl.ReleaseReportPath, httptransport.NewServer(
		eps.AnswerReleaseReport, decodeReport, common.EncodeResponse, options...))
}

// maxReportBytes bounds a report: a few hundred releases, each with a short
// reason.
const maxReportBytes = 512 << 10

func decodeSend(_ context.Context, r *http.Request) (any, error) {
	var req serverconnection.SendRequest
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	req.ProjectID, req.Release = r.PathValue("id"), r.PathValue("name")
	return req, nil
}

func decodeProject(_ context.Context, r *http.Request) (any, error) {
	return serverconnection.ProjectRequest{ProjectID: r.PathValue("id")}, nil
}

func decodeCollect(_ context.Context, r *http.Request) (any, error) {
	var req serverconnection.CollectRequest
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	req.Token = bearerToken(r)
	return req, nil
}

func decodeReport(_ context.Context, r *http.Request) (any, error) {
	var req serverconnection.ReportRequest
	if err := decodeLimited(r, &req, maxReportBytes); err != nil {
		return nil, err
	}
	req.Token = bearerToken(r)
	return req, nil
}

// bearerToken is the connection's token the calling server presents.
func bearerToken(r *http.Request) string {
	scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

// encodeCollection writes what a server checking in is handed bare, as it
// reads it, and a refusal as every other error is written.
func encodeCollection(ctx context.Context, w http.ResponseWriter, response any) error {
	if handed, ok := response.(serverconnection.CollectionResponse); ok && handed.Err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		return json.NewEncoder(w).Encode(handed.ReleaseCollection)
	}
	return common.EncodeResponse(ctx, w, response)
}

func noRequest(context.Context, *http.Request) (any, error) { return nil, nil }

func decodeID(_ context.Context, r *http.Request) (any, error) {
	return serverconnection.IDRequest{ID: r.PathValue("id")}, nil
}

func decodeConnect(_ context.Context, r *http.Request) (any, error) {
	var req serverconnection.ConnectRequest
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeHandshake(_ context.Context, r *http.Request) (any, error) {
	var hello contracts.ServerHandshake
	if err := decodeBody(r, &hello); err != nil {
		return nil, err
	}
	return hello, nil
}

// encodeHandshake writes the reply bare, as the other server reads it, and a
// refusal as every other error is written.
func encodeHandshake(ctx context.Context, w http.ResponseWriter, response any) error {
	if reply, ok := response.(serverconnection.HandshakeResponse); ok && reply.Err == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		return json.NewEncoder(w).Encode(reply.ServerHandshakeReply)
	}
	return common.EncodeResponse(ctx, w, response)
}

// decodeBody reads one JSON object and refuses fields nobody declared.
func decodeBody(r *http.Request, into any) error {
	return decodeLimited(r, into, maxRequestBytes)
}

func decodeLimited(r *http.Request, into any, limit int64) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, limit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return apierr.Invalidf("this request could not be read: %v", err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return apierr.Invalidf("this request could not be read: something follows the JSON object")
	}
	return nil
}
