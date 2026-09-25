package metrics

import (
	"slices"

	"github.com/prometheus/client_golang/prometheus"
)

// tenantScopeCollector reports what the strict tenant scope has denied, read at
// scrape time.
//
// Two series, because each alone misleads. The scope's failure mode is silence
// — a path that forgot its identity reads nothing and says nothing — so zero
// denied sites means "clean" only when the scope is actually on; with the flag
// unset it means nothing at all. docs/strict-tenant-scope.md warns about exactly
// that: check the flag first. So the flag is a series too.
//
// One series per denied call site, labelled with the site, so a staging
// rollout is "read this list" rather than grep the logs for it. Bounded by the
// amount of code that can reach a repository, not by traffic: the sites are
// keyed by program counter.
type tenantScopeCollector struct {
	enabled     func() bool
	sites       func() []string
	enabledDesc *prometheus.Desc
	siteDesc    *prometheus.Desc
}

// NewTenantScopeCollector reports the strict tenant scope's state from the two
// functions that know it.
func NewTenantScopeCollector(enabled func() bool, sites func() []string) prometheus.Collector {
	return &tenantScopeCollector{
		enabled: enabled,
		sites:   sites,
		enabledDesc: prometheus.NewDesc("metis_strict_tenant_scope_enabled",
			"1 when the strict tenant scope is on. Denied sites mean nothing while this is 0.", nil, nil),
		siteDesc: prometheus.NewDesc("metis_strict_tenant_scope_denied_site",
			"A call site that reached a repository with neither a tenant nor a system identity, "+
				"and was answered with nothing. The site label names the path that has to change.",
			[]string{"site"}, nil),
	}
}

func (c *tenantScopeCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.enabledDesc
	ch <- c.siteDesc
}

func (c *tenantScopeCollector) Collect(ch chan<- prometheus.Metric) {
	enabled := 0.0
	if c.enabled() {
		enabled = 1
	}
	ch <- prometheus.MustNewConstMetric(c.enabledDesc, prometheus.GaugeValue, enabled)
	// Sorted and de-duplicated: a registry refuses a series collected twice in
	// one scrape, and a stable order keeps the output diffable.
	sites := slices.Compact(slices.Sorted(slices.Values(c.sites())))
	for _, site := range sites {
		ch <- prometheus.MustNewConstMetric(c.siteDesc, prometheus.GaugeValue, 1, site)
	}
}
