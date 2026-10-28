package impl

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/internal/pkg/settings"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

const (
	// serverSettingHistoryLimit bounds one setting's changes a read returns.
	serverSettingHistoryLimit = 200
	// maxSettingReasonLength bounds the reason kept with a change.
	maxSettingReasonLength = 500
)

// installationSettingService reads and changes the Settings page's settings.
type installationSettingService struct {
	repo repocontracts.InstallationSettingRepository
}

// NewInstallationSettingService returns the service behind the Settings
// page's server settings.
func NewInstallationSettingService(repo repocontracts.InstallationSettingRepository) contracts.InstallationSettingService {
	return &installationSettingService{repo: repo}
}

// ServerSettings reports every setting with where its value comes from.
//
// A secret's value never leaves: the view says whether one is set, and by
// whom it was last saved.
func (s *installationSettingService) ServerSettings(ctx context.Context) (contracts.ServerSettings, error) {
	if _, err := settingsCaller(ctx); err != nil {
		return contracts.ServerSettings{}, err
	}
	current, err := s.repo.Current(ctx)
	if err != nil {
		return contracts.ServerSettings{}, err
	}
	saved := make(map[string]entities.InstallationSetting, len(current))
	for _, setting := range current {
		saved[setting.Name] = setting
	}
	definitions := settings.All()
	view := contracts.ServerSettings{Settings: make([]contracts.ServerSetting, 0, len(definitions))}
	for _, definition := range definitions {
		view.Settings = append(view.Settings, settingView(definition, saved[definition.Name]))
	}
	for _, fixed := range settings.EnvironmentOnly() {
		value, set := envvar.FromEnvironment(fixed.Name)
		shown := contracts.EnvironmentSetting{Fixed: fixed, IsSet: set}
		if !fixed.Secret {
			shown.Value = value
		}
		view.EnvironmentOnly = append(view.EnvironmentOnly, shown)
	}
	return view, nil
}

// ChangeServerSetting saves a change, after the same checks the server
// applies when it reads the setting.
//
// A setting the environment sets is refused rather than saved: the saved
// value would never be read, and the page would say something the server is
// not doing.
func (s *installationSettingService) ChangeServerSetting(ctx context.Context, change contracts.ServerSettingChange) (contracts.ServerSetting, error) {
	caller, err := settingsCaller(ctx)
	if err != nil {
		return contracts.ServerSetting{}, err
	}
	definition, err := changeableSetting(change.Name)
	if err != nil {
		return contracts.ServerSetting{}, err
	}
	if change.Value != nil {
		if err := definition.Validate(*change.Value); err != nil {
			return contracts.ServerSetting{}, fmt.Errorf("%w: %w", apierr.ErrInvalidArgument, err)
		}
	}
	reason, err := settingReason(change.Reason)
	if err != nil {
		return contracts.ServerSetting{}, err
	}
	recorded, err := s.repo.Record(ctx, entities.InstallationSetting{
		Name:    definition.Name,
		Value:   change.Value,
		Secret:  definition.Secret,
		Reason:  reason,
		SetBy:   caller.Username,
		SetByID: caller.ID,
	})
	if err != nil {
		return contracts.ServerSetting{}, err
	}
	// Said in the log as well as kept, because the log is where an operator
	// looks when the server starts behaving differently. Never the value of a
	// secret; not the value of anything, since the history holds it.
	log.Info().Str("setting", definition.Name).Str("by", caller.Username).Bool("cleared", change.Value == nil).
		Str("applies", string(definition.Applies)).Msg("A server setting was changed on the Settings page")
	// The view of what was just saved: a secret's value comes back empty.
	recorded.Value = change.Value
	return settingView(definition, recorded), nil
}

func (s *installationSettingService) ServerSettingHistory(ctx context.Context, name string) ([]entities.InstallationSetting, error) {
	if _, err := settingsCaller(ctx); err != nil {
		return nil, err
	}
	definition, known := settings.Find(name)
	if !known {
		return nil, fmt.Errorf("%w: %q is not a setting the Settings page keeps", apierr.ErrNotFound, name)
	}
	history, err := s.repo.History(ctx, definition.Name, serverSettingHistoryLimit)
	if err != nil {
		return nil, err
	}
	if definition.Secret {
		for i := range history {
			if history[i].Value != nil {
				history[i].Value = new(string)
			}
		}
	}
	return history, nil
}

// changeableSetting is the definition of a setting the page may change now.
func changeableSetting(name string) (settings.Definition, error) {
	definition, known := settings.Find(name)
	if !known {
		if slices.ContainsFunc(settings.EnvironmentOnly(), func(f settings.Fixed) bool { return f.Name == name }) {
			return definition, apierr.Forbiddenf("%s is set only in the server's environment", name)
		}
		return definition, fmt.Errorf("%w: %q is not a server setting", apierr.ErrNotFound, name)
	}
	if _, set := envvar.FromEnvironment(name); set {
		return definition, apierr.Forbiddenf("%s is set in the server's environment, which wins over the Settings page; change it there", name)
	}
	return definition, nil
}

// settingsCaller is the account changing or reading the server's settings.
//
// The route admits only a platform administrator; this is the second lock,
// so that a route wired without the gate refuses rather than opens. A change
// is recorded against an account, so a caller without one cannot make one.
func settingsCaller(ctx context.Context) (*entities.User, error) {
	caller := signedIn(ctx)
	if caller == nil || caller.Username == "" || !slices.Contains(caller.Roles, entities.RoleAdmin) {
		return nil, apierr.Forbiddenf("only an administrator of every organization can read or change the server's settings")
	}
	if caller.ID == uuid.Nil {
		return nil, apierr.Forbiddenf("a settings change records the account that made it, and this request carries none")
	}
	return caller, nil
}

// settingReason is the reason as it is kept: trimmed, and refused when there
// is none or it is too long.
func settingReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	switch {
	case reason == "":
		return "", apierr.Invalidf("say why: a server setting is changed with a reason, which is kept with it")
	case utf8.RuneCountInString(reason) > maxSettingReasonLength:
		return "", apierr.Invalidf("the reason is longer than %d characters", maxSettingReasonLength)
	}
	return reason, nil
}
