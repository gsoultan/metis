package decision_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// The decision list showed one row per stored version, so a decision edited
// five times appeared five times, and nothing on any of the rows said which of
// them was in force. Now that every save is a version, that is every decision
// anybody has ever changed. The list is one row per decision: the live version
// — what opening it opens, and what a step with no version reads — the newest,
// and when either last changed; searched and paged on the server as before.

// listed is one row of the decision list, as the API sends it.
type listed struct {
	id, key, name, hitPolicy string
	version, live, newest    int
	lastChanged              string
}

func (a *decisionAPI) list(t *testing.T, search string) []listed {
	t.Helper()
	query := url.Values{"project_id": {a.project.String()}, "page": {"1"}, "page_size": {"25"}}
	if search != "" {
		query.Set("q", search)
	}
	status, body := a.as(t, entities.RoleUser, http.MethodGet, "/api/v1/decisions/summaries?"+query.Encode(), nil)
	if status != http.StatusOK {
		t.Fatalf("list: status %d (%v)", status, body)
	}
	rows, _ := body["summaries"].([]any)
	out := make([]listed, 0, len(rows))
	for _, entry := range rows {
		row, _ := entry.(map[string]any)
		number := func(field string) int { value, _ := row[field].(float64); return int(value) }
		text := func(field string) string { value, _ := row[field].(string); return value }
		out = append(out, listed{
			id: text("id"), key: text("key"), name: text("name"), hitPolicy: text("hit_policy"),
			version: number("version"), live: number("live_version"), newest: number("newest_version"),
			lastChanged: text("last_changed_at"),
		})
	}
	return out
}

func TestTheDecisionListShowsEachDecisionOnceWithItsLiveAndNewestVersions(t *testing.T) {
	api := newDecisionAPI(t)
	first := api.create(t, "HIGH")
	second, _ := api.save(t, first, "VERY HIGH", false)["id"].(string)
	api.save(t, second, "TOP", true)
	api.createTable(t, api.tableFor("risk", "Appetite for risk", "LOW"))

	rows := api.list(t, "")
	if len(rows) != 2 || rows[0].key != "credit-band" || rows[1].key != "risk" {
		t.Fatalf("the list holds %+v, want credit-band and risk once each, by key", rows)
	}
	credit := rows[0]
	if credit.id != second || credit.version != 2 || credit.live != 2 || credit.newest != 3 {
		t.Errorf("credit-band is listed as v%d (%s), live v%d, newest v%d; want the live v2 (%s), with v3 staged",
			credit.version, credit.id, credit.live, credit.newest, second)
	}
	if credit.name != "Credit band" || credit.hitPolicy != entities.HitPolicyFirst || credit.lastChanged == "" {
		t.Errorf("credit-band's row says name %q, hit policy %q, last changed %q", credit.name, credit.hitPolicy, credit.lastChanged)
	}
	if risk := rows[1]; risk.live != 1 || risk.newest != 1 {
		t.Errorf("risk is listed live v%d, newest v%d; want v1 for both", risk.live, risk.newest)
	}

	// Searched on the server, by key or by name, ignoring case.
	for search, want := range map[string]string{"CREDIT": "credit-band", "appetite": "risk"} {
		found := api.list(t, search)
		if len(found) != 1 || found[0].key != want {
			t.Errorf("searching %q found %+v, want only %s", search, found, want)
		}
	}
}
