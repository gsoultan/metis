package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/nacl/box"

	"github.com/gsoultan/metis/internal/pkg/config"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
)

// A key and a secret made fresh for each run, as strong as an installation's,
// so none sits in the source to be mistaken for one.
var (
	testEncryptionKey = rand.Text() + rand.Text()
	testJWTSecret     = rand.Text() + rand.Text()
)

// fakeInstallation answers a joining machine as the Servers page would:
// words for its code, waiting once, then settings sealed to its key.
type fakeInstallation struct {
	t        *testing.T
	settings []byte
	refuse   string
	key      atomic.Value
	asked    atomic.Int32
}

func (f *fakeInstallation) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case serviceimpl.MachineJoinPath:
		var req contracts.MachineJoinRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			f.t.Errorf("request: %v", err)
		}
		if f.refuse != "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": f.refuse})
			return
		}
		if req.Format != contracts.MachineJoinFormat || req.Code != "ABCD-EFGH" || req.Name != "worker-3" {
			f.t.Errorf("the machine sent %+v", req)
		}
		f.key.Store(req.Key)
		_ = json.NewEncoder(w).Encode(contracts.MachineJoinReply{Format: contracts.MachineJoinFormat,
			Words: "apple bridge candle dune", Role: "worker", ExpiresAt: time.Now().Add(time.Hour)})
	case serviceimpl.MachineJoinCollectPath:
		if f.asked.Add(1) == 1 {
			_ = json.NewEncoder(w).Encode(contracts.MachineJoinCollection{Format: contracts.MachineJoinFormat})
			return
		}
		raw, _ := base64.StdEncoding.DecodeString(f.key.Load().(string))
		var key [32]byte
		copy(key[:], raw)
		sealed, err := box.SealAnonymous(nil, f.settings, &key, rand.Reader)
		if err != nil {
			f.t.Errorf("seal: %v", err)
		}
		_ = json.NewEncoder(w).Encode(contracts.MachineJoinCollection{Format: contracts.MachineJoinFormat,
			Accepted: true, Sealed: base64.StdEncoding.EncodeToString(sealed)})
	default:
		http.NotFound(w, r)
	}
}

func testJoinCommand(out *bytes.Buffer) *joinCommand {
	j := newJoinCommand(out)
	j.hostname = func() (string, error) { return "worker-3", nil }
	j.pollEvery = 5 * time.Millisecond
	return j
}

func TestAJoinedMachineWritesTheSettingsItIsHanded(t *testing.T) {
	t.Chdir(t.TempDir())
	database, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer database.Close()
	cfg, err := config.NewConfig(config.DriverPostgres,
		"postgres://metis:secret@"+database.Addr().String()+"/metis?sslmode=disable", testEncryptionKey, testJWTSecret)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	cfg.Server.Role = "worker"
	settings, err := cfg.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	installation := httptest.NewServer(&fakeInstallation{t: t, settings: settings})
	defer installation.Close()

	var out bytes.Buffer
	if err := testJoinCommand(&out).run(context.Background(), []string{installation.URL + "/", "ABCD-EFGH"}); err != nil {
		t.Fatalf("join: %v\n%s", err, out.String())
	}
	said := out.String()
	for _, want := range []string{"apple bridge candle dune", "background work (worker)", "reaches the database"} {
		if !strings.Contains(said, want) {
			t.Errorf("the command did not say %q:\n%s", want, said)
		}
	}
	info, err := os.Stat(config.DefaultConfigPath)
	if err != nil {
		t.Fatalf("config.yaml was not written: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config.yaml is %v; it holds the database password and must be the user's alone", info.Mode().Perm())
	}
	written, err := config.Load(config.DefaultConfigPath)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if written.Server.Role != "worker" || written.JWTSecret != testJWTSecret {
		t.Fatalf("wrote %+v", written)
	}
}

func TestJoiningAMachineThatIsSetUpIsRefused(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(config.DefaultConfigPath, []byte("database: {}\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	installation := &fakeInstallation{t: t}
	server := httptest.NewServer(installation)
	defer server.Close()
	var out bytes.Buffer
	err := testJoinCommand(&out).run(context.Background(), []string{server.URL, "ABCD-EFGH"})
	if err == nil || !strings.Contains(err.Error(), "set up") {
		t.Fatalf("want a machine with settings refused; got %v", err)
	}
	if installation.key.Load() != nil {
		t.Fatal("the code was sent although nothing would be written")
	}
}

func TestJoinSaysWhatTheInstallationRefused(t *testing.T) {
	t.Chdir(t.TempDir())
	server := httptest.NewServer(&fakeInstallation{t: t, refuse: "that code is not one this server made"})
	defer server.Close()
	var out bytes.Buffer
	err := testJoinCommand(&out).run(context.Background(), []string{"--name", "worker-3", server.URL, "ABCD-EFGH"})
	if err == nil || !strings.Contains(err.Error(), "that code is not one this server made") {
		t.Fatalf("want the refusal passed on; got %v", err)
	}
	if config.Exists(config.DefaultConfigPath) {
		t.Fatal("a refused machine wrote settings")
	}
}

func TestJoinTakesAnAddressAndACode(t *testing.T) {
	var out bytes.Buffer
	for _, args := range [][]string{{}, {"https://metis.example.com"}, {"metis.example.com", "ABCD"}, {"ftp://metis.example.com", "ABCD"}} {
		if err := testJoinCommand(&out).run(context.Background(), args); err == nil {
			t.Errorf("join %q was taken", args)
		}
	}
}

func TestANewMachineIsGivenThisInstallationsOwnSettings(t *testing.T) {
	t.Chdir(t.TempDir())
	const dsn = "postgres://metis:secret@db.internal:5432/metis?sslmode=require"
	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("ENCRYPTION_KEY", testEncryptionKey)
	t.Setenv("JWT_SECRET", testJWTSecret)
	t.Setenv(config.EnvServerPurpose, "production")
	t.Setenv(config.EnvServerName, "Production (Jakarta)")
	t.Setenv(config.EnvPublicURL, "https://metis.example.com")

	data, err := (&App{}).machineSettings("web")
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	cfg, err := config.Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got, err := cfg.DecryptConnectionString("")
	if err != nil || got != dsn {
		t.Fatalf("the database is %q (%v), want %q", got, err, dsn)
	}
	if cfg.EncryptionKey != testEncryptionKey || cfg.JWTSecret != testJWTSecret {
		t.Fatal("the new machine was not given this installation's keys")
	}
	want := config.ServerConfig{Role: "web", Purpose: "production", Name: "Production (Jakarta)", PublicURL: "https://metis.example.com"}
	if cfg.Server != want {
		t.Fatalf("server section %+v, want %+v", cfg.Server, want)
	}
}

// A machine being added has no account anywhere yet, so its two routes stay
// open under an embedding program's Identity too: the code admits it.
func TestTheJoinRoutesNeedNoAccountUnderAnIdentity(t *testing.T) {
	kept := setupPaths([]string{"/api/v1/login", serviceimpl.MachineJoinPath, serviceimpl.MachineJoinCollectPath, "/api/v1/machines"})
	want := []string{serviceimpl.MachineJoinPath, serviceimpl.MachineJoinCollectPath}
	if strings.Join(kept, ",") != strings.Join(want, ",") {
		t.Fatalf("kept %v, want %v", kept, want)
	}
}
