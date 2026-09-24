package participantsource_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/configsecret"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/pg"
	"github.com/gsoultan/metis/tests/testutils"
)

// A directory held in another database is configured with a connection string,
// and that string carries the password to the whole database. The participant
// sources page read sources through ListByProject and Get, which mask the
// configuration before it leaves the service — with a list of secret-looking
// keys that had no entry matching dsn. So the page received the password in
// clear, in the response body, the browser's memory and its developer tools.
//
// Root cause: configsecret recognised a secret by the words in its key, and
// "dsn" was not one of them.
func TestADirectoryDSNNeverReachesTheBrowser(t *testing.T) {
	const password = "hunter2-directory"
	dsn := "postgres://svc_directory:" + password + "@hr-db.internal:5432/hr?sslmode=require"

	conn := testutils.SetupTestConn(t)
	ctx, _, projectID := testutils.ScopedProject(t, repositories.NewRepository(conn))
	sources := pg.NewParticipantSourceRepository(conn)

	id, err := sources.Save(ctx, entities.ParticipantSource{
		Project: &entities.Project{ID: projectID},
		Name:    "HR directory",
		Kind:    "postgres",
		Config: map[string]any{
			"dsn":   dsn,
			"query": "SELECT login AS username FROM staff",
		},
		OnMissing: "leave",
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("save the source: %v", err)
	}

	listed, err := sources.ListByProject(ctx, projectID)
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected the one source just saved, got %d", len(listed))
	}
	got, err := sources.Get(ctx, id)
	if err != nil {
		t.Fatalf("get the source: %v", err)
	}

	for name, source := range map[string]entities.ParticipantSource{"ListByProject": listed[0], "Get": got} {
		if source.Config["dsn"] != configsecret.Sentinel {
			t.Errorf("%s returned the dsn as %v; it must come back as the sentinel", name, source.Config["dsn"])
		}
		// The query is not a secret, and it is what the person opened the page
		// to check.
		if source.Config["query"] != "SELECT login AS username FROM staff" {
			t.Errorf("%s altered the query: %v", name, source.Config["query"])
		}
		// Belt and braces: the password must not appear anywhere in what the
		// API would serialise, under any key.
		body, err := json.Marshal(source)
		if err != nil {
			t.Fatalf("marshal %s: %v", name, err)
		}
		if strings.Contains(string(body), password) {
			t.Errorf("%s would send the database password to the browser: %s", name, body)
		}
	}

	// The sync reads through GetWithSecrets and must still get the real string,
	// or masking has broken the thing the configuration is for.
	withSecrets, err := sources.GetWithSecrets(ctx, id)
	if err != nil {
		t.Fatalf("get the source with its secrets: %v", err)
	}
	if withSecrets.Config["dsn"] != dsn {
		t.Errorf("the sync would connect with %v instead of the stored dsn", withSecrets.Config["dsn"])
	}
}
