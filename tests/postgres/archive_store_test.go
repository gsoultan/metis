package postgres_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/archivestore"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/archivedinstance"
	"github.com/gsoultan/metis/tests/testutils"
)

// With a store chosen, an archived instance's record leaves its row for the
// store after the archive commits, still opens from the archive, is sealed in
// the store, and goes from the store when the archive's period removes it.
func TestAnArchiveRecordMovesToItsStoreAndOpensFromThere(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	ctx, _, project := testutils.ScopedProject(t, repo)
	system := entities.WithSystemContext(t.Context())
	ex := testutils.Executor(db)
	dir := t.TempDir()
	store, err := archivestore.OpenDirectory(dir)
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	archivestore.Configure(store)
	t.Cleanup(archivestore.ResetForTest)

	definition := seedDefinition(t, db, project, 1)
	id := uuid.Must(uuid.NewV7())
	if _, err := repo.Process().Create(ctx, models.ProcessInstanceModel{
		Base: models.Base{ID: models.FromUUID(id)}, ProjectID: models.FromUUID(project),
		DefinitionID: models.FromUUID(definition), Status: models.ProcessCompleted,
		BusinessKey: "order-77", Variables: models.EncryptedMap{"secret": "kept sealed"},
	}); err != nil {
		t.Fatalf("seed the instance: %v", err)
	}
	ageInstance(ctx, t, db, id, 100*24*time.Hour)
	if n, err := repo.Process().ArchiveFinished(system, time.Now(), 90*24*time.Hour); err != nil || n != 1 {
		t.Fatalf("archive: %d, %v; want 1", n, err)
	}
	if n, err := repo.Archive().MoveRecordsOut(system); err != nil || n != 1 {
		t.Fatalf("move: %d, %v; want 1", n, err)
	}
	if n, err := repo.Archive().MoveRecordsOut(system); err != nil || n != 0 {
		t.Fatalf("a second move moved %d (%v); want none", n, err)
	}
	row, found, err := archivedinstance.New().IDEq(id).One(t.Context(), ex)
	if err != nil || !found || row.RecordStore != archivestore.KindDirectory {
		t.Fatalf("the archived row: found %v, store %q, %v; want it pointing at the directory", found, row.RecordStore, err)
	}

	_, record, err := repo.Archive().Get(ctx, id)
	if err != nil {
		t.Fatalf("open the moved record: %v", err)
	}
	if record.Instance.BusinessKey != "order-77" || record.Instance.Variables["secret"] != "kept sealed" {
		t.Errorf("the moved record reads as %+v", record.Instance)
	}
	files := recordFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("the store holds %d records, want 1", len(files))
	}
	for _, file := range files {
		body, err := os.ReadFile(file) // #nosec G304 -- a file the test's own temporary store wrote
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "order-77") || strings.Contains(string(body), "kept sealed") {
			t.Error("the record in the store is readable without its key")
		}
	}

	if n, err := repo.Archive().ForgetArchivedBefore(system, time.Now().Add(time.Hour)); err != nil || n != 1 {
		t.Fatalf("forget: %d, %v; want 1", n, err)
	}
	if left := recordFiles(t, dir); len(left) != 0 {
		t.Errorf("the store still holds %v after the archive removed its row", left)
	}
}

func recordFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Ext(path) == ".record" {
			files = append(files, path)
		}
		return err
	})
	if err != nil {
		t.Fatalf("walk the store: %v", err)
	}
	return files
}
