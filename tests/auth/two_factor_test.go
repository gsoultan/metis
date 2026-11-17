package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/app"
	"github.com/gsoultan/metis/internal/pkg/health"
	"github.com/gsoultan/metis/internal/pkg/loginthrottle"
	"github.com/gsoultan/metis/internal/pkg/totp"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/endpoints"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// newTwoFactorHarness serves the API the way a production server does by
// default: every staff password sign-in asks for a code.
func newTwoFactorHarness(t *testing.T) *apiHarness {
	t.Helper()
	t.Setenv("METIS_AUTH_CACHE_TTL", "0s")
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	sse := observersimpl.NewSSEObserver()
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), sse,
		localTokenSecret, nil, nil, nil, services.WithSecondFactor(serviceimpl.SecondFactorRequired))
	handler, _ := app.BuildAPIHandler(svc, endpoints.MakeEndpoints(svc), sse, nil,
		map[string]health.Checker{}, db)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &apiHarness{t: t, svc: svc, server: server, db: db}
}

// signInReply is what /api/v1/login and the steps after it answer.
type signInReply struct {
	Token         string   `json:"token"`
	SecondFactor  string   `json:"second_factor"`
	Challenge     string   `json:"challenge"`
	RecoveryCodes []string `json:"recovery_codes"`
	Secret        string   `json:"secret"`
	URI           string   `json:"otpauth_uri"`
}

func (h *apiHarness) post(path, token string, body any) (int, signInReply, string) {
	h.t.Helper()
	status, raw := h.do(http.MethodPost, path, token, "", body)
	var reply signInReply
	if status == http.StatusOK {
		if err := json.Unmarshal([]byte(raw), &reply); err != nil {
			h.t.Fatalf("decode the reply to %s: %v (%s)", path, err, raw)
		}
	}
	return status, reply, raw
}

// passwordStep signs in with the password and returns the challenge for the
// step asked for, failing unless that step is want.
func (h *apiHarness) passwordStep(username, want string) string {
	h.t.Helper()
	status, reply, raw := h.post("/api/v1/login", "", map[string]string{"username": username, "password": localPassword})
	if status != http.StatusOK {
		h.t.Fatalf("sign in %s: status %d (%s)", username, status, raw)
	}
	if reply.SecondFactor != want || reply.Token != "" || reply.Challenge == "" {
		h.t.Fatalf("sign in %s answered %s; want step %q, a challenge and no session", username, raw, want)
	}
	return reply.Challenge
}

// enrol sets an account up with an authenticator app and returns its secret,
// the session and the recovery codes.
func (h *apiHarness) enrol(username string) (string, string, []string) {
	h.t.Helper()
	challenge := h.passwordStep(username, "enrol")
	status, enrolment, raw := h.post("/api/v1/login/enrol", "", map[string]string{"challenge": challenge})
	if status != http.StatusOK || enrolment.Secret == "" || !strings.HasPrefix(enrolment.URI, "otpauth://totp/") {
		h.t.Fatalf("begin enrolment: status %d (%s)", status, raw)
	}
	status, done, raw := h.post("/api/v1/login/enrol/confirm", "",
		map[string]string{"challenge": challenge, "code": codeAt(h.t, enrolment.Secret, 0)})
	if status != http.StatusOK || done.Token == "" {
		h.t.Fatalf("confirm enrolment: status %d (%s)", status, raw)
	}
	return enrolment.Secret, done.Token, done.RecoveryCodes
}

// codeAt is the app's code steps away from now.
func codeAt(t *testing.T, secret string, steps int64) string {
	t.Helper()
	code, err := totp.CodeAt(secret, totp.StepAt(time.Now())+steps)
	if err != nil {
		t.Fatalf("make a code: %v", err)
	}
	return code
}

func (h *apiHarness) admin(username string, org uuid.UUID) {
	h.t.Helper()
	account := entities.User{
		ID:            uuid.Must(uuid.NewV7()),
		Username:      username,
		Roles:         []string{entities.RoleAdmin},
		Organizations: []*entities.Organization{{ID: org}},
	}
	if err := h.svc.CreateUser(inOrganization(h.t.Context(), org), account, localPassword); err != nil {
		h.t.Fatalf("create the administrator %s: %v", username, err)
	}
}

// An account with no authenticator app gets no session from its password: it
// gets a challenge good for setting one up, and the first code from the app
// gives the session and the recovery codes.
func TestARequiredSecondFactorIsSetUpAtTheFirstSignIn(t *testing.T) {
	h := newTwoFactorHarness(t)
	org := h.organization("Acme")
	h.localAccount("hopper", "hopper@acme.example", org)

	challenge := h.passwordStep("hopper", "enrol")
	if status, body := h.get("/api/v1/users/me", challenge, ""); status != http.StatusUnauthorized {
		t.Fatalf("a challenge used as a session: status %d (%s), want 401", status, body)
	}
	if status, _, raw := h.post("/api/v1/login/code", "", map[string]string{"challenge": challenge, "code": "000000"}); status != http.StatusUnauthorized {
		t.Fatalf("an enrolment challenge used for a code: status %d (%s), want 401", status, raw)
	}

	status, enrolment, raw := h.post("/api/v1/login/enrol", "", map[string]string{"challenge": challenge})
	if status != http.StatusOK {
		t.Fatalf("begin enrolment: status %d (%s)", status, raw)
	}
	wrong := codeAt(t, enrolment.Secret, 5)
	if status, _, raw := h.post("/api/v1/login/enrol/confirm", "", map[string]string{"challenge": challenge, "code": wrong}); status != http.StatusUnauthorized {
		t.Fatalf("confirm with a wrong code: status %d (%s), want 401", status, raw)
	}
	status, done, raw := h.post("/api/v1/login/enrol/confirm", "",
		map[string]string{"challenge": challenge, "code": codeAt(t, enrolment.Secret, 0)})
	if status != http.StatusOK || done.Token == "" {
		t.Fatalf("confirm enrolment: status %d (%s)", status, raw)
	}
	if len(done.RecoveryCodes) != 10 {
		t.Fatalf("got %d recovery codes, want 10", len(done.RecoveryCodes))
	}
	if got := h.me(done.Token); got.Username != "hopper" {
		t.Fatalf("signed in as %q, want hopper", got.Username)
	}

	// Set up once: the next sign-in asks for a code, not a new app.
	again := h.passwordStep("hopper", "code")
	if status, _, raw := h.post("/api/v1/login/enrol", "", map[string]string{"challenge": again}); status != http.StatusUnauthorized {
		t.Fatalf("a code challenge used to enrol again: status %d (%s), want 401", status, raw)
	}
}

// A code is taken once: the one that confirmed the app, or signed in, does
// not sign in again, though a later one does.
func TestACodeIsTakenOnce(t *testing.T) {
	h := newTwoFactorHarness(t)
	org := h.organization("Acme")
	h.localAccount("hopper", "hopper@acme.example", org)
	secret, _, _ := h.enrol("hopper")

	challenge := h.passwordStep("hopper", "code")
	used := codeAt(t, secret, 0)
	if status, _, raw := h.post("/api/v1/login/code", "", map[string]string{"challenge": challenge, "code": used}); status != http.StatusUnauthorized {
		t.Fatalf("the code that confirmed the app, again: status %d (%s), want 401", status, raw)
	}
	next := codeAt(t, secret, 1)
	status, reply, raw := h.post("/api/v1/login/code", "", map[string]string{"challenge": challenge, "code": next})
	if status != http.StatusOK || reply.Token == "" {
		t.Fatalf("a fresh code: status %d (%s), want a session", status, raw)
	}
	if status, _, raw := h.post("/api/v1/login/code", "", map[string]string{"challenge": challenge, "code": next}); status != http.StatusUnauthorized {
		t.Fatalf("the same code twice: status %d (%s), want 401", status, raw)
	}
}

// A recovery code signs in once, whatever case and spacing it is typed in.
func TestARecoveryCodeSignsInOnce(t *testing.T) {
	h := newTwoFactorHarness(t)
	org := h.organization("Acme")
	h.localAccount("hopper", "hopper@acme.example", org)
	_, _, codes := h.enrol("hopper")

	typed := strings.ToUpper(strings.ReplaceAll(codes[3], "-", " "))
	challenge := h.passwordStep("hopper", "code")
	status, reply, raw := h.post("/api/v1/login/code", "", map[string]string{"challenge": challenge, "code": typed})
	if status != http.StatusOK || reply.Token == "" {
		t.Fatalf("sign in with a recovery code: status %d (%s)", status, raw)
	}
	if status, _, raw := h.post("/api/v1/login/code", "", map[string]string{"challenge": challenge, "code": codes[3]}); status != http.StatusUnauthorized {
		t.Fatalf("the same recovery code again: status %d (%s), want 401", status, raw)
	}

	status, body := h.get("/api/v1/users/me/two-factor", reply.Token, "")
	if status != http.StatusOK || !strings.Contains(body, `"recovery_codes_left":9`) || !strings.Contains(body, `"enrolled":true`) {
		t.Fatalf("status after one recovery code: %d (%s), want enrolled with 9 left", status, body)
	}
}

// Wrong codes are slowed per account, as wrong passwords are: six digits are
// a million guesses, and a challenge lasts five minutes.
func TestWrongCodesAreSlowed(t *testing.T) {
	h := newTwoFactorHarness(t)
	org := h.organization("Acme")
	h.localAccount("hopper", "hopper@acme.example", org)
	secret, _, _ := h.enrol("hopper")

	challenge := h.passwordStep("hopper", "code")
	wrong := codeAt(t, secret, 7)
	var raw string
	for range loginthrottle.FreeAttempts + 2 {
		_, _, raw = h.post("/api/v1/login/code", "", map[string]string{"challenge": challenge, "code": wrong})
	}
	if !strings.Contains(raw, "too many wrong codes") {
		t.Fatalf("after %d wrong codes the reply was %s; want to be made to wait", loginthrottle.FreeAttempts+2, raw)
	}
}

// A session minted from a password alone — before two-factor sign-in was
// required — stops working once it is.
func TestASessionWithoutACodeIsRefusedWhereOneIsRequired(t *testing.T) {
	h := newTwoFactorHarness(t)
	org := h.organization("Acme")
	h.localAccount("hopper", "hopper@acme.example", org)

	optional := serviceimpl.NewUserService(repositories.NewRepository(h.db), localTokenSecret)
	_, token, err := optional.Login(t.Context(), "hopper", localPassword)
	if err != nil {
		t.Fatalf("sign in where a code is optional: %v", err)
	}
	if status, body := h.get("/api/v1/users/me", token, ""); status != http.StatusUnauthorized {
		t.Fatalf("a password-only session where a code is required: status %d (%s), want 401", status, body)
	}
}

// Login, the password-only sign-in, never gives a session to an account with
// an authenticator app, whatever the policy.
func TestPasswordOnlyLoginRefusesAnAccountWithAnApp(t *testing.T) {
	h := newTwoFactorHarness(t)
	org := h.organization("Acme")
	h.localAccount("hopper", "hopper@acme.example", org)
	h.enrol("hopper")

	optional := serviceimpl.NewUserService(repositories.NewRepository(h.db), localTokenSecret)
	if _, _, err := optional.Login(t.Context(), "hopper", localPassword); err == nil ||
		!strings.Contains(err.Error(), "API key") {
		t.Fatalf("password-only sign-in of an account with an app: %v; want it refused, pointing at API keys", err)
	}
}

// An administrator removes the authenticator app of somebody who lost it:
// their sessions end and their next sign-in sets up a new one. Never their
// own, and never somebody who is not an administrator.
func TestAnAdministratorResetsSomebodysSecondFactor(t *testing.T) {
	h := newTwoFactorHarness(t)
	org := h.organization("Acme")
	hopper := h.localAccount("hopper", "hopper@acme.example", org)
	h.admin("grace", org)
	_, hopperSession, _ := h.enrol("hopper")
	_, graceSession, _ := h.enrol("grace")
	grace := h.me(graceSession)

	path := "/api/v1/users/" + hopper.ID.String() + "/two-factor"
	if status, body := h.do(http.MethodDelete, "/api/v1/users/"+grace.ID+"/two-factor", graceSession, org.String(), nil); status != http.StatusForbidden {
		t.Fatalf("an administrator removing their own app: status %d (%s), want 403", status, body)
	}
	if status, body := h.do(http.MethodDelete, "/api/v1/users/"+grace.ID+"/two-factor", hopperSession, org.String(), nil); status != http.StatusForbidden {
		t.Fatalf("a designer removing an administrator's app: status %d (%s), want 403", status, body)
	}
	if status, body := h.do(http.MethodDelete, path, graceSession, org.String(), nil); status != http.StatusOK {
		t.Fatalf("remove hopper's app: status %d (%s), want 200", status, body)
	}
	if status, body := h.get("/api/v1/users/me", hopperSession, ""); status != http.StatusUnauthorized {
		t.Fatalf("hopper's session after the reset: status %d (%s), want 401", status, body)
	}
	time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second)) + 10*time.Millisecond)
	h.passwordStep("hopper", "enrol")
}

// Recovery codes are replaced with a code from the app, never with a recovery
// code: a stolen session and one stolen code must not mint more.
func TestRecoveryCodesAreReplacedWithACodeFromTheApp(t *testing.T) {
	h := newTwoFactorHarness(t)
	org := h.organization("Acme")
	h.localAccount("hopper", "hopper@acme.example", org)
	secret, session, codes := h.enrol("hopper")

	if status, _, raw := h.post("/api/v1/users/me/two-factor/recovery-codes", session, map[string]string{"code": codes[0]}); status != http.StatusUnauthorized {
		t.Fatalf("replace the codes with a recovery code: status %d (%s), want 401", status, raw)
	}
	status, reply, raw := h.post("/api/v1/users/me/two-factor/recovery-codes", session, map[string]string{"code": codeAt(t, secret, 1)})
	if status != http.StatusOK || len(reply.RecoveryCodes) != 10 || reply.RecoveryCodes[0] == codes[0] {
		t.Fatalf("replace the codes: status %d (%s), want 10 new ones", status, raw)
	}
	challenge := h.passwordStep("hopper", "code")
	if status, _, raw := h.post("/api/v1/login/code", "", map[string]string{"challenge": challenge, "code": codes[1]}); status != http.StatusUnauthorized {
		t.Fatalf("an old recovery code after the replacement: status %d (%s), want 401", status, raw)
	}
}

// --reset-two-factor is the way back in for an installation whose only
// administrator lost their phone and their recovery codes.
func TestTheServerCommandRemovesAnAuthenticatorApp(t *testing.T) {
	h := newTwoFactorHarness(t)
	org := h.organization("Acme")
	h.admin("grace", org)
	_, session, _ := h.enrol("grace")

	if err := h.svc.RemoveSecondFactor(t.Context(), "grace"); err != nil {
		t.Fatalf("remove grace's app: %v", err)
	}
	if status, body := h.get("/api/v1/users/me", session, ""); status != http.StatusUnauthorized {
		t.Fatalf("grace's session after the removal: status %d (%s), want 401", status, body)
	}
	time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second)) + 10*time.Millisecond)
	h.passwordStep("grace", "enrol")
	if err := h.svc.RemoveSecondFactor(t.Context(), "grace"); err == nil {
		t.Fatal("removing an app that is not there reported success")
	}
}
