package impl

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/fcm"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// pushSender sends pushes to People's devices.
//
// Nothing it sends is stored as a notification: a push is a nudge towards
// work already in the person's inbox, and the notifications table keeps
// everything forever (finding 17). What a send did goes to the log.
type pushSender struct {
	connections connectionLister
	devices     repocontracts.PersonDeviceRepository
	pool        *fcm.Pool
	now         func() time.Time
}

// NewPushSender returns a sender reading the push connection from
// connections and the devices from devices. A nil pool uses Google's
// endpoints.
func NewPushSender(connections connectionLister, devices repocontracts.PersonDeviceRepository, pool *fcm.Pool) *pushSender {
	if pool == nil {
		pool = fcm.NewPool()
	}
	return &pushSender{connections: connections, devices: devices, pool: pool, now: time.Now}
}

func (s *pushSender) PushConfigured(ctx context.Context, projectID uuid.UUID) (bool, error) {
	_, found, err := pushConnectionKey(ctx, s.connections, projectID)
	return found, err
}

func (s *pushSender) PushToPerson(ctx context.Context, projectID, participantID uuid.UUID, content entities.PushContent) (entities.PushResult, error) {
	key, found, err := pushConnectionKey(ctx, s.connections, projectID)
	if err != nil || !found {
		return entities.PushResult{}, err
	}
	client, err := s.pool.Client(key)
	if err != nil {
		return entities.PushResult{}, fmt.Errorf("the project's push connection: %w", err)
	}
	devices, err := s.projectDevices(ctx, projectID, participantID)
	if err != nil {
		return entities.PushResult{}, err
	}
	result := entities.PushResult{Devices: len(devices)}
	var transient error
	for _, device := range devices {
		outcome, err := s.sendOne(ctx, client, device, content)
		if errors.Is(err, fcm.ErrCredentials) {
			return result, err
		}
		transient = cmpErr(transient, err)
		result.Delivered += outcome.Delivered
		result.Forgotten += outcome.Forgotten
	}
	if result.Delivered == 0 && transient != nil {
		return result, transient
	}
	return result, nil
}

// projectDevices is the person's devices registered in this project. A
// person is one project's, so this is all of them; the check is so a push
// never crosses projects whatever the rows say.
func (s *pushSender) projectDevices(ctx context.Context, projectID, participantID uuid.UUID) ([]entities.PersonDevice, error) {
	devices, err := s.devices.ListByParticipant(ctx, participantID)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(devices, func(device entities.PersonDevice) bool { return device.ProjectID != projectID }), nil
}

// sendOne pushes to one device and records what came of it. A device that is
// gone is forgotten; a malformed message is logged and not retried.
func (s *pushSender) sendOne(ctx context.Context, client *fcm.Client, device entities.PersonDevice, content entities.PushContent) (entities.PushResult, error) {
	_, err := client.Send(ctx, fcm.Message{
		Token: device.Token, Title: content.Title, Body: content.Body, Link: content.Link, Data: content.Data,
	})
	switch {
	case err == nil:
		if markErr := s.devices.MarkSent(ctx, device.ID, s.now()); markErr != nil {
			log.Warn().Err(markErr).Str("device_id", device.ID.String()).Msg("push: delivered but could not record it")
		}
		return entities.PushResult{Delivered: 1}, nil
	case errors.Is(err, fcm.ErrUnregistered):
		if forgetErr := s.devices.Forget(ctx, device.ID); forgetErr != nil {
			return entities.PushResult{}, forgetErr
		}
		log.Info().Str("device_id", device.ID.String()).Msg("push: Firebase says the device is gone; it was removed")
		return entities.PushResult{Forgotten: 1}, nil
	case errors.Is(err, fcm.ErrRejected):
		log.Warn().Err(err).Str("device_id", device.ID.String()).Msg("push: Firebase refused the message; not retried")
		return entities.PushResult{}, nil
	default:
		return entities.PushResult{}, err
	}
}

// cmpErr keeps the first error seen.
func cmpErr(first, next error) error {
	if first != nil {
		return first
	}
	return next
}
