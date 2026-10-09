package common

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
