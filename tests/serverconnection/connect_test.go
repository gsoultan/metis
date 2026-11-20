// Package serverconnection_test connects two installations, each with its own
// database, the way an administrator does from the Environments page: a code
// made on the server releases come from, entered on the one they go to, and
// accepted there by a second administrator.
package serverconnection_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/app"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/internal/pkg/health"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/endpoints"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/tests/testutils"
)

// server is one installation: its own database, served over HTTP.
type server struct {
	svc  services.ServiceFacade
	keys interface {
		serviceimpl.ReleaseKeys
		serviceimpl.ReleasePolicy
	}
	url  string
	db   *db.Conn
	repo repositories.Repository
}

func newServer(t *testing.T, name string, purpose entities.ServerPurpose) server {
	t.Helper()
	conn := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(conn)
	sse := observersimpl.NewSSEObserver()
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), sse, "connect-test", nil, nil, nil,
		services.WithServerIdentity(serviceimpl.ServerIdentitySource{
			Name:    func() string { return name },
			Purpose: func() (entities.ServerPurpose, error) { return purpose, nil },
		}))
	handler, _ := app.BuildAPIHandler(svc, endpoints.MakeEndpoints(svc), sse, nil, map[string]health.Checker{}, conn)
	web := httptest.NewServer(handler)
	t.Cleanup(web.Close)
	return server{svc: svc, keys: serviceimpl.NewConnectionReleaseKeys(repo.ServerConnection()), url: web.URL, db: conn, repo: repo}
}

func admin(username string) context.Context {
	return context.WithValue(context.Background(), pkgauth.UserContextKey,
		entities.User{ID: uuid.New(), Username: username, Roles: []string{entities.RoleAdmin}})
}

// The servers in a test talk over plain http on loopback, which only an
// installation that allows private networks may.
func allowLoopback(t *testing.T) {
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")
	t.Setenv(serviceimpl.EnvReleaseSigningKey, "")
	t.Setenv(serviceimpl.EnvReleaseTrustedKeys, "")
}

// Production connects to development with development's code, both show the
// same words, and production trusts development's key only once a second
// administrator accepts it.
func TestProductionTrustsDevelopmentOnceASecondAdministratorAccepts(t *testing.T) {
	allowLoopback(t)
	dev := newServer(t, "Development", entities.ServerDevelopment)
	prod := newServer(t, "Production", entities.ServerProduction)

	code, err := dev.svc.MakeServerConnectionCode(admin("dana"))
	if err != nil {
		t.Fatalf("make a code: %v", err)
	}
	pending, err := prod.svc.CreateServerConnection(admin("lee"), dev.url, strings.ToLower(code.Code))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if pending.Side != entities.ConnectionUpstream || pending.Purpose != entities.ServerDevelopment || pending.Name != "Development" || pending.Accepted() {
		t.Fatalf("production holds %+v, want development waiting to be accepted", pending)
	}

	devSide, err := dev.svc.ListServerConnections(admin("dana"))
	if err != nil || len(devSide) != 1 {
		t.Fatalf("development lists %v (%v), want production", devSide, err)
	}
	if devSide[0].Side != entities.ConnectionDownstream || devSide[0].Name != "Production" || devSide[0].Words != pending.Words {
		t.Fatalf("development shows %+v and production %q: both servers show the same words", devSide[0], pending.Words)
	}

	devKey := identityKey(t, dev)
	if trusted, _ := prod.keys.TrustedKeys(context.Background()); len(trusted) != 0 {
		t.Fatalf("production trusts %v before anybody accepted development", trusted)
	}
	if _, err := prod.svc.AcceptServerConnection(admin("lee"), pending.ID); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("the administrator who connected it accepted it: %v", err)
	}
	accepted, err := prod.svc.AcceptServerConnection(admin("kim"), pending.ID)
	if err != nil || accepted.AcceptedBy != "kim" {
		t.Fatalf("a second administrator could not accept: %+v %v", accepted, err)
	}
	trusted, err := prod.keys.TrustedKeys(context.Background())
	if err != nil || len(trusted) != 1 || trusted[0] != devKey {
		t.Fatalf("production trusts %v (%v), want development's key %s", trusted, err, devKey)
	}
	if needs, _ := prod.keys.RequiresLiveNote(context.Background(), uuid.Nil); needs {
		t.Fatal("a production with no staging before it asks for a live note")
	}
}

// Writing "accepted" into the database is not enough to be trusted: the
// acceptance is sealed with the server's encryption key, and a row without
// its seal, or with another row's, is accepted in name only.
func TestARowWrittenIntoTheDatabaseIsNotTrusted(t *testing.T) {
	allowLoopback(t)
	dev := newServer(t, "Development", entities.ServerDevelopment)
	prod := newServer(t, "Production", entities.ServerProduction)
	code, err := dev.svc.MakeServerConnectionCode(admin("dana"))
	if err != nil {
		t.Fatalf("make a code: %v", err)
	}
	if _, err := prod.svc.CreateServerConnection(admin("lee"), dev.url, code.Code); err != nil {
		t.Fatalf("connect: %v", err)
	}
	ex, err := prod.db.MainExecutor(context.Background())
	if err != nil {
		t.Fatalf("executor: %v", err)
	}
	if _, err := ex.Exec(context.Background(),
		`UPDATE server_connections SET accepted_at = now(), accepted_by = 'mallory', trust = 'gcm1:Zm9yZ2Vk'`, nil); err != nil {
		t.Fatalf("write the row: %v", err)
	}
	if trusted, _ := prod.keys.TrustedKeys(context.Background()); len(trusted) != 0 {
		t.Fatalf("production trusts %v from a row nobody accepted here", trusted)
	}
}

// A code is good once: a second server that tries it is refused.
func TestACodeConnectsOneServer(t *testing.T) {
	allowLoopback(t)
	dev := newServer(t, "Development", entities.ServerDevelopment)
	code, err := dev.svc.MakeServerConnectionCode(admin("dana"))
	if err != nil {
		t.Fatalf("make a code: %v", err)
	}
	if _, err := newServer(t, "Production", entities.ServerProduction).svc.CreateServerConnection(admin("lee"), dev.url, code.Code); err != nil {
		t.Fatalf("connect: %v", err)
	}
	_, err = newServer(t, "Staging", entities.ServerStaging).svc.CreateServerConnection(admin("lee"), dev.url, code.Code)
	if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "used already") {
		t.Fatalf("a used code got %v, want a refusal", err)
	}
}

// Releases go forwards only: development takes releases from nobody.
func TestDevelopmentTakesReleasesFromNobody(t *testing.T) {
	allowLoopback(t)
	prod := newServer(t, "Production", entities.ServerProduction)
	code, err := prod.svc.MakeServerConnectionCode(admin("lee"))
	if err != nil {
		t.Fatalf("make a code: %v", err)
	}
	_, err = newServer(t, "Development", entities.ServerDevelopment).svc.CreateServerConnection(admin("dana"), prod.url, code.Code)
	if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "before it") {
		t.Fatalf("development taking production's releases got %v, want a refusal", err)
	}
}

// A production that accepted a staging before it takes a release live only
// with staging's note that it went live there.
func TestAStagingBeforeProductionRequiresItsLiveNote(t *testing.T) {
	allowLoopback(t)
	staging := newServer(t, "Staging", entities.ServerStaging)
	prod := newServer(t, "Production", entities.ServerProduction)
	code, err := staging.svc.MakeServerConnectionCode(admin("dana"))
	if err != nil {
		t.Fatalf("make a code: %v", err)
	}
	pending, err := prod.svc.CreateServerConnection(admin("lee"), staging.url, code.Code)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if needs, _ := prod.keys.RequiresLiveNote(context.Background(), uuid.Nil); needs {
		t.Fatal("a staging nobody accepted already decides what production needs")
	}
	if _, err := prod.svc.AcceptServerConnection(admin("kim"), pending.ID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if needs, err := prod.keys.RequiresLiveNote(context.Background(), uuid.Nil); !needs || err != nil {
		t.Fatalf("production with an accepted staging needs a live note: %v %v", needs, err)
	}
}

// A signing key is made the first time one is needed, kept, and the same one
// is used from then on.
func TestTheSigningKeyIsMadeOnceAndKept(t *testing.T) {
	allowLoopback(t)
	dev := newServer(t, "Development", entities.ServerDevelopment)
	first, err := dev.keys.SigningKey(context.Background())
	if err != nil {
		t.Fatalf("first key: %v", err)
	}
	again, err := dev.keys.SigningKey(context.Background())
	if err != nil || !first.Equal(again) {
		t.Fatalf("the second read gave another key (%v)", err)
	}
	if got := identityKey(t, dev); got != base64.StdEncoding.EncodeToString(first.Public().(ed25519.PublicKey)) {
		t.Fatalf("the server shows %s, which is not the key it signs with", got)
	}
}

// Set in the environment, the trusted keys are the operator's: servers are
// not connected from the pages.
func TestTrustSetInTheEnvironmentLocksThePages(t *testing.T) {
	allowLoopback(t)
	dev := newServer(t, "Development", entities.ServerDevelopment)
	code, err := dev.svc.MakeServerConnectionCode(admin("dana"))
	if err != nil {
		t.Fatalf("make a code: %v", err)
	}
	_, public := key(3)
	t.Setenv(serviceimpl.EnvReleaseTrustedKeys, public)
	_, err = newServer(t, "Production", entities.ServerProduction).svc.CreateServerConnection(admin("lee"), dev.url, code.Code)
	if !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("connecting with trust set in the environment got %v, want a refusal", err)
	}
}

func identityKey(t *testing.T, s server) string {
	t.Helper()
	id, err := s.svc.ServerConnectionIdentity(admin("dana"))
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	return id.Key
}

func key(fill byte) (seed, public string) {
	raw := make([]byte, ed25519.SeedSize)
	for i := range raw {
		raw[i] = fill
	}
	k := ed25519.NewKeyFromSeed(raw)
	return base64.StdEncoding.EncodeToString(raw), base64.StdEncoding.EncodeToString(k.Public().(ed25519.PublicKey))
}

// A code this server did not make is refused, and says nothing about which
// codes it did.
func TestAGuessedCodeIsRefused(t *testing.T) {
	allowLoopback(t)
	dev := newServer(t, "Development", entities.ServerDevelopment)
	if _, err := dev.svc.MakeServerConnectionCode(admin("dana")); err != nil {
		t.Fatalf("make a code: %v", err)
	}
	_, err := newServer(t, "Production", entities.ServerProduction).svc.CreateServerConnection(admin("lee"), dev.url, "AAAA-AAAA-AAAA-AAAA")
	if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "not one this server made") {
		t.Fatalf("a guessed code got %v, want a refusal", err)
	}
}
