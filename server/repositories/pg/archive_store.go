package pg

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/crypto"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/archivestore"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/archivedinstance"
	"github.com/gsoultan/storm/runtime"
)

// moveBatch is how many records one pass of the move reads at a time.
const moveBatch = 100

// recordKeyBytes is the size of the key each record kept outside the
// database is sealed with: AES-256.
const recordKeyBytes = 32

// externalRecord is what a row holds, sealed with ENCRYPTION_KEY, for a
// record kept outside the database: where it is, and the key it was sealed
// with. Rotating ENCRYPTION_KEY reseals this and leaves the record alone.
type externalRecord struct {
	Name string `json:"name"`
	Key  []byte `json:"key"`
}

// MoveRecordsOut moves the records still kept in their rows to the store
// chosen for them, oldest first, and returns how many moved. With no store
// chosen it does nothing.
//
// Not in the transaction that archived them: a record is written to its store
// after that commits, so no call to a bucket or a file system is made while
// the archive holds its locks. A row whose record is written and whose update
// then fails keeps its record and is moved again; the record written is left
// behind, sealed with a key nobody kept.
func (r *archiveRepository) MoveRecordsOut(ctx context.Context) (int64, error) {
	if !entities.IsSystemContext(ctx) {
		return 0, fmt.Errorf("%w: moving archive records spans every tenant", apierr.ErrForbidden)
	}
	store := archivestore.Writer()
	if store == nil {
		return 0, nil
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	inRows := archivedinstance.New().
		Where(archivedinstance.RecordStore.Eq(recordInDatabase)).
		Order(archivedinstance.CreatedAt.Asc(), archivedinstance.ID.Asc()).
		Limit(moveBatch)
	var moved int64
	for {
		if err := ctx.Err(); err != nil {
			return moved, err
		}
		rows, err := inRows.All(ctx, ex, nil)
		if err != nil {
			return moved, fmt.Errorf("could not read the archive records to move: %w", err)
		}
		for _, row := range rows {
			done, err := moveRecord(ctx, ex, store, row)
			if err != nil {
				return moved, err
			}
			if done {
				moved++
			}
		}
		if len(rows) < moveBatch {
			return moved, nil
		}
	}
}

// moveRecord writes one row's record to store and points the row at it. It
// reports false when another replica moved the row first, and then removes
// the copy it wrote.
func moveRecord(ctx context.Context, ex runtime.Executor, store archivestore.Store, row archivedinstance.Row) (bool, error) {
	compressed, err := openedText(row.Record)
	if err != nil {
		return false, fmt.Errorf("could not open the archive record of instance %x: %w", row.ID, err)
	}
	key := make([]byte, recordKeyBytes)
	if _, err := rand.Read(key); err != nil {
		return false, err
	}
	sealed, err := crypto.EncryptWithKey(compressed, key)
	if err != nil {
		return false, fmt.Errorf("could not seal the archive record of instance %x: %w", row.ID, err)
	}
	// A name of its own for each attempt: two replicas moving one row at once
	// each write their own copy, and the one whose update loses removes it.
	located := externalRecord{Name: recordName(row.ProjectID, row.ID), Key: key}
	if err := store.Put(ctx, located.Name, []byte(sealed)); err != nil {
		return false, err
	}
	locator, err := sealLocator(located)
	if err != nil {
		return false, err
	}
	change := archivedinstance.MutateKey(row.ID)
	change.SetRecordStore(store.Kind())
	change.SetRecord(locator)
	updated, err := archivedinstance.New().
		Where(archivedinstance.ID.Eq(row.ID), archivedinstance.RecordStore.Eq(recordInDatabase)).
		UpdateAll(ctx, ex, change)
	if err != nil {
		return false, fmt.Errorf("could not point instance %x's archive row at its record: %w", row.ID, err)
	}
	if updated == 0 {
		return false, store.Delete(ctx, located.Name)
	}
	return true, nil
}

// recordName is a record's name in its store: under its project, so a
// project's records can be found together, and unique to the attempt.
func recordName(project, instance [16]byte) string {
	return uuid.UUID(project).String() + "/" + uuid.UUID(instance).String() + "-" + uuid.NewString()
}

func sealLocator(located externalRecord) (string, error) {
	encoded, err := json.Marshal(located)
	if err != nil {
		return "", err
	}
	return sealedText(string(encoded))
}

func openLocator(sealed string) (externalRecord, error) {
	encoded, err := openedText(sealed)
	if err != nil {
		return externalRecord{}, err
	}
	var located externalRecord
	if err := json.Unmarshal([]byte(encoded), &located); err != nil {
		return externalRecord{}, fmt.Errorf("decode: %w", err)
	}
	if located.Name == "" || len(located.Key) != recordKeyBytes {
		return externalRecord{}, errors.New("the row does not say where its record is")
	}
	return located, nil
}

// openStoredRecord opens a row's record, wherever it is kept.
func openStoredRecord(ctx context.Context, kind, sealed string) (models.ArchiveRecord, error) {
	if kind == recordInDatabase {
		return openRecord(sealed)
	}
	store, ok := archivestore.Reader(kind)
	if !ok {
		return models.ArchiveRecord{}, fmt.Errorf("the record is kept in the %s store, which this server is not configured to read", kind)
	}
	located, err := openLocator(sealed)
	if err != nil {
		return models.ArchiveRecord{}, err
	}
	blob, err := store.Get(ctx, located.Name)
	if err != nil {
		return models.ArchiveRecord{}, err
	}
	compressed, err := crypto.DecryptWithKey(string(blob), located.Key)
	if err != nil {
		return models.ArchiveRecord{}, fmt.Errorf("open: %w", err)
	}
	return inflateRecord(compressed)
}

// forgetStoredRecords removes from their stores the records of rows that
// have just been deleted. A record whose store this server cannot reach is
// left where it is, and said to be: the row that named it is already gone.
func forgetStoredRecords(ctx context.Context, rows []archivedinstance.Row) error {
	var errs []error
	for _, row := range rows {
		if row.RecordStore == recordInDatabase {
			continue
		}
		store, ok := archivestore.Reader(row.RecordStore)
		if !ok {
			errs = append(errs, fmt.Errorf("instance %x's record is in the %s store, which this server is not configured to reach", row.ID, row.RecordStore))
			continue
		}
		located, err := openLocator(row.Record)
		if err != nil {
			errs = append(errs, fmt.Errorf("instance %x's record: %w", row.ID, err))
			continue
		}
		if err := store.Delete(ctx, located.Name); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
