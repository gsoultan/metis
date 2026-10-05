package deviations

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/endpoints/deviation"
)

const unreadable = "invalid argument: this request could not be read: send one JSON object with kind, reason, node_id (a cancel may leave it out), " +
	"and for a waive outputs; to apply a plan add its visit_key and \"dry_run\": false; name each field exactly and once"

func decode(t *testing.T, body string) (deviation.DeviateInstanceRequest, error) {
	t.Helper()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/instances/abc/deviations", strings.NewReader(body))
	r.SetPathValue("id", "abc")
	decoded, err := decodeDeviateInstanceRequest(context.Background(), r)
	if err != nil {
		return deviation.DeviateInstanceRequest{}, err
	}
	req, ok := decoded.(deviation.DeviateInstanceRequest)
	if !ok {
		t.Fatalf("decoded a %T, want a DeviateInstanceRequest", decoded)
	}
	return req, nil
}

// What a request says of dry_run is read exactly: true, false, or nothing
// said. Nothing else is taken for one of them.
func TestARequestSaysDryRunFalseOnlyWithTheBooleanInTheFieldNamedSo(t *testing.T) {
	t.Parallel()
	yes, no := true, false
	cases := []struct {
		name, body string
		want       *bool
	}{
		{"not said", `{"kind":"cancel"}`, nil},
		{"null", `{"kind":"cancel","dry_run":null}`, nil},
		{"true", `{"kind":"cancel","dry_run":true}`, &yes},
		{"false", `{"kind":"cancel","dry_run":false}`, &no},
		{"false, with space around it", "{\n  \"dry_run\" : false ,\n  \"kind\" : \"cancel\"\n}\n", &no},
		{"false inside the reason", `{"kind":"cancel","reason":"\"dry_run\": false"}`, nil},
		{"false inside the outputs", `{"kind":"waive","outputs":{"dry_run":false}}`, nil},
	}
	for _, c := range cases {
		req, err := decode(t, c.body)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		switch {
		case c.want == nil && req.DryRun != nil:
			t.Errorf("%s: read as dry_run %v, want nothing said", c.name, *req.DryRun)
		case c.want != nil && (req.DryRun == nil || *req.DryRun != *c.want):
			t.Errorf("%s: read as %v, want dry_run %v", c.name, req.DryRun, *c.want)
		}
	}
}

// Every field is read into the request, and the instance comes from the
// address.
func TestARequestIsReadFieldByFieldAndItsInstanceFromTheAddress(t *testing.T) {
	t.Parallel()
	req, err := decode(t, `{"kind":"waive","node_id":"step","reason":"why","outputs":{"amount":250,"approved":true,"note":"x"},"visit_key":"dv1-k","dry_run":false}`)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.InstanceID != "abc" || req.Kind != "waive" || req.NodeID != "step" || req.Reason != "why" || req.VisitKey != "dv1-k" ||
		req.DryRun == nil || *req.DryRun {
		t.Fatalf("read as %+v", req)
	}
	// Numbers are read as the task-completion route reads a form's: a number,
	// which a gateway can compare — not the digits as text.
	if amount, ok := req.Outputs["amount"].(float64); !ok || amount != 250 || req.Outputs["approved"] != true || req.Outputs["note"] != "x" {
		t.Fatalf("outputs read as %#v", req.Outputs)
	}
}

// A body that is not exactly a request is refused as the caller's mistake, in
// one sentence that names none of the server's types. It is never read as a
// preview: the caller is told, whichever way they meant it.
func TestABodyThatIsNotExactlyARequestIsRefused(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"nothing":                          ``,
		"spaces":                           "  \n",
		"null":                             `null`,
		"a list":                           `[{"kind":"cancel","dry_run":false}]`,
		"a sentence":                       `"cancel"`,
		"cut short":                        `{"kind":"cancel","dry_run":false`,
		"not JSON":                         `kind=cancel&dry_run=false`,
		"a second object":                  `{"kind":"cancel"}{"dry_run":false}`,
		"words after the object":           `{"kind":"cancel"} false`,
		"a field there is not":             `{"kind":"cancel","node":"step"}`,
		"the instance in the body":         `{"kind":"cancel","instance_id":"abc"}`,
		"a name in other letters":          `{"kind":"cancel","DRY_RUN":false}`,
		"a name without its underscore":    `{"kind":"cancel","dryRun":false}`,
		"Kind":                             `{"Kind":"cancel"}`,
		"dry_run true, then false":         `{"kind":"cancel","dry_run":true,"dry_run":false}`,
		"dry_run false, then true":         `{"kind":"cancel","dry_run":false,"dry_run":true}`,
		"dry_run false, twice":             `{"kind":"cancel","dry_run":false,"dry_run":false}`,
		"kind twice":                       `{"kind":"hold","kind":"cancel"}`,
		"visit_key twice":                  `{"kind":"cancel","visit_key":"a","visit_key":"b"}`,
		`dry_run "false"`:                  `{"kind":"cancel","dry_run":"false"}`,
		"dry_run 0":                        `{"kind":"cancel","dry_run":0}`,
		"dry_run a list":                   `{"kind":"cancel","dry_run":[false]}`,
		"dry_run an object":                `{"kind":"cancel","dry_run":{"value":false}}`,
		"dry_run False":                    `{"kind":"cancel","dry_run":False}`,
		"a kind that is a number":          `{"kind":5}`,
		"a reason that is a list":          `{"kind":"cancel","reason":["x"]}`,
		"outputs that are a list":          `{"kind":"waive","outputs":["approved"]}`,
		"a visit key that is a number":     `{"kind":"cancel","visit_key":7}`,
		"a name that is not a JSON string": `{kind:"cancel"}`,
		// An output decides which branch a gateway takes: said twice, which of
		// the two was meant is not the decoder's to choose.
		"an output named twice":                      `{"kind":"waive","outputs":{"approved":true,"approved":false}}`,
		"an output named twice, alike":               `{"kind":"waive","outputs":{"approved":true,"approved":true}}`,
		"an output named twice, once in other marks": `{"kind":"waive","outputs":{"approved":true,"\u0061pproved":false}}`,
		"a name twice inside what an output holds":   `{"kind":"waive","outputs":{"address":{"city":"Bandung","city":"Jakarta"}}}`,
		"a name twice inside a list an output holds": `{"kind":"waive","outputs":{"lines":[{"sku":"a"},{"sku":"b","sku":"c"}]}}`,
		"an output named twice, far apart":           `{"kind":"waive","outputs":{"approved":true,"amount":1,"note":{"a":[1,2,{"b":null}]},"approved":false}}`,
		"outputs cut short inside a name said twice": `{"kind":"waive","outputs":{"approved":true,"approved":`,
	} {
		_, err := decode(t, body)
		if !errors.Is(err, apierr.ErrInvalidArgument) || err.Error() != unreadable {
			t.Errorf("%s: %v, want it refused as %q", name, err, unreadable)
		}
	}
}

// A name may be used again where it names something else: in another object.
// Only a name said twice in one object is refused.
func TestANameUsedAgainInAnotherObjectOfTheOutputsIsRead(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"a field's name inside the outputs":     `{"kind":"waive","outputs":{"kind":"x","reason":"y","outputs":{"outputs":1}}}`,
		"an output's name inside what it holds": `{"kind":"waive","outputs":{"approved":{"approved":true}}}`,
		"one name in two objects of a list":     `{"kind":"waive","outputs":{"lines":[{"sku":"a"},{"sku":"b"}],"more":[{"sku":"c"}]}}`,
		"one name in two outputs' own objects":  `{"kind":"waive","outputs":{"from":{"city":"Bandung"},"to":{"city":"Jakarta"}}}`,
		"a name that is a value somewhere else": `{"kind":"waive","outputs":{"approved":"approved","list":["approved","approved"]}}`,
		"no outputs":                            `{"kind":"waive","outputs":{}}`,
		"outputs said to be nothing":            `{"kind":"waive","outputs":null}`,
	} {
		if _, err := decode(t, body); err != nil {
			t.Errorf("%s: %v, want it read", name, err)
		}
	}
}

// A request far larger than one needs to be is refused for its size, said as
// that.
func TestARequestLargerThanOneNeedsToBeIsRefusedForItsSize(t *testing.T) {
	t.Parallel()
	const tooLarge = "invalid argument: this request is larger than 256 KiB, which is more than a waive, a cancel or a hold needs to say"
	atTheLimit := `{"kind":"cancel","reason":"` + strings.Repeat("x", maxDeviateBodyBytes-len(`{"kind":"cancel","reason":""}`)) + `"}`
	if len(atTheLimit) != maxDeviateBodyBytes {
		t.Fatalf("the body at the limit is %d bytes", len(atTheLimit))
	}
	if _, err := decode(t, atTheLimit); err != nil {
		t.Errorf("a request of exactly the limit: %v, want it read", err)
	}
	if _, err := decode(t, atTheLimit+" "); !errors.Is(err, apierr.ErrInvalidArgument) || err.Error() != tooLarge {
		t.Errorf("a request one byte over the limit: %v, want %q", err, tooLarge)
	}
}
