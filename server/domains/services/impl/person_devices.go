package impl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

const (
	// maxPersonDevices bounds the devices one person holds. A phone, a
	// tablet, a few browsers: twenty is past any real count, and the bound is
	// what keeps one person's app re-registering in a loop from growing the
	// table without end.
	maxPersonDevices = 20
	// maxDeviceTokenLength matches the column.
	maxDeviceTokenLength = 4096
	maxDeviceLabel       = 100
)

var devicePlatforms = []string{entities.DevicePlatformAndroid, entities.DevicePlatformIOS, entities.DevicePlatformWeb}

// personDeviceService registers People's devices and lets administrators
// manage them.
type personDeviceService struct {
	devices repocontracts.PersonDeviceRepository
	now     func() time.Time
}

// NewPersonDeviceService returns the device service over devices.
func NewPersonDeviceService(devices repocontracts.PersonDeviceRepository) *personDeviceService {
	return &personDeviceService{devices: devices, now: time.Now}
}

func hashDeviceToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// RegisterPersonDevice records the device for the signed-in person. An API key
// may register one: that is how a person's own app does it after they sign in
// there.
func (s *personDeviceService) RegisterPersonDevice(ctx context.Context, person entities.Person, device entities.PersonDevice) (entities.PersonDevice, error) {
	device, err := cleanDevice(device)
	if err != nil {
		return entities.PersonDevice{}, err
	}
	held, err := s.devices.ListByParticipant(ctx, person.ID)
	if err != nil {
		return entities.PersonDevice{}, err
	}
	known := slices.ContainsFunc(held, func(d entities.PersonDevice) bool { return d.Token == device.Token })
	if !known && len(held) >= maxPersonDevices {
		return entities.PersonDevice{}, apierr.Invalidf("you have %d devices registered, the most one person may; remove one first", maxPersonDevices)
	}
	device.ID = uuid.Must(uuid.NewV7())
	device.ProjectID = person.ProjectID
	device.ParticipantID = person.ID
	device.CreatedAt = s.now().UTC()
	return s.devices.Register(ctx, device, hashDeviceToken(device.Token))
}

// cleanDevice checks what an app sent and trims what it may.
func cleanDevice(device entities.PersonDevice) (entities.PersonDevice, error) {
	device.Token = strings.TrimSpace(device.Token)
	if device.Token == "" || len(device.Token) > maxDeviceTokenLength || strings.IndexFunc(device.Token, unicode.IsSpace) >= 0 {
		return entities.PersonDevice{}, apierr.Invalidf("a device needs its push token: up to %d characters, no spaces", maxDeviceTokenLength)
	}
	device.Platform = strings.ToLower(strings.TrimSpace(device.Platform))
	if !slices.Contains(devicePlatforms, device.Platform) {
		return entities.PersonDevice{}, apierr.Invalidf("platform must be one of %s", strings.Join(devicePlatforms, ", "))
	}
	device.Label = strings.TrimSpace(device.Label)
	if utf8.RuneCountInString(device.Label) > maxDeviceLabel {
		return entities.PersonDevice{}, apierr.Invalidf("a device's name is at most %d characters", maxDeviceLabel)
	}
	return device, nil
}

func (s *personDeviceService) ListPersonDevices(ctx context.Context, person entities.Person) ([]entities.PersonDevice, error) {
	return s.devices.ListByParticipant(ctx, person.ID)
}

func (s *personDeviceService) RemovePersonDevice(ctx context.Context, person entities.Person, id uuid.UUID) error {
	return s.devices.Remove(ctx, person.ID, id)
}

func (s *personDeviceService) ListParticipantDevices(ctx context.Context, projectID, participantID uuid.UUID) ([]entities.PersonDevice, error) {
	if err := requireProjectAndParticipant(projectID, participantID); err != nil {
		return nil, err
	}
	return s.devices.ListInProject(ctx, projectID, participantID)
}

func (s *personDeviceService) CountParticipantDevices(ctx context.Context, projectID uuid.UUID) (map[uuid.UUID]int, error) {
	if projectID == uuid.Nil {
		return nil, apierr.Invalidf("a project is required")
	}
	return s.devices.CountByProject(ctx, projectID)
}

func (s *personDeviceService) RemoveParticipantDevice(ctx context.Context, projectID, participantID, id uuid.UUID) error {
	if err := requireProjectAndParticipant(projectID, participantID); err != nil {
		return err
	}
	return s.devices.RemoveInProject(ctx, projectID, participantID, id)
}

// requireProjectAndParticipant refuses a missing id. The tenant check lets a
// nil project through, so an administrator's read must never reach it with one.
func requireProjectAndParticipant(projectID, participantID uuid.UUID) error {
	if projectID == uuid.Nil {
		return apierr.Invalidf("a project is required")
	}
	if participantID == uuid.Nil {
		return apierr.Invalidf("a participant is required")
	}
	return nil
}
