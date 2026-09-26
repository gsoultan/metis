package participants

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/gsoultan/metis/server/endpoints/participant"
	"github.com/gsoultan/metis/server/transports/https/common"
	"github.com/rs/zerolog/log"
)

// maxUploadBytes bounds a directory upload.
//
// The body is caller-supplied and read into memory. Ten megabytes is far past
// any real directory — the import itself refuses past ten thousand rows — and
// small enough that an upload cannot exhaust the server by being large.
const maxUploadBytes = 10 << 20

func RegisterHandlers(m *http.ServeMux, eps participant.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/participants", httptransport.NewServer(
		eps.ListParticipants,
		decodeListParticipantsRequest,
		common.EncodeResponse,
		options...,
	))
	m.Handle("POST /api/v1/participants/import", httptransport.NewServer(
		eps.ImportParticipants,
		decodeImportParticipantsRequest,
		common.EncodeResponse,
		options...,
	))
	// DELETE rather than another POST under /participants/, and no conflict with
	// the import route above: ServeMux matches on method as well as path, and
	// the two patterns never both match a request.
	m.Handle("DELETE /api/v1/participants/{id}", httptransport.NewServer(
		eps.RemoveParticipant,
		decodeRemoveParticipantRequest,
		common.EncodeResponse,
		options...,
	))
}

// decodeRemoveParticipantRequest takes the person from the path and the project
// from the query, so the service can check the two agree.
func decodeRemoveParticipantRequest(_ context.Context, r *http.Request) (any, error) {
	return participant.RemoveParticipantRequest{
		ProjectID: r.URL.Query().Get("project_id"),
		ID:        r.PathValue("id"),
	}, nil
}

func decodeListParticipantsRequest(_ context.Context, r *http.Request) (any, error) {
	return participant.ListParticipantsRequest{
		ProjectID: r.URL.Query().Get("project_id"),
		Limit:     common.LimitParam(r),
	}, nil
}

// decodeImportParticipantsRequest reads either an uploaded file or a described
// remote source.
//
// The content type decides, because a file has to arrive as multipart and the
// other two are naturally JSON. One route rather than two: the caller is
// answering one question, and splitting it would mean two ways to say "import"
// that return the same thing.
func decodeImportParticipantsRequest(_ context.Context, r *http.Request) (any, error) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		return decodeUpload(r)
	}

	var req participant.ImportParticipantsRequest
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxUploadBytes)).Decode(&req); err != nil {
		return nil, err
	}
	return req, nil
}

func decodeUpload(r *http.Request) (any, error) {
	// MaxBytesReader first, because ParseMultipartForm's argument is only the
	// in-memory budget: everything past it spills to temporary files, so the
	// call alone bounds memory and not the upload. Without this an unbounded
	// body fills the disk instead of the heap, which is the same denial of
	// service wearing a different hat.
	r.Body = http.MaxBytesReader(nil, r.Body, maxUploadBytes)
	// #nosec G120 -- the body is bounded by MaxBytesReader on the line above.
	// gosec matches the call rather than the bound, so it reports this either
	// way; the annotation is here so the finding does not read as unreviewed.
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		return nil, fmt.Errorf("could not read the upload: %w", err)
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		return nil, fmt.Errorf("no file was uploaded: %w", err)
	}
	defer func() {
		// Logged rather than discarded: ParseMultipartForm spills a large
		// upload to a temporary file, and one that will not close is a file
		// handle this process keeps for every import somebody runs.
		if err := file.Close(); err != nil {
			log.Warn().Err(err).Msg("Could not close an uploaded participant directory")
		}
	}()

	// Bounded again at the read: ParseMultipartForm spills past its limit to
	// disk rather than refusing, so the limit above is not by itself a bound on
	// what this reads into memory.
	body, err := common.ReadLimited(file, maxUploadBytes)
	if err != nil {
		return nil, err
	}
	return participant.ImportParticipantsRequest{
		ProjectID: r.FormValue("project_id"),
		Kind:      "csv",
		CSV:       body,
	}, nil
}
