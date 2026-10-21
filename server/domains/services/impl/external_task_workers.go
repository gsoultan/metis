package impl

import (
	"context"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/rs/zerolog/log"
)

// checkWorkerVersion refuses a version the workers' list could not show as
// the worker wrote it.
func checkWorkerVersion(version string) error {
	if utf8.RuneCountInString(version) > entities.MaxWorkerVersionLength {
		return apierr.Invalidf("worker_version is at most %d characters", entities.MaxWorkerVersionLength)
	}
	for _, r := range version {
		if unicode.IsControl(r) {
			return apierr.Invalidf("worker_version cannot hold a control character, such as a line break or a tab")
		}
	}
	return nil
}

// recordWorker notes that a worker fetched a topic.
//
// A failure to write it is logged and the fetch answered all the same: the
// row says who is serving a topic, and a worker refused its tasks because
// that could not be written would make the topic unserved in fact.
func (s *externalTaskService) recordWorker(ctx context.Context, topic, workerID string) {
	if err := s.repo.ExternalTaskWorker().Seen(ctx, topic, workerID, entities.WorkerVersionFrom(ctx), time.Now()); err != nil {
		log.Warn().Err(err).Str("topic", topic).Str("worker_id", workerID).Msg("Could not record the worker that fetched external tasks")
	}
}

// ExternalTaskTopics lists the caller's organization's topics with the
// workers serving each.
func (s *externalTaskService) ExternalTaskTopics(ctx context.Context) ([]entities.ExternalTaskTopic, error) {
	return s.repo.ExternalTaskWorker().Topics(ctx, time.Now())
}
