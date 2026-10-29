package serversetting

import (
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

// ChangeRequest saves a value for a setting, or with a null value returns it
// to the environment's value or the default.
type ChangeRequest struct {
	// Name is taken from the address, never the body.
	Name   string  `json:"-"`
	Value  *string `json:"value"`
	Reason string  `json:"reason"`
}

// HistoryRequest names the setting whose changes are asked for.
type HistoryRequest struct {
	Name string `json:"-"`
}

// ListResponse is every setting the server reads.
type ListResponse struct {
	Settings        []SettingView     `json:"settings"`
	EnvironmentOnly []EnvironmentView `json:"environment_only"`
	Err             error             `json:"err,omitzero"`
}

func (r ListResponse) Failed() error { return r.Err }

// ChangeResponse is the setting as it is after the change.
type ChangeResponse struct {
	Setting *SettingView `json:"setting,omitzero"`
	Err     error        `json:"err,omitzero"`
}

func (r ChangeResponse) Failed() error { return r.Err }

// HistoryResponse is one setting's changes, newest first.
type HistoryResponse struct {
	Changes []ChangeView `json:"changes"`
	Err     error        `json:"err,omitzero"`
}

func (r HistoryResponse) Failed() error { return r.Err }

// SettingView is a setting as the Settings page shows it. Value is always
// empty for a secret; is_set says whether one is.
type SettingView struct {
	Name           string     `json:"name"`
	Group          string     `json:"group"`
	Label          string     `json:"label"`
	Help           string     `json:"help"`
	Default        string     `json:"default"`
	Kind           string     `json:"kind"`
	Secret         bool       `json:"secret"`
	Applies        string     `json:"applies"`
	Source         string     `json:"source"`
	Value          string     `json:"value"`
	IsSet          bool       `json:"is_set"`
	PendingRestart bool       `json:"pending_restart"`
	UpdatedAt      *time.Time `json:"updated_at,omitzero"`
	UpdatedBy      string     `json:"updated_by,omitzero"`
}

// EnvironmentView is a setting only the server's environment sets.
type EnvironmentView struct {
	Name   string `json:"name"`
	Class  string `json:"class"`
	Reason string `json:"reason"`
	Secret bool   `json:"secret"`
	IsSet  bool   `json:"is_set"`
	Value  string `json:"value"`
}

// ChangeView is one change in a setting's history. Value is null for a
// clearing, and empty for a secret.
type ChangeView struct {
	Value   *string   `json:"value"`
	Reason  string    `json:"reason"`
	SetBy   string    `json:"set_by"`
	SetAt   time.Time `json:"set_at"`
	Cleared bool      `json:"cleared"`
}

func listResponseOf(current contracts.ServerSettings) ListResponse {
	response := ListResponse{
		Settings:        make([]SettingView, 0, len(current.Settings)),
		EnvironmentOnly: make([]EnvironmentView, 0, len(current.EnvironmentOnly)),
	}
	for _, setting := range current.Settings {
		response.Settings = append(response.Settings, settingViewOf(setting))
	}
	for _, fixed := range current.EnvironmentOnly {
		response.EnvironmentOnly = append(response.EnvironmentOnly, EnvironmentView{
			Name: fixed.Fixed.Name, Class: string(fixed.Fixed.Class), Reason: fixed.Fixed.Reason,
			Secret: fixed.Fixed.Secret, IsSet: fixed.IsSet, Value: fixed.Value,
		})
	}
	return response
}

func settingViewOf(setting contracts.ServerSetting) SettingView {
	d := setting.Definition
	return SettingView{
		Name: d.Name, Group: string(d.Group), Label: d.Label, Help: d.Help, Default: d.Default,
		Kind: string(d.Kind), Secret: d.Secret, Applies: string(d.Applies),
		Source: string(setting.Source), Value: setting.Value, IsSet: setting.IsSet,
		PendingRestart: setting.PendingRestart, UpdatedAt: setting.UpdatedAt, UpdatedBy: setting.UpdatedBy,
	}
}

func changeViewsOf(history []entities.InstallationSetting) []ChangeView {
	views := make([]ChangeView, 0, len(history))
	for _, change := range history {
		views = append(views, ChangeView{
			Value: change.Value, Reason: change.Reason, SetBy: change.SetBy, SetAt: change.CreatedAt,
			Cleared: change.Value == nil,
		})
	}
	return views
}
