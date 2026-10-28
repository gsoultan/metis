package impl

import (
	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/internal/pkg/settings"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

// settingView says what one setting is, as this replica reads it.
func settingView(definition settings.Definition, saved entities.InstallationSetting) contracts.ServerSetting {
	view := contracts.ServerSetting{Definition: definition, Source: contracts.SourceDefault}
	if saved.Name != "" {
		created := saved.CreatedAt
		view.UpdatedAt, view.UpdatedBy = &created, saved.SetBy
	}
	if value, set := envvar.FromEnvironment(definition.Name); set {
		view.Source, view.IsSet = contracts.SourceEnvironment, true
		view.Value = shownValue(definition, value)
		return view
	}
	running, runs := envvar.Lookup(definition.Name)
	if saved.Value != nil {
		view.Source, view.IsSet = contracts.SourceSaved, true
		view.Value = shownValue(definition, *saved.Value)
		view.PendingRestart = definition.Applies == settings.AtRestart && (!runs || running != *saved.Value)
		return view
	}
	// Cleared on the page, and still run with until a restart.
	view.PendingRestart = definition.Applies == settings.AtRestart && runs
	return view
}

func shownValue(definition settings.Definition, value string) string {
	if definition.Secret {
		return ""
	}
	return value
}
