package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/config"
	"github.com/gsoultan/metis/internal/pkg/crypto"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// A rotation: the new key becomes ENCRYPTION_KEY and the old one
// ENCRYPTION_KEY_PREVIOUS, `metis --reseal` moves every sealed value, and
// `--reseal-check` says when the old key can go. This drives the pass over a
// real database: a sealed column written by the repository that owns it, and
// a table holding a sealed value in every form one is stored in.
func TestResealMovesEverySealedValueOntoTheNewKey(t *testing.T) {
	const (
		oldKey   = "the-key-that-leaked-9f2c7a41e0b3d865"
		newKey   = "the-key-that-replaces-it-4be81d07c2f9a653"
		thirdKey = "a-key-nobody-configured-2e5d90c1a7f8b346"
	)
	gormDB := testutils.SetupTestDB(t)
	conn := testutils.StormConn(gormDB)
	repo := repositories.NewRepository(conn)
	ctx := entities.WithSystemContext(t.Context())
	t.Cleanup(crypto.ResetForTest)
	configure := func(current string, previous ...string) {
		t.Helper()
		crypto.ResetForTest()
		if err := crypto.Configure(current); err != nil {
			t.Fatalf("configure: %v", err)
		}
		if len(previous) > 0 {
			if err := crypto.ConfigurePrevious(previous...); err != nil {
				t.Fatalf("configure the previous key: %v", err)
			}
		}
	}
	exec := func(stmt string, args ...any) {
		t.Helper()
		if err := gormDB.WithContext(ctx).Exec(stmt, args...).Error; err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	seal := func(plain string) string {
		t.Helper()
		sealed, err := crypto.Encrypt(plain)
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		return sealed
	}

	configure(oldKey)
	jobID := uuid.New()
	if _, err := repo.Job().Create(ctx, models.JobModel{
		Base: models.Base{ID: models.UUID(jobID)}, Type: "timer", Status: models.JobPending,
		NextRunAt: time.Now().Add(time.Hour), Payload: map[string]any{"amount": 40000},
	}); err != nil {
		t.Fatalf("create a job: %v", err)
	}
	exec(`CREATE TABLE reseal_probe (id serial PRIMARY KEY, t text, v varchar(300), j jsonb, js json, b bytea, plain text)`)
	exec(`INSERT INTO reseal_probe (t, v, j, js, b, plain)
	      VALUES (?, ?, to_jsonb(?::text), to_json(?::text), convert_to(?, 'UTF8'), 'gcm1:a step somebody named this way')`,
		seal("text"), seal("varchar"), seal("jsonb"), seal("json"), seal("bytea"))
	configure(thirdKey)
	exec(`INSERT INTO reseal_probe (t) VALUES (?)`, seal("sealed by somebody else"))

	// The rotation.
	configure(newKey, oldKey)
	pool := conn.Main()
	sum := func(check bool) resealCount {
		t.Helper()
		counts, err := resealDatabase(ctx, pool, check)
		if err != nil {
			t.Fatalf("reseal (check=%v): %v", check, err)
		}
		var total resealCount
		for _, count := range counts {
			total.add(count)
		}
		return total
	}

	// Six values under the old key: the job's payload and five in the probe.
	// The value under a third key, and the text that only looks sealed, are
	// reported and left alone.
	if got := sum(true); got.Moved != 6 || got.Unreadable != 2 {
		t.Fatalf("the check found %+v, want 6 to move and 2 unreadable", got)
	}
	if got := sum(true); got.Moved != 6 {
		t.Fatalf("checking changed something: a second check found %+v", got)
	}
	if got := sum(false); got.Moved != 6 {
		t.Fatalf("the reseal moved %+v, want 6", got)
	}

	// The old key goes.
	configure(newKey)
	if got := sum(true); got.Moved != 0 || got.Unreadable != 2 {
		t.Fatalf("after the reseal, without the old key, the check found %+v", got)
	}
	job, err := repo.Job().Get(ctx, jobID)
	if err != nil || job.Payload["amount"] != float64(40000) {
		t.Fatalf("the job's payload does not read under the new key alone: %v, %v", job.Payload, err)
	}
	var text, bytea string
	if err := gormDB.WithContext(ctx).Raw(
		`SELECT t, convert_from(b, 'UTF8') FROM reseal_probe WHERE b IS NOT NULL`).Row().Scan(&text, &bytea); err != nil {
		t.Fatalf("read the probe: %v", err)
	}
	for want, sealed := range map[string]string{"text": text, "bytea": bytea} {
		if plain, err := crypto.Decrypt(sealed); err != nil || plain != want {
			t.Errorf("%s: %q, %v", want, plain, err)
		}
	}
}

// config.yaml's connection string is sealed too, and the server cannot reach
// its database without reading it, so it is moved with the rest.
func TestResealMovesTheConfigConnectionString(t *testing.T) {
	const (
		oldKey = "the-key-that-leaked-9f2c7a41e0b3d865"
		newKey = "the-key-that-replaces-it-4be81d07c2f9a653"
	)
	t.Cleanup(crypto.ResetForTest)
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg, err := config.NewConfig("postgres", "postgres://metis@db/metis", oldKey, "a-jwt-secret-for-this-test-only")
	if err != nil {
		t.Fatalf("new config: %v", err)
	}
	cfg.EncryptionKey = newKey // the operator has rotated the key in the file
	if err := cfg.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := crypto.Configure(newKey); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if err := crypto.ConfigurePrevious(oldKey); err != nil {
		t.Fatalf("configure the previous key: %v", err)
	}

	if count, err := resealConfigFile(path, true); err != nil || count.Moved != 1 {
		t.Fatalf("check: %+v, %v", count, err)
	}
	if count, err := resealConfigFile(path, false); err != nil || count.Moved != 1 {
		t.Fatalf("reseal: %+v, %v", count, err)
	}

	crypto.ResetForTest()
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	dsn, err := reloaded.DecryptConnectionString(newKey)
	if err != nil || dsn != "postgres://metis@db/metis" {
		t.Fatalf("the connection string does not open under the new key alone: %q, %v", dsn, err)
	}
}
