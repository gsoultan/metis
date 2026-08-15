package pagination_test

import (
	"strings"
	"testing"

	"github.com/gsoultan/gobpm/server/endpoints/process"
)

// A project id that does not parse must not widen the query.
//
// The endpoint discarded the parse error, leaving uuid.Nil — which
// ListInstancesPaged reads as "no project filter" and answers with every
// instance the tenant can see. A typo in an id silently turned a project
// listing into a tenant-wide one.
func TestListInstancesRefusesAMalformedProjectID(t *testing.T) {
	ep := process.MakeListInstancesEndpoint(nil)

	got, err := ep(t.Context(), process.ListInstancesRequest{ProjectID: "not-a-uuid"})
	if err != nil {
		t.Fatalf("the endpoint returned a transport error: %v", err)
	}

	res, ok := got.(process.ListInstancesResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", got)
	}
	if res.Err == nil {
		t.Fatal("a malformed project id was accepted; the listing would not have been scoped to a project")
	}
	if !strings.Contains(res.Err.Error(), "not a valid identifier") {
		t.Errorf("the error does not explain what was wrong: %v", res.Err)
	}
	if len(res.Instances) != 0 {
		t.Errorf("instances were returned for a malformed project id: %d", len(res.Instances))
	}
}
