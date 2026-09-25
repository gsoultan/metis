package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestTheTenantScopeSaysWhetherItIsOnAndWhatItDenied(t *testing.T) {
	sites := []string{
		"gormProcessRepository.GetForUpdate (process.go:54) called from Engine.GetInstanceForUpdate",
		"gormJobRepository.ListPending (job.go:88) called from jobService.dispatchPendingJobs",
		// The same site twice must not fail the scrape.
		"gormJobRepository.ListPending (job.go:88) called from jobService.dispatchPendingJobs",
	}
	collector := NewTenantScopeCollector(func() bool { return true }, func() []string { return sites })

	if got := gathered(t, collector, "metis_strict_tenant_scope_enabled"); got != 1 {
		t.Fatalf("enabled = %v, want 1", got)
	}
	if got := testutil.CollectAndCount(collector, "metis_strict_tenant_scope_denied_site"); got != 2 {
		t.Fatalf("%d denied-site series, want the two distinct sites", got)
	}
}

// With the flag off nothing is ever denied, so an empty list must not read as a
// clean rollout: the enabled series says why it is empty.
func TestAScopeThatIsOffSaysSo(t *testing.T) {
	collector := NewTenantScopeCollector(func() bool { return false }, func() []string { return nil })
	if got := testutil.CollectAndCount(collector, "metis_strict_tenant_scope_denied_site"); got != 0 {
		t.Fatalf("%d denied sites with the scope off", got)
	}
	if got := testutil.CollectAndCount(collector, "metis_strict_tenant_scope_enabled"); got != 1 {
		t.Fatal("the enabled series is missing, so an empty list cannot be told from a clean one")
	}
}

// gathered is the value of a single-series gauge a collector reports.
func gathered(t *testing.T, collector prometheus.Collector, name string) float64 {
	t.Helper()
	registry := prometheus.NewRegistry()
	registry.MustRegister(collector)
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, family := range families {
		if family.GetName() == name && len(family.GetMetric()) == 1 {
			return family.GetMetric()[0].GetGauge().GetValue()
		}
	}
	t.Fatalf("no single-series %s gathered", name)
	return 0
}
