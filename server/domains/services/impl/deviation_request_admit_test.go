package impl

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// Who may decide a request is a pure rule; the database answers only whether
// somebody else administers the organization, and only when it matters.
func TestAdmitDecider(t *testing.T) {
	ana, budi := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	request := entities.DeviationRequest{RequestedBy: "ana", RequestedByID: ana, Status: entities.DeviationRequestPending}
	another := func(answer bool) func() (bool, error) { return func() (bool, error) { return answer, nil } }
	asked := false
	notAsked := func() (bool, error) { asked = true; return false, nil }

	if d, err := admitDecider(entities.User{ID: budi, Username: "budi"}, request, "", false, notAsked); err != nil || d.SelfApproved || d.DeciderID != budi || asked {
		t.Fatalf("a second administrator: %+v %v (asked about others: %v)", d, err, asked)
	}
	if _, err := admitDecider(entities.User{Username: "budi"}, request, "", true, notAsked); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("a caller with no account id: %v", err)
	}
	for _, c := range []struct {
		name      string
		allow     bool
		other     bool
		reason    string
		wantSelf  bool
		wantError string
	}{
		{"setting off, another administrator", false, true, "x", false, "A different administrator has to approve it"},
		{"setting off, sole administrator", false, false, "x", false, "nobody else administers this organization"},
		{"setting on, another administrator", true, true, "x", false, "A different administrator has to approve it"},
		{"setting on, sole administrator, no reason", true, false, "  ", false, "Say why you are approving your own request"},
		{"setting on, sole administrator, reason", true, false, "the board agreed", true, ""},
	} {
		d, err := admitDecider(entities.User{ID: ana, Username: "ana-renamed"}, request, c.reason, c.allow, another(c.other))
		if c.wantError == "" {
			if err != nil || d.SelfApproved != c.wantSelf || d.Reason != "the board agreed" {
				t.Errorf("%s: %+v %v", c.name, d, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.wantError) {
			t.Errorf("%s: %v, want it to contain %q", c.name, err, c.wantError)
		}
	}
}

// What admitDecider leaves to nobody's good sense: a decision says who and
// when, a reason is kept without the spaces around it and has the ledger's
// limit whoever gives it, a question the database could not answer is not
// read as "nobody else", and a request that names no requester's account has
// no second administrator anybody could be shown to be.
func TestAdmitDeciderRefusesWhatItCannotTell(t *testing.T) {
	ana, budi := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	request := entities.DeviationRequest{RequestedBy: "ana", RequestedByID: ana, Status: entities.DeviationRequestPending}
	never := func() (bool, error) {
		t.Fatal("asked whether anybody else administers the organization")
		return false, nil
	}

	before := time.Now()
	d, err := admitDecider(entities.User{ID: budi, Username: "budi"}, request, "  checked with finance \n", false, never)
	if err != nil || d.Decider != "budi" || d.Reason != "checked with finance" || d.At.Before(before) || d.At.After(time.Now()) {
		t.Fatalf("a second administrator's decision: %+v %v", d, err)
	}
	long := strings.Repeat("é", entities.MaxDeviationReasonLength+1)
	if _, err := admitDecider(entities.User{ID: budi, Username: "budi"}, request, long, false, never); !errors.Is(err, apierr.ErrInvalidArgument) ||
		!strings.Contains(err.Error(), "longer than 2000 characters") {
		t.Fatalf("a second administrator's reason past the limit: %v", err)
	}
	if _, err := admitDecider(entities.User{ID: budi, Username: "budi"}, request, long[:2*entities.MaxDeviationReasonLength], false, never); err != nil {
		t.Fatalf("a reason of exactly the limit, counted in characters: %v", err)
	}
	unanswered := errors.New("the database is away")
	_, err = admitDecider(entities.User{ID: ana, Username: "ana"}, request, "the board agreed", true, func() (bool, error) { return false, unanswered })
	if !errors.Is(err, unanswered) || errors.Is(err, apierr.ErrForbidden) || errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("a question nobody answered: %v, want the failure itself and no approval", err)
	}
	if _, err := admitDecider(entities.User{ID: ana, Username: "ana"}, request, long, true, func() (bool, error) { return false, nil }); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("a self-approval's reason past the limit: %v", err)
	}
	nobodys := entities.DeviationRequest{RequestedBy: "ana", Status: entities.DeviationRequestPending}
	_, err = admitDecider(entities.User{ID: budi, Username: "budi"}, nobodys, "", true, never)
	if err == nil || errors.Is(err, apierr.ErrForbidden) || errors.Is(err, apierr.ErrInvalidArgument) || errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("a request that names no requester's account: %v, want it refused as the server's", err)
	}
}

// Who may ask for or decide a request: a signed-in administrator of the
// organization the request is for, with an account. Absent means refuse.
func TestOnlyAnAdministratorWithAnAccountAsksOrDecides(t *testing.T) {
	org, elsewhere, account := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	in := func(organization uuid.UUID, caller any) context.Context {
		ctx := context.Background()
		if organization != uuid.Nil {
			ctx = entities.WithTenantContext(ctx, entities.TenantContext{TenantID: organization.String()})
		}
		if caller != nil {
			ctx = context.WithValue(ctx, pkgauth.UserContextKey, caller)
		}
		return ctx
	}
	admin := entities.User{ID: account, Username: "ana", Roles: []string{entities.RoleAdmin}}
	if got, err := requireDecidingAdministrator(in(org, admin)); err != nil || got.ID != account || got.Username != "ana" {
		t.Fatalf("an administrator of the organization: %+v %v", got, err)
	}
	const notAnAdministrator = "only an administrator can ask for or decide a request for a second administrator"
	for name, c := range map[string]struct {
		ctx  context.Context
		want string
	}{
		"nobody signed in":                    {in(org, nil), notAnAdministrator},
		"an operator":                         {in(org, entities.User{ID: account, Username: "olga", Roles: []string{entities.RoleOperator}}), notAnAdministrator},
		"an administrator of no organization": {in(uuid.Nil, admin), notAnAdministrator},
		"an administrator elsewhere only": {in(org, entities.User{ID: account, Username: "ana",
			RolesByOrganization: map[uuid.UUID][]string{elsewhere: {entities.RoleAdmin}}}), notAnAdministrator},
		"an administrator with no name": {in(org, entities.User{ID: account, Roles: []string{entities.RoleAdmin}}), notAnAdministrator},
		"an administrator with no account id": {in(org, entities.User{Username: "ana", Roles: []string{entities.RoleAdmin}}),
			"needs an account, and this request carries none"},
	} {
		_, err := requireDecidingAdministrator(c.ctx)
		if !errors.Is(err, apierr.ErrForbidden) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want forbidden saying %q", name, err, c.want)
		}
	}
}

// A request that has been decided says what became of it, by whom and when,
// to whoever tries to decide it again.
func TestADecidedRequestSaysWhatBecameOfIt(t *testing.T) {
	at := time.Date(2026, 10, 5, 9, 30, 0, 0, time.FixedZone("WIB", 7*3600))
	deadline := time.Date(2026, 10, 8, 2, 30, 0, 0, time.UTC)
	decided := func(status entities.DeviationRequestStatus, outcome map[string]any) entities.DeviationRequest {
		return entities.DeviationRequest{ID: uuid.Must(uuid.NewV7()), Status: status, RequestedBy: "ana", DecidedBy: "budi",
			DecidedAt: &at, ExpiresAt: deadline, UpdatedAt: at, Outcome: outcome}
	}
	const when = "5 October 2026 02:30 UTC"
	for status, want := range map[entities.DeviationRequestStatus]string{
		entities.DeviationRequestApproved: "budi approved this on " + when + "; it is being applied.",
		entities.DeviationRequestApplied:  "budi approved this on " + when + ", and it was applied.",
		entities.DeviationRequestRejected: "budi rejected this on " + when + ".",
		entities.DeviationRequestExpired:  "This request expired on 8 October 2026 02:30 UTC.",
		entities.DeviationRequestStale:    "This request went stale on " + when + ": it no longer held when somebody came to approve it.",
	} {
		// Judged a minute after the approval: its run window is open.
		err := decidedRefusalAt(decided(status, nil), at.Add(time.Minute))
		if !errors.Is(err, apierr.ErrInvalidArgument) || err.Error() != apierr.Invalidf("%s", want).Error() {
			t.Errorf("%s: %v, want a refusal saying exactly %q", status, err, want)
		}
	}
	// A request stored as approved is being applied only while its run window
	// is open. Once it has closed — an hour after the approval, or at the
	// request's own deadline — it is not, whatever the table still says.
	closed := "budi approved this on " + when + ", and no run of it reported back in the time one is given; it is no longer in use. Ask again."
	if err := decidedRefusalAt(decided(entities.DeviationRequestApproved, nil), at.Add(time.Hour)); !errors.Is(err, apierr.ErrInvalidArgument) || err.Error() != apierr.Invalidf("%s", closed).Error() {
		t.Errorf("approved, an hour after the approval: %v, want %q", err, closed)
	}
	nearly := deadline.Add(-time.Minute)
	late := decided(entities.DeviationRequestApproved, nil)
	late.DecidedAt = &nearly
	if err := decidedRefusalAt(late, deadline); !errors.Is(err, apierr.ErrInvalidArgument) || !strings.HasSuffix(err.Error(), "it is no longer in use. Ask again.") {
		t.Errorf("approved a minute before its deadline, read at the deadline: %v, want it no longer in use", err)
	}
	if err := decidedRefusalAt(late, nearly.Add(time.Second)); !strings.HasSuffix(err.Error(), "; it is being applied.") {
		t.Errorf("approved a minute before its deadline, read a second later: %v, want it being applied", err)
	}
	// An applied request whose run changed nothing says so, and which way.
	for note, want := range map[string]string{
		"every instance the run reached was passed over":    "budi approved this on " + when + ", and its run changed nothing: every instance the run reached was passed over. Ask again for what remains.",
		"no instance was active on the version when it ran": "budi approved this on " + when + ", and its run changed nothing: no instance was active on the version when it ran. Ask again for what remains.",
	} {
		spent := decided(entities.DeviationRequestApplied, map[string]any{"changed": float64(0), "note": note})
		if err := decidedRefusalAt(spent, at.Add(time.Minute)); !errors.Is(err, apierr.ErrInvalidArgument) || err.Error() != apierr.Invalidf("%s", want).Error() {
			t.Errorf("applied, having changed nothing: %v, want %q", err, want)
		}
	}
	interrupted := decidedRefusal(decided(entities.DeviationRequestInterrupted, map[string]any{"error": "the run did not report back"}))
	if want := "budi approved this on " + when + ", and the run stopped part-way: the run did not report back. Ask again for what remains."; !errors.Is(interrupted, apierr.ErrInvalidArgument) || !strings.HasSuffix(interrupted.Error(), want) {
		t.Errorf("interrupted: %v, want %q", interrupted, want)
	}
	// A stale request names nobody and may carry no time of its own: it went
	// stale when its row was last written.
	stale := decided(entities.DeviationRequestStale, nil)
	stale.DecidedAt, stale.DecidedBy = nil, ""
	if err := decidedRefusal(stale); !strings.Contains(err.Error(), "went stale on "+when) {
		t.Errorf("a stale request with no time of its own: %v", err)
	}
	// One that still waits has not been decided: saying it had would be the
	// code's mistake, never an answer for whoever asked.
	waiting := decidedRefusal(decided(entities.DeviationRequestPending, nil))
	if waiting == nil || errors.Is(waiting, apierr.ErrInvalidArgument) || errors.Is(waiting, apierr.ErrForbidden) {
		t.Errorf("a request that still waits: %v, want a plain error", waiting)
	}
}

// A request its own requester approved says so to whoever tries to decide it
// again: "ana approved this" alone would read as a second administrator's
// approval. Told from the account ids, as the request tells it — and only of
// an approval: a request its requester rejected was withdrawn.
func TestADecidedRequestSaysWhenItsRequesterApprovedIt(t *testing.T) {
	at := time.Date(2026, 10, 5, 2, 30, 0, 0, time.UTC)
	ana := uuid.Must(uuid.NewV7())
	own := func(status entities.DeviationRequestStatus, outcome map[string]any) entities.DeviationRequest {
		return entities.DeviationRequest{ID: uuid.Must(uuid.NewV7()), Status: status, RequestedBy: "ana", RequestedByID: ana,
			DecidedBy: "ana", DecidedByID: ana, DecidedAt: &at, ExpiresAt: at.Add(72 * time.Hour), UpdatedAt: at, Outcome: outcome}
	}
	const approved = "ana approved this — their own request, with no second administrator — on 5 October 2026 02:30 UTC"
	for status, want := range map[entities.DeviationRequestStatus]string{
		entities.DeviationRequestApproved:    approved + "; it is being applied.",
		entities.DeviationRequestApplied:     approved + ", and it was applied.",
		entities.DeviationRequestInterrupted: approved + ", and the run stopped part-way: the run did not report back. Ask again for what remains.",
		entities.DeviationRequestRejected:    "ana rejected this on 5 October 2026 02:30 UTC.",
	} {
		// Judged a minute after the approval: its run window is open.
		err := decidedRefusalAt(own(status, map[string]any{"error": "the run did not report back"}), at.Add(time.Minute))
		if !errors.Is(err, apierr.ErrInvalidArgument) || err.Error() != apierr.Invalidf("%s", want).Error() {
			t.Errorf("%s: %v, want a refusal saying exactly %q", status, err, want)
		}
	}
}

// Two deciders meet at the request's row, and the second reads what the first
// left. Should a decision ever be written past that row, the repository still
// refuses the second — and that refusal is somebody's request, decided, not
// the server's failure.
func TestADecisionSomebodyElseMadeFirstIsNotTheServersFailure(t *testing.T) {
	at := time.Date(2026, 10, 5, 2, 30, 0, 0, time.UTC)
	applied := entities.DeviationRequest{Status: entities.DeviationRequestApplied, DecidedBy: "budi", DecidedAt: &at}
	reread := func() (entities.DeviationRequest, error) { return applied, nil }
	for name, lost := range map[string]error{
		"the request was decided first": repocontracts.ErrDeviationRequestDecided,
		"the row was decided first":     repocontracts.ErrDeviationRowDecided,
		"wrapped on the way up":         errors.Join(errors.New("deciding"), repocontracts.ErrDeviationRequestDecided),
	} {
		err := decidedFirst(lost, reread)
		if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "budi approved this on 5 October 2026 02:30 UTC, and it was applied.") {
			t.Errorf("%s: %v, want it told as a request already decided", name, err)
		}
	}
	other := errors.New("the database is away")
	if err := decidedFirst(other, reread); !errors.Is(err, other) || errors.Is(err, apierr.ErrInvalidArgument) {
		t.Errorf("any other failure: %v, want it as it is", err)
	}
	if err := decidedFirst(nil, reread); err != nil {
		t.Errorf("no failure: %v", err)
	}
	unread := errors.New("could not read it again")
	if err := decidedFirst(repocontracts.ErrDeviationRowDecided, func() (entities.DeviationRequest, error) { return entities.DeviationRequest{}, unread }); !errors.Is(err, unread) || errors.Is(err, apierr.ErrInvalidArgument) {
		t.Errorf("a request that could not be read again: %v, want that failure", err)
	}
}
