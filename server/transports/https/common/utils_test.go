package common

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httptransport "github.com/go-kit/kit/transport/http"
)

func TestEncodeErrorRedactsSensitiveData(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	err := errors.New("database failure password=super-secret")

	EncodeError(t.Context(), err, recorder)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status code: got %d want %d", recorder.Code, http.StatusInternalServerError)
	}

	body := recorder.Body.String()
	if strings.Contains(body, "super-secret") {
		t.Fatalf("response body leaked sensitive value: %s", body)
	}
	if !strings.Contains(body, "***REDACTED***") {
		t.Fatalf("response body should contain redacted marker: %s", body)
	}
}

type acceptedReply struct {
	Pending bool  `json:"pending"`
	Err     error `json:"-"`
}

func (r acceptedReply) StatusCode() int {
	if r.Pending {
		return http.StatusAccepted
	}
	return http.StatusOK
}

func (r acceptedReply) Failed() error { return r.Err }

// "Waiting for a second administrator" is not "done": a reply that says so is
// a 202, and every reply that says nothing stays a 200.
func TestAResponseMaySayItWasAcceptedRatherThanDone(t *testing.T) {
	for _, c := range []struct {
		reply any
		want  int
	}{{acceptedReply{Pending: true}, http.StatusAccepted}, {acceptedReply{}, http.StatusOK}, {map[string]string{"ok": "yes"}, http.StatusOK}} {
		rec := httptest.NewRecorder()
		if err := EncodeResponse(context.Background(), rec, c.reply); err != nil || rec.Code != c.want ||
			rec.Header().Get("Content-Type") != "application/json; charset=utf-8" {
			t.Errorf("%T: %d %q %v, want %d", c.reply, rec.Code, rec.Header().Get("Content-Type"), err, c.want)
		}
	}
}

// A reply that failed is answered with the status of its failure, whatever
// status it would have had: the failure is asked for first.
func TestAReplyThatFailedIsAnsweredAsItsFailureWhateverStatusItNames(t *testing.T) {
	rec := httptest.NewRecorder()
	failed := acceptedReply{Pending: true, Err: errors.New("the server could not")}
	if err := EncodeResponse(context.Background(), rec, failed); err != nil || rec.Code != http.StatusInternalServerError ||
		!strings.Contains(rec.Body.String(), `"error"`) || strings.Contains(rec.Body.String(), "pending") {
		t.Errorf("a failed reply that names 202: %d (%s) %v, want the failure's 500 and nothing of the reply", rec.Code, rec.Body.String(), err)
	}
}

// unwritable is a reply that cannot be written as JSON.
type unwritable struct{ Said string }

func (unwritable) MarshalJSON() ([]byte, error) {
	return nil, errors.New("this reply cannot be written")
}

// unwritableAccepted is one that also names a status of its own.
type unwritableAccepted struct{ unwritable }

func (unwritableAccepted) StatusCode() int { return http.StatusAccepted }

// served answers what a client gets from a route whose endpoint replies with
// reply: the real server go-kit builds, with this package's two encoders, as
// every route in the product is built.
func served(t *testing.T, reply any) (status int, contentType string, body []byte) {
	t.Helper()
	server := httptest.NewServer(httptransport.NewServer(
		func(context.Context, any) (any, error) { return reply, nil },
		func(context.Context, *http.Request) (any, error) { return struct{}{}, nil },
		EncodeResponse,
		httptransport.ServerErrorEncoder(EncodeError),
	))
	t.Cleanup(server.Close)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, http.NoBody)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read the reply: %v", err)
	}
	return resp.StatusCode, resp.Header.Get("Content-Type"), body
}

// A reply that cannot be written is the server's failure, a 500, whatever
// status it would have named: the status goes out only once the body is
// known to exist. Writing the status first would answer "accepted" — or
// "ok" — over a body that says the server failed.
func TestAReplyThatCannotBeWrittenIsA500WhateverStatusItNames(t *testing.T) {
	for name, reply := range map[string]any{
		"a reply that names no status": unwritable{Said: "x"},
		"a reply that names 202":       unwritableAccepted{},
		"one inside another":           map[string]any{"inner": unwritable{}},
	} {
		status, contentType, body := served(t, reply)
		if status != http.StatusInternalServerError || contentType != "application/json; charset=utf-8" || !bytes.Contains(body, []byte(`"error"`)) {
			t.Errorf("%s: %d %q (%s), want the 500 of a failure", name, status, contentType, body)
		}
	}
	// Asked directly, the encoder says it failed and has written nothing: no
	// status, and none of the body.
	rec := httptest.NewRecorder()
	if err := EncodeResponse(context.Background(), rec, unwritableAccepted{}); err == nil || rec.Body.Len() != 0 || rec.Code == http.StatusAccepted {
		t.Errorf("an unwritable reply: err %v, status %d, %d bytes written; want an error, and neither its status nor anything else written",
			err, rec.Code, rec.Body.Len())
	}
}

// What a reply is written as has not changed: the bytes the stream encoder
// wrote — the line it ends with, and the marks it escapes, among them — under
// the same Content-Type and status, for a reply that names a status and for
// one that does not.
func TestAReplyIsWrittenAsItAlwaysWas(t *testing.T) {
	long := strings.Repeat("a long reply, written whole; ", 4000)
	for name, reply := range map[string]any{
		"an object":            map[string]any{"ok": "yes", "n": 1.5, "list": []string{}, "nothing": nil},
		"marks that escape":    map[string]string{"html": "<b>&amp;</b>", "line": "a\u2028b", "quote": `"`},
		"a list":               []int{1, 2, 3},
		"text":                 "plain",
		"nothing at all":       nil,
		"a struct":             struct{ A, B string }{"a", "b"},
		"a reply with a 202":   acceptedReply{Pending: true},
		"a reply with a 200":   acceptedReply{},
		"more than one buffer": map[string]string{"said": long},
	} {
		var want bytes.Buffer
		if err := json.NewEncoder(&want).Encode(reply); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		wantStatus := http.StatusOK
		if coder, ok := reply.(interface{ StatusCode() int }); ok {
			wantStatus = coder.StatusCode()
		}
		rec := httptest.NewRecorder()
		if err := EncodeResponse(context.Background(), rec, reply); err != nil || rec.Code != wantStatus ||
			rec.Header().Get("Content-Type") != "application/json; charset=utf-8" || !bytes.Equal(rec.Body.Bytes(), want.Bytes()) {
			t.Errorf("%s: %d %q %v\n%.200q\nwant %d and\n%.200q", name, rec.Code, rec.Header().Get("Content-Type"), err, rec.Body.String(), wantStatus, want.String())
		}
		// And over a real connection, where the server adds its own headers.
		status, contentType, body := served(t, reply)
		if status != wantStatus || contentType != "application/json; charset=utf-8" || !bytes.Equal(body, want.Bytes()) {
			t.Errorf("%s, served: %d %q\n%.200q\nwant %d and\n%.200q", name, status, contentType, body, wantStatus, want.String())
		}
	}
}
