package machine_test

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/nacl/box"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// settingsFor stands in for the server's config.yaml: what it was asked for,
// so the test can see the role reached it.
func settingsFor(role string) ([]byte, error) {
	return []byte("server:\n  role: " + role + "\n"), nil
}

func newMachineKey(t *testing.T) (public, private *[32]byte, encoded string) {
	t.Helper()
	public, private, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	return public, private, base64.StdEncoding.EncodeToString(public[:])
}

func TestAMachineUsesItsCodeIsAcceptedAndCollectsItsSettingsOnce(t *testing.T) {
	svc := serviceimpl.NewMachineService(repositories.NewRepository(testutils.SetupTestDB(t)).Machine(), settingsFor)
	admin := as("ana", entities.RoleAdmin)

	issued, err := svc.MakeMachineJoin(admin, "worker")
	if err != nil {
		t.Fatalf("make a code: %v", err)
	}
	if issued.Code == "" || issued.Role != "worker" {
		t.Fatalf("want a code for a worker, got %+v", issued)
	}

	// Before the machine uses it, there is nothing to accept, and nothing to
	// collect.
	if _, err := svc.AcceptMachineJoin(admin, issued.ID); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("a code nobody used was accepted: %v", err)
	}
	if _, err := svc.CollectMachineJoin(t.Context(), issued.Code); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("a code nobody used could be collected with: %v", err)
	}

	public, private, key := newMachineKey(t)
	reply, err := svc.RequestMachineJoin(t.Context(), contracts.MachineJoinRequest{
		Format: contracts.MachineJoinFormat, Code: issued.Code, Name: "worker-3", Key: key,
	})
	if err != nil {
		t.Fatalf("use the code: %v", err)
	}
	if reply.Words != entities.MachineJoinWords(issued.Code, key) || reply.Role != "worker" {
		t.Fatalf("the machine was told %+v", reply)
	}
	// Typed with spaces and in small letters, it is the same code; used, it
	// is good for nobody else.
	_, _, other := newMachineKey(t)
	if _, err := svc.RequestMachineJoin(t.Context(), contracts.MachineJoinRequest{
		Format: contracts.MachineJoinFormat, Code: issued.Code, Name: "intruder", Key: other,
	}); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("a used code was taken again: %v", err)
	}

	waiting, err := svc.CollectMachineJoin(t.Context(), issued.Code)
	if err != nil || waiting.Accepted || waiting.Sealed != "" {
		t.Fatalf("before Accept, want waiting with nothing sealed; got %+v, %v", waiting, err)
	}

	joins, err := svc.ListMachineJoins(admin)
	if err != nil || len(joins) != 1 {
		t.Fatalf("want the one machine waiting; got %+v, %v", joins, err)
	}
	if joins[0].Name != "worker-3" || joins[0].Words != reply.Words || joins[0].RequestedAt == nil {
		t.Fatalf("the Servers page does not show the machine as it asked: %+v", joins[0])
	}

	accepted, err := svc.AcceptMachineJoin(admin, issued.ID)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if accepted.AcceptedBy != "ana" || accepted.Sealed != "" {
		t.Fatalf("accepted as %+v", accepted)
	}
	if _, err := svc.AcceptMachineJoin(admin, issued.ID); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("a machine was accepted twice: %v", err)
	}

	collected, err := svc.CollectMachineJoin(t.Context(), issued.Code)
	if err != nil || !collected.Accepted {
		t.Fatalf("collect: %+v, %v", collected, err)
	}
	raw, err := base64.StdEncoding.DecodeString(collected.Sealed)
	if err != nil {
		t.Fatalf("sealed is not base64: %v", err)
	}
	settings, ok := box.OpenAnonymous(nil, raw, public, private)
	if !ok || string(settings) != "server:\n  role: worker\n" {
		t.Fatalf("the machine's key does not open its settings: %q, %v", settings, ok)
	}
	// Another key does not open them.
	otherPublic, otherPrivate, _ := newMachineKey(t)
	if _, ok := box.OpenAnonymous(nil, raw, otherPublic, otherPrivate); ok {
		t.Fatal("the settings opened with a key that is not the machine's")
	}

	// Handed out once: collected, they are gone, and the page lists the join
	// no more.
	if _, err := svc.CollectMachineJoin(t.Context(), issued.Code); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("the settings were handed out twice: %v", err)
	}
	if joins, _ := svc.ListMachineJoins(admin); len(joins) != 0 {
		t.Fatalf("a collected join is still listed: %+v", joins)
	}
}

func TestAMachineIsRefusedAndItsCodeTakenBack(t *testing.T) {
	svc := serviceimpl.NewMachineService(repositories.NewRepository(testutils.SetupTestDB(t)).Machine(), settingsFor)
	admin := as("ana", entities.RoleAdmin)
	issued, err := svc.MakeMachineJoin(admin, "")
	if err != nil || issued.Role != "all" {
		t.Fatalf("a code with no role runs everything; got %+v, %v", issued, err)
	}
	_, _, key := newMachineKey(t)
	if _, err := svc.RequestMachineJoin(t.Context(), contracts.MachineJoinRequest{
		Format: contracts.MachineJoinFormat, Code: issued.Code, Name: "web-9", Key: key,
	}); err != nil {
		t.Fatalf("use the code: %v", err)
	}
	if err := svc.RemoveMachineJoin(admin, issued.ID); err != nil {
		t.Fatalf("refuse: %v", err)
	}
	if _, err := svc.CollectMachineJoin(t.Context(), issued.Code); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("a refused machine could still ask: %v", err)
	}
	if err := svc.RemoveMachineJoin(admin, issued.ID); !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("removing it again: %v", err)
	}
}

func TestOnlyAnAdministratorOfEveryOrganizationAddsMachines(t *testing.T) {
	svc := serviceimpl.NewMachineService(repositories.NewRepository(testutils.SetupTestDB(t)).Machine(), settingsFor)
	operator := as("bo", entities.RoleOperator)
	if _, err := svc.MakeMachineJoin(operator, "web"); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("an operator made a code: %v", err)
	}
	if _, err := svc.ListMachineJoins(operator); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("an operator listed the joins: %v", err)
	}
	if _, err := svc.AcceptMachineJoin(operator, uuid.New()); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("an operator accepted a machine: %v", err)
	}
	if err := svc.RemoveMachineJoin(operator, uuid.New()); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("an operator refused a machine: %v", err)
	}
	if _, err := svc.MakeMachineJoin(as("ana", entities.RoleAdmin), "everything"); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("a role that does not exist was taken: %v", err)
	}
}

func TestARequestThatIsNotAMachinesIsRefused(t *testing.T) {
	svc := serviceimpl.NewMachineService(repositories.NewRepository(testutils.SetupTestDB(t)).Machine(), settingsFor)
	issued, err := svc.MakeMachineJoin(as("ana", entities.RoleAdmin), "web")
	if err != nil {
		t.Fatalf("make a code: %v", err)
	}
	_, _, key := newMachineKey(t)
	for name, req := range map[string]contracts.MachineJoinRequest{
		"another format": {Format: "metis.machine-join/v0", Code: issued.Code, Name: "a", Key: key},
		"no name":        {Format: contracts.MachineJoinFormat, Code: issued.Code, Name: " ", Key: key},
		"two lines":      {Format: contracts.MachineJoinFormat, Code: issued.Code, Name: "a\nb", Key: key},
		"a short key":    {Format: contracts.MachineJoinFormat, Code: issued.Code, Name: "a", Key: "c2hvcnQ="},
		"a wrong code":   {Format: contracts.MachineJoinFormat, Code: "AAAA-BBBB-CCCC-DDDD", Name: "a", Key: key},
	} {
		if _, err := svc.RequestMachineJoin(t.Context(), req); !errors.Is(err, apierr.ErrInvalidArgument) {
			t.Errorf("%s: want refused, got %v", name, err)
		}
	}
	// None of those used the code up.
	if _, err := svc.RequestMachineJoin(t.Context(), contracts.MachineJoinRequest{
		Format: contracts.MachineJoinFormat, Code: issued.Code, Name: "web-1", Key: key,
	}); err != nil {
		t.Fatalf("the code was used up by a refused request: %v", err)
	}
}

func TestACodeIsGoodForLessTimeThanItsJoin(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t)).Machine()
	now := time.Now().UTC()
	_, err := repo.CreateJoin(t.Context(), entities.MachineJoin{
		CodeHash: "c0de", Role: "web", CreatedBy: "ana", ExpiresAt: now.Add(30 * time.Minute),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// The join has half an hour left, but its code was to be used by the
	// first quarter hour of the hour: it is too late for it.
	if _, err := repo.RequestJoin(t.Context(), "c0de", now.Add(45*time.Minute), now, "m", "k", "w"); !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("a code past its time was taken: %v", err)
	}
	if _, err := repo.RequestJoin(t.Context(), "c0de", now.Add(15*time.Minute), now, "m", "k", "w"); err != nil {
		t.Fatalf("a code in its time was refused: %v", err)
	}
	if n, err := repo.ForgetEndedJoins(t.Context(), now.Add(time.Hour)); err != nil || n != 1 {
		t.Fatalf("want the ended join removed; got %d, %v", n, err)
	}
}
