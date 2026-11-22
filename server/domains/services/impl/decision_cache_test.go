package impl

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

type countedRead struct {
	reads int
	err   error
}

func (r *countedRead) read() (models.DecisionDefinitionModel, error) {
	r.reads++
	return models.DecisionDefinitionModel{Key: "risk", Version: r.reads}, r.err
}

func testDecisionCache(ttl time.Duration) (*decisionCache, *time.Time) {
	now := time.Unix(1_000_000, 0)
	c := newDecisionCache()
	c.ttl = ttl
	c.now = func() time.Time { return now }
	return c, &now
}

func TestADecisionIsReadOncePerTTL(t *testing.T) {
	c, now := testDecisionCache(time.Second)
	r := &countedRead{}
	project := uuid.New()
	for range 5 {
		if _, err := c.get(t.Context(), project, "risk", 0, r.read); err != nil {
			t.Fatal(err)
		}
	}
	if r.reads != 1 {
		t.Fatalf("%d reads within the TTL, want 1", r.reads)
	}
	*now = now.Add(time.Second)
	if m, _ := c.get(t.Context(), project, "risk", 0, r.read); r.reads != 2 || m.Version != 2 {
		t.Fatalf("after the TTL: %d reads, version %d; want a fresh read", r.reads, m.Version)
	}
}

func TestADecisionCachedForOneTenantIsNotServedToAnother(t *testing.T) {
	c, _ := testDecisionCache(time.Minute)
	r := &countedRead{}
	project := uuid.New()
	acme := entities.WithTenantContext(context.Background(), entities.TenantContext{TenantID: uuid.NewString()})
	globex := entities.WithTenantContext(context.Background(), entities.TenantContext{TenantID: uuid.NewString()})
	_, _ = c.get(acme, project, "risk", 0, r.read)
	_, _ = c.get(globex, project, "risk", 0, r.read)
	if r.reads != 2 {
		t.Fatalf("%d reads for two tenants, want one each: one tenant was served the other's table", r.reads)
	}
}

func TestAPinnedVersionAndTheLiveOneAreHeldApart(t *testing.T) {
	c, _ := testDecisionCache(time.Minute)
	r := &countedRead{}
	project := uuid.New()
	_, _ = c.get(t.Context(), project, "risk", 0, r.read)
	_, _ = c.get(t.Context(), project, "risk", 3, r.read)
	if r.reads != 2 {
		t.Fatalf("%d reads, want the live version and v3 read separately", r.reads)
	}
}

func TestAFailedReadIsNotKept(t *testing.T) {
	c, _ := testDecisionCache(time.Minute)
	r := &countedRead{err: errors.New("not found")}
	project := uuid.New()
	_, _ = c.get(t.Context(), project, "risk", 0, r.read)
	r.err = nil
	if _, err := c.get(t.Context(), project, "risk", 0, r.read); err != nil || r.reads != 2 {
		t.Fatalf("err %v after %d reads: a failure was served from the cache", err, r.reads)
	}
}

func TestForgettingAndAZeroTTLReadAgain(t *testing.T) {
	c, _ := testDecisionCache(time.Minute)
	r := &countedRead{}
	project := uuid.New()
	_, _ = c.get(t.Context(), project, "risk", 0, r.read)
	c.forget()
	_, _ = c.get(t.Context(), project, "risk", 0, r.read)
	if r.reads != 2 {
		t.Fatalf("%d reads, want a read after forget", r.reads)
	}
	off, _ := testDecisionCache(0)
	_, _ = off.get(t.Context(), project, "risk", 0, r.read)
	_, _ = off.get(t.Context(), project, "risk", 0, r.read)
	if r.reads != 4 {
		t.Fatalf("%d reads with the cache off, want every one read", r.reads)
	}
}

func TestTheDecisionCacheTTLSetting(t *testing.T) {
	for raw, want := range map[string]time.Duration{
		"": time.Second, "0s": 0, "5s": 5 * time.Second, "2m": time.Second, "-1s": time.Second, "x": time.Second,
	} {
		t.Setenv(envDecisionCacheTTL, raw)
		if got := decisionCacheTTL(); got != want {
			t.Errorf("%s=%q: %s, want %s", envDecisionCacheTTL, raw, got, want)
		}
	}
}
