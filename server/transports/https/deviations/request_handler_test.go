package deviations

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/endpoints/deviation"
)

const aRequestID = "0192e7a1-0000-7000-8000-000000000000"

func decodeDecision(t *testing.T, body string) (deviation.DecideDeviationRequestRequest, error) {
	t.Helper()
	r := httptest.NewRequestWithContext(context.Background(), "POST", "/api/v1/deviation-requests/"+aRequestID+"/approve", strings.NewReader(body))
	r.SetPathValue("id", aRequestID)
	decoded, err := decodeDecideRequest(context.Background(), r)
	if err != nil {
		return deviation.DecideDeviationRequestRequest{}, err
	}
	return decoded.(deviation.DecideDeviationRequestRequest), nil
}

// A decision says a reason or says nothing. An approval may be sent with no
// body at all; anything that is not exactly that is refused, in one sentence
// that names no type of the server's.
//
// What is approved is the command the request sealed. A decision that names a
// command, outputs or a visit key of its own is refused, never read: a
// lenient reading would let an approver believe they had approved what they
// sent.
func TestADecisionIsReadExactlyOrRefused(t *testing.T) {
	t.Parallel()
	for body, want := range map[string]string{
		"": "", "  \n\t": "", "{}": "", ` { } `: "", `{"reason":"checked"}`: "checked", ` {"reason":"a\nb"} `: "a\nb",
		`{"reason":null}`: "", `{"reason":""}`: "",
	} {
		got, err := decodeDecision(t, body)
		if err != nil || got.Reason != want || got.ID != aRequestID {
			t.Errorf("%q: %+v, %v; want the reason %q and the id from the address", body, got, err, want)
		}
	}
	const sentence = "this request could not be read: send one JSON object with a reason, or nothing; name the field exactly and once"
	for what, body := range map[string]string{
		"another field":              `{"verdict":"yes"}`,
		"the field in other letters": `{"Reason":"x"}`,
		"the field said twice":       `{"reason":"a","reason":"b"}`,
		"a reason that is no text":   `{"reason":7}`,
		"a reason that is an object": `{"reason":{"reason":"x"}}`,
		"a list":                     `["reason"]`,
		"something after it":         `{"reason":"x"} {"reason":"y"}`,
		"words after it":             `{"reason":"x"} approve`,
		"not JSON":                   `reason=x`,
		"null":                       `null`,
		"a sentence":                 `"checked"`,
		"cut short":                  `{"reason":"x"`,
		"the id in the body":         `{"reason":"x","id":"` + aRequestID + `"}`,
		"a command":                  `{"reason":"x","command":{"kind":"waive","node_id":"step"}}`,
		"outputs":                    `{"reason":"x","outputs":{"approved":true}}`,
		"outputs alone":              `{"outputs":{"approved":true}}`,
		"a visit key":                `{"reason":"x","visit_key":"dv1-k"}`,
		"a step":                     `{"reason":"x","node_id":"step"}`,
		"a kind":                     `{"reason":"x","kind":"waive"}`,
		"dry_run":                    `{"reason":"x","dry_run":false}`,
		"who decided":                `{"reason":"x","decided_by":"boss"}`,
		"the request, in the body":   `{"reason":"x","request_id":"` + aRequestID + `"}`,
		"self_approved":              `{"reason":"x","self_approved":true}`,
	} {
		_, err := decodeDecision(t, body)
		if !errors.Is(err, apierr.ErrInvalidArgument) || err.Error() != apierr.Invalidf(sentence).Error() {
			t.Errorf("%s: %v, want the refusal %q", what, err, sentence)
		}
	}
	const tooLarge = "this request is larger than 16 KiB, which is more than a decision needs to say"
	if _, err := decodeDecision(t, `{"reason":"`+strings.Repeat("x", maxDecideBodyBytes)+`"}`); !errors.Is(err, apierr.ErrInvalidArgument) ||
		err.Error() != apierr.Invalidf(tooLarge).Error() {
		t.Errorf("a body larger than a decision needs: %v, want the refusal %q", err, tooLarge)
	}
	// Spaces count towards the size: a body is not read past the limit to
	// find out whether it says anything.
	if _, err := decodeDecision(t, strings.Repeat(" ", maxDecideBodyBytes+1)); !errors.Is(err, apierr.ErrInvalidArgument) ||
		err.Error() != apierr.Invalidf(tooLarge).Error() {
		t.Errorf("spaces past the limit: %v, want the refusal %q", err, tooLarge)
	}
	// A reason as long as the record keeps, in the widest characters, fits.
	if got, err := decodeDecision(t, `{"reason":"`+strings.Repeat("字", 2000)+`"}`); err != nil || len([]rune(got.Reason)) != 2000 {
		t.Errorf("a reason of 2000 characters: %v, want it read", err)
	}
}

func decodeQueue(t *testing.T, query string) (deviation.ListDeviationRequestsRequest, error) {
	t.Helper()
	decoded, err := decodeListDeviationRequestsRequest(context.Background(),
		httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/deviation-requests?"+query, nil))
	if err != nil {
		return deviation.ListDeviationRequestsRequest{}, err
	}
	return decoded.(deviation.ListDeviationRequestsRequest), nil
}

// The queue is asked for by its address: a status, a project, a page. What
// is not said is left for the service to choose, and a page that is not a
// number is the caller's to fix rather than quietly the first.
func TestTheQueueIsAskedForByItsAddress(t *testing.T) {
	t.Parallel()
	for query, want := range map[string]deviation.ListDeviationRequestsRequest{
		"":                                   {},
		"status=expired":                     {Status: "expired"},
		"project_id=p&status=s":              {Status: "s", ProjectID: "p"},
		"page=2&page_size=50":                {Page: 2, PageSize: 50},
		"page=&page_size=":                   {},
		"page=%202%20":                       {Page: 2},
		"page=-1&page_size=0":                {Page: -1},
		"page=3&something=else&status=stale": {Status: "stale", Page: 3},
	} {
		if got, err := decodeQueue(t, query); err != nil || got != want {
			t.Errorf("%q: %+v, %v; want %+v", query, got, err, want)
		}
	}
	const sentence = "page and page_size are whole numbers"
	for _, query := range []string{"page=two", "page_size=1.5", "page=1e3", "page_size=0x10", "page=99999999999999999999"} {
		if _, err := decodeQueue(t, query); !errors.Is(err, apierr.ErrInvalidArgument) || err.Error() != apierr.Invalidf(sentence).Error() {
			t.Errorf("%q: %v, want the refusal %q", query, err, sentence)
		}
	}
	// Where a page starts is the one multiplied by the other, and a product
	// that no longer fits in a number starts somewhere else. A million is far
	// below where that begins, and far above any page somebody reads.
	if got, err := decodeQueue(t, "page=1000000&page_size=1000000"); err != nil || got.Page != 1000000 || got.PageSize != 1000000 {
		t.Errorf("a millionth page: %+v, %v; want it read", got, err)
	}
	const tooFar = "page and page_size are at most 1000000"
	for _, query := range []string{"page=1000001", "page_size=1000001", "page=9223372036854775807", "page_size=9223372036854775807&page=2"} {
		if _, err := decodeQueue(t, query); !errors.Is(err, apierr.ErrInvalidArgument) || err.Error() != apierr.Invalidf(tooFar).Error() {
			t.Errorf("%q: %v, want the refusal %q", query, err, tooFar)
		}
	}
}

// The request is named in the address and nowhere else.
func TestARequestIsReadFromTheAddress(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequestWithContext(context.Background(), "GET", "/api/v1/deviation-requests/"+aRequestID, strings.NewReader(`{"id":"another"}`))
	r.SetPathValue("id", aRequestID)
	decoded, err := decodeGetDeviationRequestRequest(context.Background(), r)
	if got, ok := decoded.(deviation.GetDeviationRequestRequest); err != nil || !ok || got.ID != aRequestID {
		t.Fatalf("%+v, %v; want the id from the address", decoded, err)
	}
}
