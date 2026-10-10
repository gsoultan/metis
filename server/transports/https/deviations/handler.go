package deviations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/endpoints/deviation"
	"github.com/gsoultan/metis/server/transports/https/common"
)

// maxDeviateBodyBytes bounds a request to waive, cancel or hold.
//
// The request is read into memory before anything judges it. What it may
// carry is small — a reason of at most 2,000 characters and outputs of at
// most 64 KiB — so this is several times what a request needs and far less
// than a way to exhaust a server.
const maxDeviateBodyBytes = 256 << 10

func RegisterHandlers(m *http.ServeMux, eps deviation.Endpoints, options []httptransport.ServerOption) {
	m.Handle("GET /api/v1/instances/{id}/deviations", httptransport.NewServer(
		eps.ListInstanceDeviations,
		decodeListInstanceDeviationsRequest,
		common.EncodeResponse,
		options...,
	))

	// Waive, cancel or hold one instance where it stands. The reply is a
	// Failer, so a refusal is answered with the status of its class and not
	// with a 200 that carries an error.
	m.Handle("POST /api/v1/instances/{id}/deviations", httptransport.NewServer(
		eps.DeviateInstance,
		decodeDeviateInstanceRequest,
		common.EncodeResponse,
		options...,
	))

	registerRequestHandlers(m, eps, options)
}

func decodeListInstanceDeviationsRequest(_ context.Context, r *http.Request) (any, error) {
	return deviation.ListInstanceDeviationsRequest{InstanceID: r.PathValue("id")}, nil
}

// deviateFields is the fields a request to waive, cancel or hold may carry,
// each written exactly so.
var deviateFields = map[string]struct{}{
	"kind": {}, "node_id": {}, "reason": {}, "outputs": {}, "visit_key": {}, "dry_run": {},
}

// decodeDeviateInstanceRequest reads a request to waive, cancel or hold, and
// refuses one it cannot read exactly.
//
// The request is a preview unless it says "dry_run": false, so how it is read
// decides whether an instance is changed. It is read strictly: one JSON
// object and nothing after it, only the fields a request has, each named
// exactly and once, each holding the kind of value it takes. The decoder
// alone would let three things by that a strict reading must not: a field's
// name in other letters ("DRY_RUN"), a field said twice — where it keeps the
// last, so "dry_run": true, "dry_run": false is an apply to it and a preview
// to whoever reads the first — and anything written after the object.
//
// The same holds inside outputs: a name said twice there is refused too
// (namedOnceThroughout). What a waived step counts as decides which branch a
// gateway takes, and the decoder would keep the last of two values without a
// word.
//
// A body that cannot be read is the caller's mistake and is answered as one,
// never as a preview: a client that meant to apply and wrote "dry_run":
// "false" must not be told its request went well. The answer is a sentence
// and not the decoder's error, which names the server's own types; what the
// decoder said is logged at a level that is off unless somebody is looking,
// since any caller can send a bad body as often as they like.
func decodeDeviateInstanceRequest(_ context.Context, r *http.Request) (any, error) {
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxDeviateBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, apierr.Invalidf("this request is larger than %d KiB, which is more than a waive, a cancel or a hold needs to say",
				maxDeviateBodyBytes>>10)
		}
		return nil, unreadableDeviateRequest(r, err)
	}
	if err := namedExactlyAndOnce(body, deviateFields); err != nil {
		return nil, unreadableDeviateRequest(r, err)
	}
	var req deviation.DeviateInstanceRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return nil, unreadableDeviateRequest(r, err)
	}
	req.InstanceID = r.PathValue("id")
	return req, nil
}

// unreadableDeviateRequest is the refusal of a body that could not be read,
// in words somebody can act on; why is logged, not told.
func unreadableDeviateRequest(r *http.Request, why error) error {
	log.Debug().Err(why).Str("path", r.URL.Path).Msg("A request to waive, cancel or hold could not be read")
	return apierr.Invalidf("this request could not be read: send one JSON object with kind, reason, node_id (a cancel may leave it out), " +
		"and for a waive outputs; to apply a plan add its visit_key and \"dry_run\": false; name each field exactly and once")
}

// namedExactlyAndOnce reports why body is not one JSON object whose fields
// are each one a request has, written exactly as it is named and given once,
// with nothing after the object; nil when it is. fields is the fields a
// request of this kind may carry: a waive, cancel or hold has its own
// (deviateFields), and a decision on a request has one (decideFields).
//
// It looks at the names only — the request's own, and those inside outputs,
// which are a caller's to choose and so are asked only to be said once. What
// each field holds is the decoder's to read.
func namedExactlyAndOnce(body []byte, fields map[string]struct{}) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	opening, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("the body does not begin as JSON: %w", err)
	}
	if opening != json.Delim('{') {
		return fmt.Errorf("the body is a JSON %T, not an object", opening)
	}
	// Bounded by the fields a request has: a name is kept only if it is one.
	seen := make(map[string]struct{}, len(fields))
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		// Inside an object and before a value, the token is the field's name.
		name, isName := token.(string)
		if !isName {
			return fmt.Errorf("the body has a %T where a field's name belongs", token)
		}
		if _, known := fields[name]; !known {
			// The name is the caller's, and as long as they like: the log
			// keeps the start of it.
			return fmt.Errorf("the body has a field %.64q, which a request does not have", name)
		}
		if _, twice := seen[name]; twice {
			return fmt.Errorf("the body says %q twice", name)
		}
		seen[name] = struct{}{}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		if name == "outputs" {
			if err := namedOnceThroughout(value); err != nil {
				return fmt.Errorf("in outputs, %w", err)
			}
		}
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	switch _, err := decoder.Token(); {
	case err == nil:
		return errors.New("the body goes on after the object")
	case !errors.Is(err, io.EOF):
		return fmt.Errorf("the body goes on after the object: %w", err)
	}
	return nil
}

// namedOnceThroughout reports why a JSON value says a name twice in one
// object, at whatever depth; nil when no object in it does. The same name in
// two different objects is two names.
//
// value is one whole JSON value, already read as such, so it is well formed
// and no deeper than the decoder lets a value be. It is walked token by token
// with the open objects and lists kept in a slice, not on the stack, and the
// names it keeps are a part of a body that has a size.
func namedOnceThroughout(value []byte) error {
	// One entry for each object or list that is open: the names said so far
	// in an object (nil for a list), and whether its next token is a name.
	type container struct {
		names     map[string]struct{}
		nameNext  bool
		isAnArray bool
	}
	var open []container
	// valueRead notes, in the object a value belongs to, that a name comes next.
	valueRead := func() {
		if last := len(open) - 1; last >= 0 && !open[last].isAnArray {
			open[last].nameNext = true
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		switch token := token.(type) {
		case json.Delim:
			switch token {
			case '{':
				valueRead()
				open = append(open, container{names: map[string]struct{}{}, nameNext: true})
			case '[':
				valueRead()
				open = append(open, container{isAnArray: true})
			default:
				open = open[:len(open)-1]
			}
		case string:
			last := len(open) - 1
			if last < 0 || !open[last].nameNext {
				valueRead()
				continue
			}
			if _, twice := open[last].names[token]; twice {
				return fmt.Errorf("an object says %.64q twice", token)
			}
			open[last].names[token] = struct{}{}
			open[last].nameNext = false
		default:
			valueRead()
		}
	}
}
