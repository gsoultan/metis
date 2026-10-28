package app

import (
	"context"
	"fmt"
	"time"

	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/internal/pkg/settings"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/pg"
	"github.com/rs/zerolog/log"
)

// savedSettingsReload is how often a replica reads the Settings page's values
// again, for the settings that apply without a restart. A setting changed on
// one replica reaches every other within this; one small read of a table that
// changes a few times a year is what it costs.
const savedSettingsReload = 30 * time.Second

// loadSavedSettings installs what administrators set on the Settings page, so
// every reader of a setting finds it under the environment.
//
// A failure to read them stops the start. Running on defaults instead would
// quietly undo an administrator's retention period or mail server, and the
// first sign would be data kept for ever or mail never sent.
func (a *App) loadSavedSettings(ctx context.Context) error {
	if a.storm == nil {
		return nil
	}
	values, err := a.readSavedSettings(ctx)
	if err != nil {
		return err
	}
	a.bootSettings = values
	envvar.UseSaved(values)
	if len(values) > 0 {
		log.Info().Int("settings", len(values)).Msg("Read the settings saved on the Settings page; the environment wins where it sets one")
	}
	return nil
}

// readSavedSettings reads the current values and keeps those the Settings page
// may set, as they would pass its checks today.
//
// One saved by an older release and refused now — a bound tightened, a
// setting made environment-only — is named and left out, so the server reads
// it as unset rather than as something the page would no longer accept.
func (a *App) readSavedSettings(ctx context.Context) (map[string]string, error) {
	current, err := pg.NewInstallationSettingRepository(a.storm).Current(entities.WithSystemContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("could not read the settings saved on the Settings page: %w", err)
	}
	values := make(map[string]string, len(current))
	for _, setting := range current {
		if setting.Value == nil {
			continue
		}
		definition, known := settings.Find(setting.Name)
		if !known {
			log.Warn().Str("setting", setting.Name).Msg("A saved setting is no longer one the Settings page may set, and is ignored")
			continue
		}
		if err := definition.Validate(*setting.Value); err != nil {
			log.Warn().Str("setting", setting.Name).Err(err).Msg("A saved setting is no longer a value the Settings page accepts, and is ignored")
			continue
		}
		values[setting.Name] = *setting.Value
	}
	return values, nil
}

// reloadSavedSettings keeps the settings that apply without a restart in step
// with the Settings page, on every replica. The ones that apply at restart
// keep the values this process started with: the server built its workers and
// limits from them, and switching what they read mid-flight would make the
// page's "after a restart" untrue.
func (a *App) reloadSavedSettings(ctx context.Context) {
	if a.storm == nil {
		return
	}
	ticker := time.NewTicker(savedSettingsReload)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			latest, err := a.readSavedSettings(ctx)
			if err != nil {
				log.Warn().Err(err).Msg("Could not read the saved settings again; the live ones keep their last values")
				continue
			}
			envvar.UseSaved(mergeLiveSettings(a.bootSettings, latest))
		}
	}
}

// mergeLiveSettings is what a running process reads: the latest value of
// every live setting, and the value it started with of every other.
func mergeLiveSettings(boot, latest map[string]string) map[string]string {
	merged := make(map[string]string, len(boot)+len(latest))
	for _, definition := range settings.All() {
		source := boot
		if definition.Applies == settings.Live {
			source = latest
		}
		if value, ok := source[definition.Name]; ok {
			merged[definition.Name] = value
		}
	}
	return merged
}
