package model

import "github.com/gsoultan/storm"

// InstallationSetting is one change to a server setting made from the
// Settings page: the value an administrator gave a setting that is otherwise
// read from the environment, or nil for "back to the environment's value, or
// the default".
//
// Rows are never changed or removed. A setting's value is the one its newest
// row says, and the rows before it are the record of who changed it, when and
// why — the audit trail for settings, which belong to no instance.
//
// A secret's value is sealed under ENCRYPTION_KEY, so --reseal moves it with
// every other sealed column, and a backup of this table opens nothing.
type InstallationSetting struct {
	storm.Model

	// Name is the setting's environment variable, such as
	// NOTIFICATION_SMTP_HOST: one name for a setting, wherever it is set.
	Name  string
	Value *string
	// Sealed says Value is ciphertext.
	Sealed bool

	Reason string
	SetBy  string
	// SetByUser is the account that made the change, kept as set_by_id. It
	// has no foreign key: the history outlives the account.
	SetByUser User
}

func (s *InstallationSetting) Schema(t *storm.Table) {
	t.Name("installation_settings")
	t.Col(&s.Name).Size(100)
	t.Col(&s.SetBy).Size(255)
	t.Col(&s.SetByUser).Named("set_by_id").NoIndex().AcknowledgeNoFK("the history outlives the account that made the change")
	// Every setting's newest row, which the server reads at boot, and one
	// setting's history, newest first.
	t.Index(&s.Name, &s.CreatedAt, &s.ID).Named("ix_installation_settings_name")
}
