package impl

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// The batch operation worker: works through what was asked for, a few
// instances at a time.
//
// It takes the oldest running operation no worker holds, for a lease, acts on
// up to Chunk of its instances, records each result as it goes, and lets the
// operation go. Every replica runs one; two never take the same operation at
// once, and a result is recorded only for an instance still pending, so an
// instance is never counted twice. A worker that dies mid-chunk loses at most
// the result of the instance it was on: the lease runs out, the instance is
// still pending, and the next worker acts on it again — a retry with nothing
// left to resolve, or a cancel of an instance that has ended, is skipped.

// BatchOperationSettings is how the worker paces itself.
type BatchOperationSettings struct {
	// PollInterval is how often it looks for an operation to work on.
	PollInterval time.Duration
	// Lease is how long an operation is held by a worker that took it before
	// another may take it.
	Lease time.Duration
	// Chunk is how many instances one turn acts on before the operation is
	// let go, so that operations take turns and the standing of whoever asked
	// is read again.
	Chunk int
	// OperationsPerTick bounds how many operations one tick works on.
	OperationsPerTick int
}

// DefaultBatchOperationSettings is a turn of 50 instances every two seconds
// for each of up to ten operations, held for two minutes.
func DefaultBatchOperationSettings() BatchOperationSettings {
	return BatchOperationSettings{
		PollInterval:      2 * time.Second,
		Lease:             2 * time.Minute,
		Chunk:             50,
		OperationsPerTick: 10,
	}
}

func (s *batchOperationService) StartBatchOperations(ctx context.Context) {
	// Every tenant's operations, as the job worker does every tenant's jobs.
	// Each instance is then acted on as the account that asked (actingAs).
	ctx = entities.WithSystemContext(ctx)
	log.Info().
		Dur("poll", s.settings.PollInterval).
		Int("chunk", s.settings.Chunk).
		Msg("Batch operation worker started")

	go func() {
		ticker := time.NewTicker(s.settings.PollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// A panic here would end the ticker, and with it every operation.
				if err := runRecovered("batch operation poll", func() error {
					s.drain(ctx)
					return nil
				}); err != nil {
					// Logged with its stack by runRecovered; the next tick polls again.
					continue
				}
			}
		}
	}()
}

// drain gives a turn to each running operation, up to OperationsPerTick.
func (s *batchOperationService) drain(ctx context.Context) {
	for range s.settings.OperationsPerTick {
		if ctx.Err() != nil {
			return
		}
		operation, found, err := s.repo.BatchOperation().ClaimRunning(ctx, s.now(), s.settings.Lease)
		if err != nil {
			log.Error().Err(err).Msg("A batch operation could not be claimed")
			return
		}
		if !found {
			return
		}
		if err := s.turn(ctx, operation); err != nil {
			if ctx.Err() != nil {
				return
			}
			// Not lost: the operation is still running, and is taken again
			// when its lease runs out.
			log.Error().Err(err).Str("batch_operation", operation.ID.String()).
				Msg("A turn of a batch operation could not be finished; it will be taken again")
		}
	}
}

// turn acts on up to Chunk of an operation's pending instances, then lets it
// go — completed, if none is left.
func (s *batchOperationService) turn(ctx context.Context, operation entities.BatchOperation) error {
	acting, refusal, err := s.actingAs(ctx, operation)
	if err != nil {
		return err
	}
	if refusal != "" {
		_, err := s.repo.BatchOperation().Stop(ctx, operation.ID, "metis", refusal, s.now())
		return err
	}
	pending, err := s.repo.BatchOperation().PendingInstances(ctx, operation.ID, s.settings.Chunk)
	if err != nil {
		return err
	}
	for _, instance := range pending {
		if err := ctx.Err(); err != nil {
			// Shutting down: what is left stays pending, and the lease lets
			// another worker take it.
			return err
		}
		item := s.act(acting, operation, instance)
		settledAt := s.now()
		item.SettledAt = &settledAt
		if _, err := s.repo.BatchOperation().Settle(ctx, operation.ID, item); err != nil {
			return err
		}
	}
	return s.repo.BatchOperation().Finish(ctx, operation.ID, s.now())
}

// actingAs is the context each instance is acted on in: the account that
// asked, as it stands now, in the organization the operation is for — with
// the system's reach taken away. refusal says why the operation must stop
// instead: the account is gone, or may no longer make the act.
//
// Read at every turn, not once. Somebody whose role is taken away while their
// operation is running stops acting within a turn, as they would stop being
// able to make the request.
func (s *batchOperationService) actingAs(ctx context.Context, operation entities.BatchOperation) (context.Context, string, error) {
	if s.accounts == nil {
		return nil, "this server cannot act for the account that asked", nil
	}
	account, err := s.accounts(ctx, operation.RequestedByID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, fmt.Sprintf("the account that asked, %s, is no longer there", operation.RequestedBy), nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("could not read the account that asked for the operation: %w", err)
	}
	may := operates(account, operation.OrganizationID)
	if operation.Kind == entities.BatchCancel {
		may = administers(account, operation.OrganizationID)
	}
	if !may {
		return nil, fmt.Sprintf("%s may no longer %s instances of this organization", operation.RequestedBy, operation.Kind), nil
	}
	acting := entities.WithoutSystemContext(ctx)
	acting = context.WithValue(acting, pkgauth.UserContextKey, account)
	acting = entities.WithTenantContext(acting, entities.TenantContext{TenantID: operation.OrganizationID.String()})
	return acting, "", nil
}

// act makes the operation's act on one instance and says what came of it.
func (s *batchOperationService) act(ctx context.Context, operation entities.BatchOperation, instance uuid.UUID) entities.BatchItem {
	var item entities.BatchItem
	err := runRecovered("batch operation item", func() error {
		switch operation.Kind {
		case entities.BatchRetry:
			item = s.retry(ctx, instance)
		case entities.BatchCancel:
			item = s.cancel(ctx, operation, instance)
		default:
			item = failedItem(instance, fmt.Errorf("an operation of kind %q cannot be carried out", operation.Kind))
		}
		return nil
	})
	if err != nil {
		item = failedItem(instance, err)
	}
	item.InstanceID = instance
	item.Detail = cutDetail(item.Detail)
	return item
}

// retry resolves every open incident of the instance, as resolving them one
// at a time does: a failed job runs again, an external task is offered again.
func (s *batchOperationService) retry(ctx context.Context, instance uuid.UUID) entities.BatchItem {
	incidents, err := s.incidents.ListIncidents(ctx, instance)
	if err != nil {
		return failedItem(instance, err)
	}
	resolved := 0
	for _, incident := range incidents {
		if incident.Status != entities.IncidentOpen {
			continue
		}
		if err := s.incidents.ResolveIncident(ctx, incident.ID); err != nil {
			return failedItem(instance, err)
		}
		resolved++
	}
	if resolved == 0 {
		return entities.BatchItem{Status: entities.BatchItemSkipped, Detail: "it had no open incident to retry"}
	}
	return entities.BatchItem{Status: entities.BatchItemSucceeded, Detail: fmt.Sprintf("retried %d incident(s)", resolved)}
}

// cancel cancels the instance where it stands, through the same preview and
// apply an administrator makes for one instance: what the preview refuses is
// the item's failure, and the apply is the plan just previewed.
func (s *batchOperationService) cancel(ctx context.Context, operation entities.BatchOperation, instance uuid.UUID) entities.BatchItem {
	row, err := s.repo.Process().Get(ctx, instance)
	if err != nil {
		return failedItem(instance, err)
	}
	switch row.Status {
	case models.ProcessCompleted, models.ProcessCancelled:
		return entities.BatchItem{Status: entities.BatchItemSkipped, Detail: fmt.Sprintf("it had already ended (%s)", row.Status)}
	}
	command := entities.DeviationCommand{
		InstanceID: instance,
		Kind:       entities.DeviationCancel,
		NodeID:     whereItStands(row),
		Reason:     operation.Reason,
		DryRun:     true,
	}
	preview, err := s.deviator.DeviateInstance(ctx, command)
	if err != nil {
		return failedItem(instance, err)
	}
	if !preview.Plan.Applicable() {
		return entities.BatchItem{Status: entities.BatchItemFailed, Detail: strings.Join(preview.Plan.Refusals, " ")}
	}
	command.DryRun = false
	command.VisitKey = preview.Plan.VisitKey
	outcome, err := s.deviator.DeviateInstance(ctx, command)
	if err != nil {
		return failedItem(instance, err)
	}
	if waiting := outcome.PendingApproval; waiting != nil && !outcome.Applied {
		// Not made, and not refused: a second administrator decides it, as
		// they would for the same cancel asked for alone.
		return entities.BatchItem{Status: entities.BatchItemSkipped,
			Detail: fmt.Sprintf("the cancel waits for a second administrator, request %s", waiting.RequestID)}
	}
	if !outcome.Applied {
		return entities.BatchItem{Status: entities.BatchItemFailed, Detail: "the cancel was not applied"}
	}
	return entities.BatchItem{Status: entities.BatchItemSucceeded, Detail: "cancelled"}
}

// whereItStands is a step the instance holds a token on, which a cancel names
// — any one of them, since a cancel withdraws the work on every step — or none
// for an instance that waits at no step.
func whereItStands(instance models.ProcessInstanceModel) string {
	for _, token := range instance.Tokens {
		if token.NodeID != "" {
			return token.NodeID
		}
	}
	return ""
}

// failedItem is a failure in words somebody can act on. What the caller sent
// or may not do is said as the single act would say it; anything else is the
// server's, logged with the instance and said as such.
func failedItem(instance uuid.UUID, err error) entities.BatchItem {
	for _, kind := range []error{apierr.ErrInvalidArgument, apierr.ErrNotFound, apierr.ErrForbidden} {
		if errors.Is(err, kind) {
			detail := strings.TrimPrefix(err.Error(), kind.Error()+": ")
			return entities.BatchItem{Status: entities.BatchItemFailed, Detail: detail}
		}
	}
	log.Error().Err(err).Str("instance", instance.String()).Msg("A batch operation could not act on an instance")
	return entities.BatchItem{Status: entities.BatchItemFailed, Detail: "the act failed inside Metis; the server's log has why"}
}

// cutDetail keeps a detail to what its column holds.
func cutDetail(detail string) string {
	if utf8.RuneCountInString(detail) <= entities.MaxBatchItemDetailLength {
		return detail
	}
	runes := []rune(detail)
	return string(runes[:entities.MaxBatchItemDetailLength-1]) + "…"
}
