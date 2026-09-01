package security

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gsoultan/metis/server/repositories/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// dbIdempotencyStore keeps records in the database, so every replica sees the
// same answer to "has this already been done?".
//
// The claim is a single conditional INSERT. That matters more than it looks:
// checking for a row and then inserting one leaves a window in which two
// replicas both find nothing and both execute, which is the duplicate write
// this whole mechanism exists to prevent. The database decides, once.
type dbIdempotencyStore struct {
	db  *gorm.DB
	ttl time.Duration
	now func() time.Time
}

// NewDBIdempotencyStore returns a store shared by every replica on this
// database.
func NewDBIdempotencyStore(db *gorm.DB, ttl time.Duration) IdempotencyStore {
	if ttl <= 0 {
		ttl = defaultIdempotencyTTL
	}
	return &dbIdempotencyStore{db: db, ttl: ttl, now: time.Now}
}

// storageKeyHash reduces an unbounded scoped key to a fixed-width primary key.
// The parts — path, tenant, user, the client's chosen value — have no length
// limit, and an indexed column does.
func storageKeyHash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func (s *dbIdempotencyStore) Claim(ctx context.Context, key, requestHash string) (ClaimOutcome, error) {
	hashed := storageKeyHash(key)
	now := s.now()

	record := models.IdempotencyRecordModel{
		Key:         hashed,
		RequestHash: requestHash,
		CreatedAt:   now,
	}

	// DoNothing rather than an upsert: losing this race means somebody else
	// owns the key, and overwriting their claim would let both callers execute.
	result := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&record)

	switch {
	case result.Error == nil && result.RowsAffected > 0:
		return ClaimOutcome{Owned: true}, nil
	case result.Error != nil && !isDuplicateKey(result.Error):
		return ClaimOutcome{}, fmt.Errorf("claim idempotency key: %w", result.Error)
	}

	// Somebody holds it — either this insert conflicted, or a dialect without
	// DO NOTHING support returned a duplicate-key error. Both mean: read what
	// they recorded.
	existing, err := s.get(ctx, hashed)
	if err != nil {
		return ClaimOutcome{}, err
	}
	if existing == nil {
		// The holder finished and the row was swept between the failed insert
		// and this read. Nothing is in flight and no answer survives, so the
		// caller may execute — which is safe, since that is exactly the state a
		// first-ever request is in.
		return ClaimOutcome{Owned: true}, nil
	}

	return s.outcomeFor(ctx, existing, requestHash, now)
}

// outcomeFor turns a stored row into what the caller should do about it.
func (s *dbIdempotencyStore) outcomeFor(ctx context.Context, existing *models.IdempotencyRecordModel, requestHash string, now time.Time) (ClaimOutcome, error) {
	if existing.RequestHash != requestHash {
		return ClaimOutcome{Conflict: true}, nil
	}
	if existing.Completed {
		if now.Sub(existing.CreatedAt) > s.ttl {
			// Aged out. Take it over rather than replaying an answer older than
			// the retry window it was kept for.
			if err := s.reclaim(ctx, existing.Key, requestHash, now); err != nil {
				return ClaimOutcome{}, err
			}
			return ClaimOutcome{Owned: true}, nil
		}
		return ClaimOutcome{Response: responseFrom(existing)}, nil
	}
	return ClaimOutcome{}, nil // claimed elsewhere, still running
}

// reclaim resets an aged-out record for a new attempt.
func (s *dbIdempotencyStore) reclaim(ctx context.Context, hashedKey, requestHash string, now time.Time) error {
	err := s.db.WithContext(ctx).
		Model(&models.IdempotencyRecordModel{}).
		Where("record_key = ?", hashedKey).
		Select("request_hash", "completed", "status_code", "headers", "body", "created_at", "completed_at").
		Updates(models.IdempotencyRecordModel{
			RequestHash: requestHash,
			CreatedAt:   now,
		}).Error
	if err != nil {
		return fmt.Errorf("reclaim expired idempotency key: %w", err)
	}
	return nil
}

func (s *dbIdempotencyStore) Complete(ctx context.Context, key string, response StoredResponse) error {
	now := s.now()

	// A struct update with an explicit Select, not a map. GORM applies a
	// field's serializer only when it can match the field, and a map keyed by
	// column name bypasses that — the header map then reaches the driver raw
	// and the write fails with "unsupported type map[string][]string". Select
	// is what makes the false/zero fields land anyway, since a struct update
	// otherwise skips zero values.
	err := s.db.WithContext(ctx).
		Model(&models.IdempotencyRecordModel{}).
		Where("record_key = ? AND completed = ?", storageKeyHash(key), false).
		Select("completed", "status_code", "headers", "body", "completed_at").
		Updates(models.IdempotencyRecordModel{
			Completed:   true,
			StatusCode:  response.StatusCode,
			Headers:     response.Header,
			Body:        response.Body,
			CompletedAt: &now,
		}).Error
	if err != nil {
		return fmt.Errorf("record idempotency response: %w", err)
	}
	return nil
}

// Abandon deletes an incomplete claim, so a request that died mid-flight does
// not leave every retry waiting for an answer nobody will write.
func (s *dbIdempotencyStore) Abandon(ctx context.Context, key string) error {
	err := s.db.WithContext(ctx).
		Where("record_key = ? AND completed = ?", storageKeyHash(key), false).
		Delete(&models.IdempotencyRecordModel{}).Error
	if err != nil {
		return fmt.Errorf("abandon idempotency claim: %w", err)
	}
	return nil
}

// Await polls until the holder records a response or the budget runs out.
//
// Polling, because coordinating across processes without a broker leaves no
// channel to wait on. The budget is bounded so a holder that died does not
// strand its retries: the caller is told to retry, which the key makes safe.
func (s *dbIdempotencyStore) Await(ctx context.Context, key string) (*StoredResponse, error) {
	hashed := storageKeyHash(key)

	budget, cancel := context.WithTimeout(ctx, idempotencyWaitBudget)
	defer cancel()

	ticker := time.NewTicker(idempotencyPollInterval)
	defer ticker.Stop()

	for {
		record, err := s.get(budget, hashed)
		if err != nil {
			return nil, err
		}
		if record == nil {
			// The claim was abandoned or swept. Nothing is coming.
			return nil, nil
		}
		if record.Completed {
			return responseFrom(record), nil
		}

		select {
		case <-ticker.C:
		case <-budget.Done():
			return nil, budget.Err()
		}
	}
}

func (s *dbIdempotencyStore) get(ctx context.Context, hashedKey string) (*models.IdempotencyRecordModel, error) {
	var record models.IdempotencyRecordModel
	err := s.db.WithContext(ctx).Where("record_key = ?", hashedKey).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read idempotency record: %w", err)
	}
	return &record, nil
}

func responseFrom(record *models.IdempotencyRecordModel) *StoredResponse {
	header := make(http.Header, len(record.Headers))
	for name, values := range record.Headers {
		header[name] = values
	}
	return &StoredResponse{StatusCode: record.StatusCode, Header: header, Body: record.Body}
}

// isDuplicateKey reports whether an insert lost a race for the primary key.
//
// GORM surfaces this as ErrDuplicatedKey on the dialects that translate it, and
// the message check covers the ones that do not: SQL Server's driver reports a
// raw 2627/2601. A missed classification here turns ordinary contention into a
// 500, so the net is deliberately wide — the fallback path re-reads the row and
// behaves correctly either way.
func isDuplicateKey(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"duplicate key",        // PostgreSQL, SQL Server
		"duplicate entry",      // MySQL
		"unique constraint",    // SQLite
		"violation of primary", // SQL Server
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}
