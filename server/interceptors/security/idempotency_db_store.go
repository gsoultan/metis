package security

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gsoultan/metis/internal/pkg/crypto"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/idempotencyrecord"
	"github.com/gsoultan/storm/runtime"
)

// dbIdempotencyStore keeps records in the database, so every replica sees the
// same answer to "has this already been done?".
//
// The claim is a single conditional INSERT. That matters more than it looks:
// checking for a row and then inserting one leaves a window in which two
// replicas both find nothing and both execute, which is the duplicate write
// this whole mechanism exists to prevent. The database decides, once.
type dbIdempotencyStore struct {
	conn *db.Conn
	ttl  time.Duration
	now  func() time.Time
}

// NewDBIdempotencyStore returns the store that survives a restart.
//
// In the database rather than in memory, because the guarantee an
// Idempotency-Key makes is about the operation, not about the process that
// happened to receive it: a replica restarting between the claim and the
// response must not let the same request run twice.
func NewDBIdempotencyStore(conn *db.Conn, ttl time.Duration) IdempotencyStore {
	return &dbIdempotencyStore{conn: conn, ttl: ttl, now: func() time.Time { return time.Now().UTC() }}
}

// storageKeyHash is what is stored instead of the key itself.
//
// The key is chosen by the caller and can carry anything — an order number, an
// email address. Hashing it means the table cannot become a list of whatever
// clients happened to name their requests.
func storageKeyHash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// Claim tries to become the one caller that runs this request.
//
// A conditional insert, and the condition is the whole mechanism: two replicas
// handed the same key both try, the primary key lets one through, and the loser
// is told somebody else owns it. Reading first and inserting second leaves a
// window where both read "absent" and both run.
func (s *dbIdempotencyStore) Claim(ctx context.Context, key, requestHash string) (ClaimOutcome, error) {
	hashed := storageKeyHash(key)
	now := s.now()

	ex, err := s.conn.Executor(ctx)
	if err != nil {
		return ClaimOutcome{}, err
	}
	ins := idempotencyrecord.Create()
	ins.SetRecordKey(hashed)
	ins.SetRequestHash(requestHash)
	ins.SetCreatedAt(now)
	ins.SetCompleted(false)
	// Set explicitly, not left to a default. A claim has no answer yet, but the
	// column is NOT NULL — and the insert reads every column back through
	// RETURNING, so a column this does not assign comes back NULL and the
	// decoder panics on a zero-length integer rather than reporting anything a
	// reader could act on.
	ins.SetStatusCode(0)
	ins.DoNothing()
	if _, err := ins.Insert(ctx, ex); err == nil {
		return ClaimOutcome{Owned: true}, nil
	} else if !errors.Is(err, runtime.ErrConflict) && !errors.Is(err, runtime.ErrUniqueViolation) {
		return ClaimOutcome{}, fmt.Errorf("claim idempotency key: %w", err)
	}

	existing, err := s.get(ctx, hashed)
	if err != nil {
		return ClaimOutcome{}, err
	}
	if existing == nil {
		// The row vanished between the refused insert and this read — a sweep
		// removed it. Owning it is the right answer: nobody else is running.
		return ClaimOutcome{Owned: true}, nil
	}
	return s.outcomeFor(ctx, *existing, requestHash, now)
}

// outcomeFor decides what an existing record means for this caller.
func (s *dbIdempotencyStore) outcomeFor(ctx context.Context, existing idempotencyrecord.Row, requestHash string, now time.Time) (ClaimOutcome, error) {
	if existing.RequestHash != requestHash {
		// The same key with a different body. Refused rather than replayed:
		// returning the first request's answer to a second, different request
		// is worse than refusing either.
		return ClaimOutcome{Conflict: true}, nil
	}
	if existing.Completed {
		if now.Sub(existing.CreatedAt) > s.ttl {
			if err := s.reclaim(ctx, existing, requestHash, now); err != nil {
				return ClaimOutcome{}, err
			}
			return ClaimOutcome{Owned: true}, nil
		}
		response, err := responseFrom(existing)
		if err != nil {
			return ClaimOutcome{}, err
		}
		return ClaimOutcome{Response: response}, nil
	}
	// Claimed elsewhere and still running. The caller waits.
	return ClaimOutcome{}, nil
}

// reclaim takes over a record whose answer has expired.
func (s *dbIdempotencyStore) reclaim(ctx context.Context, existing idempotencyrecord.Row, requestHash string, now time.Time) error {
	ex, err := s.conn.Executor(ctx)
	if err != nil {
		return err
	}
	mut := idempotencyrecord.Mutate(existing)
	mut.SetRequestHash(requestHash)
	mut.SetCompleted(false)
	mut.SetStatusCode(0)
	mut.SetHeaders(nil)
	mut.SetBody(nil)
	mut.SetCreatedAt(now)
	mut.SetCompletedAtNull()
	if err := mut.Update(ctx, ex); err != nil {
		return fmt.Errorf("reclaim expired idempotency key: %w", err)
	}
	return nil
}

// Complete records the answer, so a retry replays it rather than running again.
func (s *dbIdempotencyStore) Complete(ctx context.Context, key string, response StoredResponse) error {
	ex, err := s.conn.Executor(ctx)
	if err != nil {
		return err
	}
	headers, err := json.Marshal(response.Header)
	if err != nil {
		return fmt.Errorf("encode idempotency response headers: %w", err)
	}
	// Sealed: the answer to a command is often the thing the command created —
	// an instance, a task — with its variables in it.
	body, err := sealBody(response.Body)
	if err != nil {
		return fmt.Errorf("seal idempotency response: %w", err)
	}
	now := s.now()
	// Conditional on still being incomplete, so a late writer cannot overwrite
	// an answer somebody has already been given.
	if _, err := ex.Exec(ctx,
		`UPDATE idempotency_records
		    SET completed = true, status_code = $1, headers = $2, body = $3, completed_at = $4
		  WHERE record_key = $5 AND completed = false`,
		[]any{int64(response.StatusCode), headers, body, now, storageKeyHash(key)}); err != nil {
		return fmt.Errorf("record idempotency response: %w", err)
	}
	return nil
}

// Abandon releases a claim whose work did not finish, so the next caller can
// run it rather than waiting for an answer that will never come.
func (s *dbIdempotencyStore) Abandon(ctx context.Context, key string) error {
	ex, err := s.conn.Executor(ctx)
	if err != nil {
		return err
	}
	if _, err := ex.Exec(ctx,
		"DELETE FROM idempotency_records WHERE record_key = $1 AND completed = false",
		[]any{storageKeyHash(key)}); err != nil {
		return fmt.Errorf("abandon idempotency claim: %w", err)
	}
	return nil
}

// Await waits for whoever owns the claim to finish.
//
// Polling rather than a notification, and bounded: a caller that waits forever
// on a replica that died holds a connection until something times out, which is
// how one stuck request becomes an outage.
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
			return nil, nil
		}
		if record.Completed {
			return responseFrom(*record)
		}

		select {
		case <-ticker.C:
		case <-budget.Done():
			return nil, budget.Err()
		}
	}
}

func (s *dbIdempotencyStore) get(ctx context.Context, hashedKey string) (*idempotencyrecord.Row, error) {
	ex, err := s.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	row, found, err := idempotencyrecord.New().
		Where(idempotencyrecord.RecordKey.Eq(hashedKey)).
		One(ctx, ex)
	if err != nil {
		return nil, fmt.Errorf("read idempotency record: %w", err)
	}
	if !found {
		return nil, nil
	}
	return &row, nil
}

func responseFrom(record idempotencyrecord.Row) (*StoredResponse, error) {
	header := http.Header{}
	if len(record.Headers) > 0 {
		if err := json.Unmarshal(record.Headers, &header); err != nil {
			return nil, fmt.Errorf("decode idempotency response headers: %w", err)
		}
	}
	body, err := openBody(record.Body)
	if err != nil {
		return nil, fmt.Errorf("open idempotency response: %w", err)
	}
	return &StoredResponse{StatusCode: int(record.StatusCode), Header: header, Body: body}, nil
}

// sealBody encrypts a stored answer. An empty one stays empty.
func sealBody(body []byte) ([]byte, error) {
	if len(body) == 0 {
		return body, nil
	}
	sealed, err := crypto.Encrypt(string(body))
	if err != nil {
		return nil, err
	}
	return []byte(sealed), nil
}

// openBody reverses sealBody, passing an answer stored before sealing existed
// through as it was.
func openBody(stored []byte) ([]byte, error) {
	if !crypto.IsCiphertext(string(stored)) {
		return stored, nil
	}
	plain, err := crypto.Decrypt(string(stored))
	if err != nil {
		return nil, err
	}
	return []byte(plain), nil
}
