package pg

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/installationsetting"
)

type installationSettingRepository struct{ conn }

// NewInstallationSettingRepository returns the store of the settings set from
// the Settings page.
//
// Installation-wide, so no tenant scope applies: who may read or change them
// is decided before this is reached, by the platform administrator gate.
func NewInstallationSettingRepository(c *db.Conn) contracts.InstallationSettingRepository {
	return &installationSettingRepository{conn{conn: c}}
}

// installationSettingRowsRead bounds the rows read to work out the current
// values. Each change is a row; an installation whose settings were changed
// this often is answered from its newest changes.
const installationSettingRowsRead = 10_000

// Record adds one change.
func (r *installationSettingRepository) Record(ctx context.Context, setting entities.InstallationSetting) (entities.InstallationSetting, error) {
	if setting.Name == "" {
		return entities.InstallationSetting{}, fmt.Errorf("a setting change names its setting")
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return entities.InstallationSetting{}, err
	}
	ins, err := stageSetting(setting)
	if err != nil {
		return entities.InstallationSetting{}, err
	}
	row, err := ins.Insert(ctx, ex)
	if err != nil {
		return entities.InstallationSetting{}, fmt.Errorf("could not record the setting: %w", err)
	}
	return settingFrom(row, false)
}

// Current reads the rows newest first and keeps the first of each setting.
func (r *installationSettingRepository) Current(ctx context.Context) ([]entities.InstallationSetting, error) {
	rows, err := r.read(ctx, installationsetting.New(), installationSettingRowsRead)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var current []entities.InstallationSetting
	for _, row := range rows {
		if seen[row.Name] {
			continue
		}
		seen[row.Name] = true
		setting, err := settingFrom(row, true)
		if err != nil {
			return nil, fmt.Errorf("could not open the setting %s: %w", row.Name, err)
		}
		current = append(current, setting)
	}
	return current, nil
}

// History lists one setting's changes, newest first.
func (r *installationSettingRepository) History(ctx context.Context, name string, limit int64) ([]entities.InstallationSetting, error) {
	rows, err := r.read(ctx, installationsetting.New().Where(installationsetting.Name.Eq(name)), limit)
	if err != nil {
		return nil, err
	}
	history := make([]entities.InstallationSetting, 0, len(rows))
	for _, row := range rows {
		setting, err := settingFrom(row, false)
		if err != nil {
			return nil, err
		}
		history = append(history, setting)
	}
	return history, nil
}

func (r *installationSettingRepository) read(ctx context.Context, q installationsetting.Query, limit int64) ([]installationsetting.Row, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := q.Order(installationsetting.CreatedAt.Desc(), installationsetting.ID.Desc()).Limit(limit).All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read the settings: %w", err)
	}
	return rows, nil
}

// stageSetting sets every column of a new row. Its id is a v7: two changes in
// one transaction share a creation time, and the newest is told by id.
func stageSetting(setting entities.InstallationSetting) (installationsetting.Ins, error) {
	id := setting.ID
	if id == uuid.Nil {
		id = uuid.Must(uuid.NewV7())
	}
	ins := installationsetting.Create()
	ins.SetID(id)
	ins.SetName(setting.Name)
	ins.SetSealed(setting.Secret)
	switch {
	case setting.Value == nil:
		ins.SetValueNull()
	case setting.Secret:
		// Refused rather than written in the clear when there is no key.
		sealed, err := sealedText(*setting.Value)
		if err != nil {
			return ins, fmt.Errorf("could not seal the setting %s: %w", setting.Name, err)
		}
		ins.SetValue(sealed)
	default:
		ins.SetValue(*setting.Value)
	}
	ins.SetReason(setting.Reason)
	ins.SetSetBy(setting.SetBy)
	ins.SetSetByID(setting.SetByID)
	return ins, nil
}

// settingFrom maps a row. A sealed value is opened only when open is asked
// for; otherwise it is left out, so nothing but the server's own reader holds
// a secret in the clear.
func settingFrom(row installationsetting.Row, open bool) (entities.InstallationSetting, error) {
	setting := entities.InstallationSetting{
		ID:        row.ID,
		CreatedAt: row.CreatedAt,
		Name:      row.Name,
		Secret:    row.Sealed,
		Reason:    row.Reason,
		SetBy:     row.SetBy,
		SetByID:   row.SetByID,
	}
	if !row.Value.Valid {
		return setting, nil
	}
	value := row.Value.V
	if row.Sealed {
		if !open {
			// Present, but not for this reader.
			value = ""
		} else {
			opened, err := openedText(value)
			if err != nil {
				return setting, err
			}
			value = opened
		}
	}
	setting.Value = &value
	return setting, nil
}
