package postgres_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/interceptors/security"
	"github.com/gsoultan/metis/tests/testutils"
)

// An answer, once kept, is the answer: a late writer does not overwrite it and
// abandoning the key does not drop it. An unanswered claim abandoned is free
// for the next caller.
func TestAnIdempotencyAnswerIsKeptOnce(t *testing.T) {
	db := testutils.SetupTestDB(t)
	store := security.NewDBIdempotencyStore(db, time.Hour)
	ctx := entities.WithSystemContext(t.Context())

	if out, err := store.Claim(ctx, "kept", "h"); err != nil || !out.Owned {
		t.Fatalf("claim: %+v %v", out, err)
	}
	header := http.Header{"Location": []string{"/instances/1"}}
	if err := store.Complete(ctx, "kept", security.StoredResponse{StatusCode: 201, Header: header, Body: []byte("first")}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if err := store.Complete(ctx, "kept", security.StoredResponse{StatusCode: 500, Body: []byte("late")}); err != nil {
		t.Fatalf("late complete: %v", err)
	}
	if err := store.Abandon(ctx, "kept"); err != nil {
		t.Fatalf("abandon: %v", err)
	}
	out, err := store.Claim(ctx, "kept", "h")
	if err != nil || out.Owned || out.Response == nil {
		t.Fatalf("a retry was not replayed: %+v %v", out, err)
	}
	if r := out.Response; r.StatusCode != 201 || string(r.Body) != "first" || r.Header.Get("Location") != "/instances/1" {
		t.Errorf("replayed %d %q %v, want the first answer", r.StatusCode, r.Body, r.Header)
	}

	if out, err := store.Claim(ctx, "dropped", "h"); err != nil || !out.Owned {
		t.Fatalf("claim: %+v %v", out, err)
	}
	if out, err := store.Claim(ctx, "dropped", "h"); err != nil || out.Owned || out.Response != nil {
		t.Fatalf("a running claim was taken twice: %+v %v", out, err)
	}
	if err := store.Abandon(ctx, "dropped"); err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if out, err := store.Claim(ctx, "dropped", "h"); err != nil || !out.Owned {
		t.Errorf("an abandoned claim was not free: %+v %v", out, err)
	}
}
