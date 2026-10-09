package deviation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
)

type pendingView struct {
	RequestID   string    `json:"request_id"`
	Status      string    `json:"status"`
	RequestedBy string    `json:"requested_by"`
	ExpiresAt   time.Time `json:"expires_at"`
	Because     []string  `json:"because"`
}

type requestReply struct {
	Request struct {
		ID             string         `json:"id"`
		Kind           string         `json:"kind"`
		Status         string         `json:"status"`
		RequestedBy    string         `json:"requested_by"`
		DecidedBy      string         `json:"decided_by"`
		DecisionReason string         `json:"decision_reason"`
		DecidedAt      time.Time      `json:"decided_at"`
		ExpiresAt      time.Time      `json:"expires_at"`
		SelfApproved   bool           `json:"self_approved"`
		Instances      []string       `json:"instances"`
		InstancesInAll int            `json:"instances_in_all"`
		Because        []string       `json:"because"`
		Plan           map[string]any `json:"plan"`
		Command        map[string]any `json:"command"`
		Outcome        map[string]any `json:"outcome"`
	} `json:"request"`
	Applied   bool           `json:"applied"`
	Deviation map[string]any `json:"deviation"`
}

// queueReply is a page of the queue, each request as it is written.
type queueReply struct {
	Requests []map[string]any `json:"requests"`
	Total    int64            `json:"total"`
}

// requestFields is the fields a request read by itself has, before anybody
// has decided it. A waive adds instance_id; a migration adds the two
// definition ids; a decision adds decided_by and decided_at, and
// decision_reason when one was given.
var requestFields = []string{"id", "kind", "status", "project_id", "requested_by", "reason", "because", "instances",
	"instances_in_all", "command", "plan", "expires_at", "self_approved", "outcome", "created_at"}

// listedRequestFields is the fields a request has in the queue, before
// anybody has decided it. The queue does not read what was asked, what the
// requester was shown or which instances it covers, and so does not write
// them — not as nothing, and not as an empty one of each: the single read
// has them.
var listedRequestFields = []string{"id", "kind", "status", "project_id", "requested_by", "reason",
	"expires_at", "self_approved", "outcome", "created_at"}

const (
	decisionReason = "checked the ticket with finance"
	// seconderName is the second administrator a test's organization has.
	seconderName = "seconder"
	// requestStillWaits ends the refusal of an approval that the requester's
	// values, not the approver, made impossible.
	requestStillWaits = "The request is still waiting: reject it, and the waive can be asked for again."
	// noSuchRequest is what a request the caller cannot see is answered,
	// whether it is another organization's or nobody's.
	noSuchRequest = "no such request"
	// decidedOn is how a refusal writes the moment a request was decided.
	decidedOn = "2 January 2006 15:04 MST"

	requestsPath = "/api/v1/deviation-requests"
)

func requestPath(requestID string) string { return requestsPath + "/" + requestID }

// secondAdministrator is the token of the organization's second
// administrator, signed in the first time it is asked for. A test that
// compares every table asks for it before its first reading.
func (h *deviationHarness) secondAdministrator(t *testing.T) string {
	t.Helper()
	if h.seconder == "" {
		h.seconder = h.signIn(t, seconderName, entities.RoleAdmin)
	}
	return h.seconder
}

// seconded sends an apply as an organization with two administrators gets one
// done.
//
// A waive has to have waited: its apply must be the 202 of a first ask —
// nothing applied, not a replay, naming the request — or the test fails
// here. A waive the route applied on one administrator's call would otherwise
// pass every assertion a test goes on to make of "the applied waive". The
// second administrator then approves over the approve route, and that
// route's answer is given: its status, its plan, whether it applied, its
// record.
//
// A cancel and a hold are one administrator's call: what the route answers is
// given as it is, and a 202 for one of them fails the test here.
func (h *deviationHarness) seconded(t *testing.T, admin string, instanceID uuid.UUID, body map[string]any) (int, deviateReply, string) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode body: %v", err)
	}
	return h.secondedWith(t, admin, instanceID, string(encoded))
}

// secondedWith is seconded for a body already written.
func (h *deviationHarness) secondedWith(t *testing.T, admin string, instanceID uuid.UUID, body string) (int, deviateReply, string) {
	t.Helper()
	var sent struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal([]byte(body), &sent); err != nil {
		t.Fatalf("an apply that is to be seconded has to be readable: %v (%s)", err, body)
	}
	status, asked, raw := h.deviateWith(t, admin, instanceID, body)
	if sent.Kind != "waive" {
		if status == http.StatusAccepted {
			t.Fatalf("a %s answered 202: only a waive waits for a second administrator (%s)", sent.Kind, raw)
		}
		return status, asked, raw
	}
	if status != http.StatusAccepted || asked.Applied || asked.Replayed || asked.PendingApproval == nil || asked.PendingApproval.RequestID == "" {
		t.Fatalf("the apply of a waive: %d (%s), want the 202 of a first ask — nothing applied, and the request it waits on", status, raw)
	}
	status, raw = h.send(t, h.secondAdministrator(t), requestPath(asked.PendingApproval.RequestID)+"/approve", "")
	var approved deviateReply
	if status == http.StatusOK {
		if err := json.Unmarshal([]byte(raw), &approved); err != nil {
			t.Fatalf("decode: %v (%s)", err, raw)
		}
	}
	return status, approved, raw
}

// askToWaive previews and applies a waive of "step" as token's account, and
// returns the request it waits on.
func (h *deviationHarness) askToWaive(t *testing.T, token string, instanceID uuid.UUID) string {
	t.Helper()
	preview := map[string]any{"kind": "waive", "node_id": "step", "reason": routeReason}
	status, planned, raw := h.deviate(t, token, instanceID, preview)
	if status != http.StatusOK || !planned.Plan.RequiresSecondApprover {
		t.Fatalf("the preview: %d (%s), want a plan that needs a second administrator", status, raw)
	}
	apply := map[string]any{"kind": "waive", "node_id": "step", "reason": routeReason, "visit_key": planned.Plan.VisitKey, "dry_run": false}
	status, asked, raw := h.deviate(t, token, instanceID, apply)
	if status != http.StatusAccepted || asked.Applied || asked.PendingApproval == nil || asked.PendingApproval.RequestID == "" {
		t.Fatalf("the apply: %d (%s), want 202 and the request it waits on", status, raw)
	}
	return asked.PendingApproval.RequestID
}

func (h *deviationHarness) decide(t *testing.T, token, requestID, verb, reason string) (int, requestReply, string) {
	t.Helper()
	body := map[string]any{}
	if reason != "" {
		body["reason"] = reason
	}
	status, raw := h.do(t, http.MethodPost, token, requestPath(requestID)+"/"+verb, body)
	var reply requestReply
	if status == http.StatusOK {
		if err := json.Unmarshal([]byte(raw), &reply); err != nil {
			t.Fatalf("decode: %v (%s)", err, raw)
		}
	}
	return status, reply, strings.TrimSpace(raw)
}

func (h *deviationHarness) requestStatus(t *testing.T, requestID string) string {
	t.Helper()
	var status string
	if err := h.db.Raw(`SELECT status FROM deviation_requests WHERE id = ?`, requestID).Row().Scan(&status); err != nil {
		t.Fatalf("read the request: %v", err)
	}
	return status
}

func (h *deviationHarness) requestCount(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := h.db.Raw(`SELECT count(*) FROM deviation_requests`).Row().Scan(&n); err != nil {
		t.Fatalf("count the requests: %v", err)
	}
	return n
}

// call sends one request exactly as it is written — any method, any body, any
// headers — and answers the status, the reply as it came, and whether the
// Idempotency-Key header's own check said it was answering again.
func (h *deviationHarness) call(t *testing.T, method, token, path, body string, headers ...string) (status int, raw string, replayed bool) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, h.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := routeClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	var out bytes.Buffer
	if _, err := out.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read the reply: %v", err)
	}
	return resp.StatusCode, strings.TrimSpace(out.String()), resp.Header.Get("Idempotency-Replayed") == "true"
}

// readRequest reads one request over its route.
func (h *deviationHarness) readRequest(t *testing.T, token, requestID string) (int, requestReply, string) {
	t.Helper()
	status, raw, _ := h.call(t, http.MethodGet, token, requestPath(requestID), "")
	var reply requestReply
	if status == http.StatusOK {
		if err := json.Unmarshal([]byte(raw), &reply); err != nil {
			t.Fatalf("decode: %v (%s)", err, raw)
		}
	}
	return status, reply, raw
}

// queue reads a page of the queue; query is what follows the "?", or nothing.
func (h *deviationHarness) queue(t *testing.T, token, query string) (int, queueReply, string) {
	t.Helper()
	path := requestsPath
	if query != "" {
		path += "?" + query
	}
	status, raw, _ := h.call(t, http.MethodGet, token, path, "")
	var reply queueReply
	if status == http.StatusOK {
		if err := json.Unmarshal([]byte(raw), &reply); err != nil {
			t.Fatalf("decode: %v (%s)", err, raw)
		}
	}
	return status, reply, raw
}

// letTheDeadlinePass moves a request's deadline into the past: the state time
// passing makes, with no sweep having come round yet.
func (h *deviationHarness) letTheDeadlinePass(t *testing.T, requestID string) {
	t.Helper()
	if err := h.db.Exec(`UPDATE deviation_requests SET expires_at = now() - interval '1 minute' WHERE id = ?`, requestID).Error; err != nil {
		t.Fatalf("let time pass: %v", err)
	}
}
