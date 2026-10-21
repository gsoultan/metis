package externaltask_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type workersList struct {
	LiveWindowSeconds int64 `json:"live_window_seconds"`
	Topics            []struct {
		Topic    string `json:"topic"`
		Waiting  int64  `json:"waiting"`
		Locked   int64  `json:"locked"`
		Unserved bool   `json:"unserved"`
		Workers  []struct {
			WorkerID   string  `json:"worker_id"`
			Version    string  `json:"version"`
			LastSeenAt *string `json:"last_seen_at"`
			Live       bool    `json:"live"`
			Locked     int64   `json:"locked"`
		} `json:"workers"`
	} `json:"topics"`
}

func (w world) workers(t *testing.T, token string) workersList {
	t.Helper()
	status, body := call(t, w.server, http.MethodGet, "/api/v1/external-task-workers", token, nil)
	var list workersList
	if status != http.StatusOK {
		t.Fatalf("list the workers: status %d (%s)", status, body)
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatalf("read the workers: %v (%s)", err, body)
	}
	return list
}

// A charge step with nobody fetching it waits, and until now nothing said so.
// The list says the topic is unserved; once a worker fetches, it names the
// worker, the version it said it was and what it holds — and only to the
// organization whose tasks they are.
func TestTheWorkersListSaysWhoServesATopicAndWhichHaveNobody(t *testing.T) {
	w := newWorld(t)

	before := w.workers(t, w.member)
	if before.LiveWindowSeconds != 120 {
		t.Errorf("the live window is %ds, want 120", before.LiveWindowSeconds)
	}
	if len(before.Topics) != 1 || before.Topics[0].Topic != topic {
		t.Fatalf("topics before any worker: %+v, want the charge topic alone", before.Topics)
	}
	if got := before.Topics[0]; got.Waiting != 1 || !got.Unserved || len(got.Workers) != 0 {
		t.Fatalf("the charge topic before any worker is %+v; want one task waiting, unserved, no workers", got)
	}

	status, body := call(t, w.server, http.MethodPost, "/api/v1/external-tasks/fetch-and-lock", w.member, map[string]any{
		"topic": topic, "worker_id": holder, "max_tasks": 1, "lock_duration_ms": 60_000, "worker_version": "metis-sdk-go/0.6.0",
	})
	if status != http.StatusOK || !strings.Contains(body, topic) {
		t.Fatalf("fetch-and-lock with a version: status %d (%s)", status, body)
	}

	after := w.workers(t, w.member)
	if len(after.Topics) != 1 {
		t.Fatalf("topics after a fetch: %+v", after.Topics)
	}
	got := after.Topics[0]
	if got.Waiting != 0 || got.Locked != 1 || got.Unserved {
		t.Errorf("the charge topic after a fetch is %+v; want nothing waiting, one locked, served", got)
	}
	if len(got.Workers) != 1 {
		t.Fatalf("workers on the charge topic: %+v, want the one that fetched", got.Workers)
	}
	worker := got.Workers[0]
	if worker.WorkerID != holder || worker.Version != "metis-sdk-go/0.6.0" || !worker.Live || worker.Locked != 1 || worker.LastSeenAt == nil {
		t.Errorf("the worker is listed as %+v; want %s, its version, live, holding one, with when it was seen", worker, holder)
	}

	if other := w.workers(t, w.outsider); len(other.Topics) != 0 {
		t.Errorf("another organization was shown this one's topics: %+v", other.Topics)
	}
}

// A version is a label shown to people, so one too long to show, or carrying
// control characters, is refused rather than cut or stored. The fetch answers a
// refusal the way it answers every other: in its error field.
func TestAFetchSayingAnUnreadableVersionIsRefused(t *testing.T) {
	w := newWorld(t)

	for name, version := range map[string]string{
		"too long":   strings.Repeat("v", 65),
		"a new line": "1.0\nadmin",
	} {
		status, body := call(t, w.server, http.MethodPost, "/api/v1/external-tasks/fetch-and-lock", w.member, map[string]any{
			"topic": topic, "worker_id": holder, "max_tasks": 1, "lock_duration_ms": 60_000, "worker_version": version,
		})
		var reply struct {
			Tasks []json.RawMessage `json:"tasks"`
			Error string            `json:"error"`
		}
		if err := json.Unmarshal([]byte(body), &reply); err != nil {
			t.Fatalf("read the fetch's reply: status %d, %v (%s)", status, err, body)
		}
		if len(reply.Tasks) != 0 || !strings.Contains(reply.Error, "worker_version") {
			t.Errorf("a version that is %s: %s; want no tasks and an error naming worker_version", name, body)
		}
	}
	if list := w.workers(t, w.member); len(list.Topics) != 1 || list.Topics[0].Waiting != 1 || len(list.Topics[0].Workers) != 0 {
		t.Errorf("a refused fetch locked or recorded something: %+v", list.Topics)
	}
}
