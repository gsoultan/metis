package impl

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/nacl/box"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

// MachineJoinPath is where a new machine uses its code, and
// MachineJoinCollectPath where it waits to be accepted and collects its
// settings. Both are public: the code admits the call.
const (
	MachineJoinPath        = "/api/v1/machines/join"
	MachineJoinCollectPath = "/api/v1/machines/join/collect"
)

const (
	// machineJoinLifetime is how long a join lasts from its code being made,
	// whatever step it reached: time to install Metis on the new machine,
	// run the command, accept the machine and for it to collect its
	// settings.
	machineJoinLifetime = time.Hour
	// machineJoinCodeLifetime is how long the code itself is good for: the
	// whole hour, as the Servers page says. A code used late leaves less
	// time to accept the machine, and the command says when it ran out.
	machineJoinCodeLifetime = machineJoinLifetime
	// machineJoinsKeptFor is how long a join is kept after it ended, so the
	// Servers page does not lose one the moment it ends; then it is removed.
	machineJoinsKeptFor = 24 * time.Hour
	// maxSealedSettings is the column's size.
	maxSealedSettings = 16384
)

// MachineSettings is this installation's config.yaml for a new machine that
// runs role: its database, its keys, and what this server is. Nil when the
// process cannot hand them out, and then no machine is accepted.
type MachineSettings func(role string) ([]byte, error)

// errNoMachineSettings refuses an accept in a process that cannot read its
// own settings.
var errNoMachineSettings = apierr.Invalidf(
	"this server cannot hand out its settings, so a machine cannot be added from here; set the new machine up with the same database, ENCRYPTION_KEY and JWT_SECRET instead")

func validMachineRole(role string) bool {
	switch role {
	case "all", "web", "worker":
		return true
	}
	return false
}

func (s *machineService) MakeMachineJoin(ctx context.Context, role string) (contracts.IssuedMachineJoin, error) {
	if err := machineAdmin(ctx); err != nil {
		return contracts.IssuedMachineJoin{}, err
	}
	role = strings.TrimSpace(role)
	if role == "" {
		role = "all"
	}
	if !validMachineRole(role) {
		return contracts.IssuedMachineJoin{}, apierr.Invalidf("a machine runs pages and API (web), background work (worker) or both (all), not %q", role)
	}
	raw := make([]byte, connectionCodeBytes)
	if _, err := rand.Read(raw); err != nil {
		return contracts.IssuedMachineJoin{}, fmt.Errorf("could not make a code: %w", err)
	}
	code := groupCode(codeEncoding.EncodeToString(raw))
	expires := s.now().UTC().Add(machineJoinLifetime)
	join, err := s.repo.CreateJoin(ctx, entities.MachineJoin{
		CodeHash: codeHash(code), Role: role, CreatedBy: signedIn(ctx).Username, ExpiresAt: expires,
	})
	if err != nil {
		return contracts.IssuedMachineJoin{}, err
	}
	return contracts.IssuedMachineJoin{ID: join.ID, Code: code, Role: role,
		ExpiresAt: expires.Add(machineJoinCodeLifetime - machineJoinLifetime)}, nil
}

func (s *machineService) ListMachineJoins(ctx context.Context) ([]entities.MachineJoin, error) {
	if err := machineAdmin(ctx); err != nil {
		return nil, err
	}
	return s.repo.Joins(ctx, s.now())
}

func (s *machineService) AcceptMachineJoin(ctx context.Context, id uuid.UUID) (entities.MachineJoin, error) {
	if err := machineAdmin(ctx); err != nil {
		return entities.MachineJoin{}, err
	}
	if s.settings == nil {
		return entities.MachineJoin{}, errNoMachineSettings
	}
	join, err := s.waitingJoin(ctx, id)
	if err != nil {
		return entities.MachineJoin{}, err
	}
	key, err := machineKey(join.Key)
	if err != nil {
		return entities.MachineJoin{}, err
	}
	settings, err := s.settings(join.Role)
	if err != nil {
		return entities.MachineJoin{}, fmt.Errorf("could not read this server's settings for the new machine: %w", err)
	}
	sealedBytes, err := box.SealAnonymous(nil, settings, key, rand.Reader)
	if err != nil {
		return entities.MachineJoin{}, fmt.Errorf("could not seal the settings to the machine's key: %w", err)
	}
	sealed := base64.StdEncoding.EncodeToString(sealedBytes)
	if len(sealed) > maxSealedSettings {
		return entities.MachineJoin{}, fmt.Errorf("this server's settings are %d bytes sealed, more than the %d a join holds", len(sealed), maxSealedSettings)
	}
	caller := signedIn(ctx).Username
	if err := s.repo.AcceptJoin(ctx, id, caller, sealed, s.now()); err != nil {
		if errors.Is(err, apierr.ErrNotFound) {
			return entities.MachineJoin{}, apierr.Invalidf("that machine is not waiting to be accepted any more; it was accepted already, or the join ended")
		}
		return entities.MachineJoin{}, err
	}
	// Who handed the database out, and to which machine: the one record of
	// it beyond the machine appearing on the Servers page.
	log.Info().Str("actor", caller).Str("machine", join.Name).Str("role", join.Role).
		Str("words", join.Words).Msg("A machine was accepted, and this installation's settings were sealed to its key")
	now := s.now().UTC()
	join.AcceptedBy, join.AcceptedAt, join.Sealed = caller, &now, ""
	return join, nil
}

// waitingJoin is the join id when a machine used its code and nobody has
// accepted it yet.
func (s *machineService) waitingJoin(ctx context.Context, id uuid.UUID) (entities.MachineJoin, error) {
	joins, err := s.repo.Joins(ctx, s.now())
	if err != nil {
		return entities.MachineJoin{}, err
	}
	for _, join := range joins {
		if join.ID != id {
			continue
		}
		switch {
		case join.RequestedAt == nil:
			return entities.MachineJoin{}, apierr.Invalidf("no machine has used that code yet; run the command on the new machine first")
		case join.AcceptedAt != nil:
			return entities.MachineJoin{}, apierr.Invalidf("that machine was accepted already")
		}
		return join, nil
	}
	return entities.MachineJoin{}, fmt.Errorf("%w: there is no machine being added with that id; the join may have ended", apierr.ErrNotFound)
}

func (s *machineService) RemoveMachineJoin(ctx context.Context, id uuid.UUID) error {
	if err := machineAdmin(ctx); err != nil {
		return err
	}
	err := s.repo.RemoveJoin(ctx, id)
	if errors.Is(err, apierr.ErrNotFound) {
		return fmt.Errorf("%w: there is no machine being added with that id", apierr.ErrNotFound)
	}
	return err
}

// errUnknownJoinCode is every refusal of a code, alike: until the code is
// good, the caller learns nothing about this server.
var errUnknownJoinCode = apierr.Invalidf("that code is not one this server made, was used already, or has expired; make a new one on the Servers page")

func (s *machineService) RequestMachineJoin(ctx context.Context, req contracts.MachineJoinRequest) (contracts.MachineJoinReply, error) {
	if req.Format != contracts.MachineJoinFormat {
		return contracts.MachineJoinReply{}, apierr.Invalidf("this server joins machines with %s, and was sent %q; run the same release of Metis on both", contracts.MachineJoinFormat, req.Format)
	}
	code := normalCode(req.Code)
	if code == "" {
		return contracts.MachineJoinReply{}, apierr.Invalidf("enter the code the Servers page showed")
	}
	name, err := machineName(req.Name)
	if err != nil {
		return contracts.MachineJoinReply{}, err
	}
	if _, err := machineKey(req.Key); err != nil {
		return contracts.MachineJoinReply{}, err
	}
	words := entities.MachineJoinWords(code, req.Key)
	now := s.now()
	join, err := s.repo.RequestJoin(ctx, codeHash(code),
		now.Add(machineJoinLifetime-machineJoinCodeLifetime), now, name, req.Key, words)
	if errors.Is(err, apierr.ErrNotFound) {
		return contracts.MachineJoinReply{}, errUnknownJoinCode
	}
	if err != nil {
		return contracts.MachineJoinReply{}, err
	}
	return contracts.MachineJoinReply{Format: contracts.MachineJoinFormat, Words: words, Role: join.Role, ExpiresAt: join.ExpiresAt}, nil
}

func (s *machineService) CollectMachineJoin(ctx context.Context, code string) (contracts.MachineJoinCollection, error) {
	code = normalCode(code)
	if code == "" {
		return contracts.MachineJoinCollection{}, errUnknownJoinCode
	}
	join, err := s.repo.JoinByCode(ctx, codeHash(code))
	if errors.Is(err, apierr.ErrNotFound) {
		return contracts.MachineJoinCollection{}, errUnknownJoinCode
	}
	if err != nil {
		return contracts.MachineJoinCollection{}, err
	}
	if join.RequestedAt == nil || join.CollectedAt != nil || !join.ExpiresAt.After(s.now()) {
		return contracts.MachineJoinCollection{}, errUnknownJoinCode
	}
	if join.AcceptedAt == nil {
		return contracts.MachineJoinCollection{Format: contracts.MachineJoinFormat}, nil
	}
	if err := s.repo.CollectJoin(ctx, join.ID, s.now()); err != nil {
		if errors.Is(err, apierr.ErrNotFound) {
			return contracts.MachineJoinCollection{}, errUnknownJoinCode
		}
		return contracts.MachineJoinCollection{}, err
	}
	return contracts.MachineJoinCollection{Format: contracts.MachineJoinFormat, Accepted: true, Sealed: join.Sealed}, nil
}

// machineName is a host name as the Servers page shows it: one line, and
// the column's size at most.
func machineName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", apierr.Invalidf("the machine sent no name")
	case len(name) > 255:
		return "", apierr.Invalidf("the machine's name is longer than 255 characters")
	case strings.IndexFunc(name, unicode.IsControl) >= 0:
		return "", apierr.Invalidf("the machine's name is more than one line")
	}
	return name, nil
}

// machineKey reads a machine's X25519 public key.
func machineKey(encoded string) (*[32]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) != 32 {
		return nil, apierr.Invalidf("the machine's key is not a 32-byte X25519 key in base64")
	}
	var key [32]byte
	copy(key[:], raw)
	return &key, nil
}
