package userimport_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/endpoints/participant"
)

// pagedParticipants is a paged answer as it crosses the wire.
type pagedParticipants struct {
	Participants []entities.WorkflowUser `json:"participants"`
	Page         *participant.PageInfo   `json:"page"`
	Err          string                  `json:"error"`
}

const pagedDirectory = "username,display_name,email\n" +
	"erin,Erin Example,erin@example.com\n" +
	"dave,Dave Fifty%,dave@example.org\n" +
	"carol,Carol Smith,carol@example.com\n" +
	"bob,Bob Smith,bob_s@example.org\n" +
	"ada,Ada Lovelace,ada@example.com\n"

// The People page reads the directory a page at a time, and its search has
// to find people on pages it is not showing.
func TestTheDirectoryIsReadAPageAtATime(t *testing.T) {
	server, projectID := serveParticipants(t)
	upload(t, server, projectID, pagedDirectory)

	cases := []struct {
		name    string
		query   string
		want    []string
		total   int64
		hasMore bool
	}{
		{"the first page", "&page=1&page_size=2", []string{"ada", "bob"}, 5, true},
		{"a middle page", "&page=2&page_size=2", []string{"carol", "dave"}, 5, true},
		{"the last page", "&page=3&page_size=2", []string{"erin"}, 5, false},
		{"past the end", "&page=9&page_size=2", []string{}, 5, false},
		{"a search by display name", "&q=smith", []string{"bob", "carol"}, 2, false},
		{"a search by email", "&q=example.org", []string{"bob", "dave"}, 2, false},
		{"a search, paged", "&q=smith&page_size=1", []string{"bob"}, 2, true},
		{"a search for LIKE's wildcards", "&q=" + url.QueryEscape("_s@"), []string{"bob"}, 1, false},
		{"a percent sign is literal", "&q=" + url.QueryEscape("%"), []string{"dave"}, 1, false},
		{"a search nobody matches", "&q=zed", []string{}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := listParticipantsPage(t, server, projectID, tc.query)
			if got.Page == nil {
				t.Fatalf("a paged request should say which page it answered, got none")
			}
			if names := usernames(got.Participants); strings.Join(names, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("got %v, want %v", names, tc.want)
			}
			if got.Page.Total != tc.total || got.Page.HasMore != tc.hasMore {
				t.Fatalf("page = %+v, want total %d and has_more %v", *got.Page, tc.total, tc.hasMore)
			}
		})
	}
}

// Asking for nothing about pages answers what it always did: everybody, with
// no page, because the designer's picker and a sync read the whole list.
func TestAnUnpagedListStillAnswersEverybody(t *testing.T) {
	server, projectID := serveParticipants(t)
	upload(t, server, projectID, pagedDirectory)

	got := listParticipantsPage(t, server, projectID, "")
	if got.Page != nil {
		t.Fatalf("an unpaged request should not answer with a page, got %+v", *got.Page)
	}
	if len(got.Participants) != 5 {
		t.Fatalf("an unpaged request should answer everybody, got %v", usernames(got.Participants))
	}
}

// The page size is clamped to the contract's bound, and the answer says what
// was served rather than what was asked for.
func TestAPageSizeIsClamped(t *testing.T) {
	server, projectID := serveParticipants(t)
	upload(t, server, projectID, pagedDirectory)

	got := listParticipantsPage(t, server, projectID, "&page_size=100000")
	if got.Page == nil || got.Page.PageSize != 200 || got.Page.Page != 1 {
		t.Fatalf("page_size=100000 should be served as 200 on page 1, got %+v", got.Page)
	}
}

// A search is bounded: a name or an address is far shorter than this.
func TestAnOverlongSearchIsRefused(t *testing.T) {
	server, projectID := serveParticipants(t)

	got := listParticipantsPage(t, server, projectID, "&q="+strings.Repeat("a", 201))
	if got.Err == "" {
		t.Fatalf("a 201-character search should be refused, got %v", usernames(got.Participants))
	}
}

func listParticipantsPage(t *testing.T, server *httptest.Server, projectID uuid.UUID, extra string) pagedParticipants {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		fmt.Sprintf("%s/api/v1/participants?project_id=%s%s", server.URL, projectID, extra), nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	var listed pagedParticipants
	if err := json.NewDecoder(response.Body).Decode(&listed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return listed
}
