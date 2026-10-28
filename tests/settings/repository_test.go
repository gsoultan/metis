package settings_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/pg"
	"github.com/gsoultan/metis/tests/testutils"
)

func value(s string) *string { return &s }

// A setting's current value is its newest change; clearing it is a change
// too, and the history keeps every one.
func TestTheNewestChangeIsTheSettingsValue(t *testing.T) {
	conn := testutils.SetupTestConn(t)
	repo := pg.NewInstallationSettingRepository(conn)
	ctx := entities.WithSystemContext(t.Context())
	admin := uuid.New()

	record := func(name string, v *string) {
		t.Helper()
		if _, err := repo.Record(ctx, entities.InstallationSetting{
			Name: name, Value: v, Reason: "test", SetBy: "admin", SetByID: admin,
		}); err != nil {
			t.Fatalf("record %s: %v", name, err)
		}
	}
	record("NOTIFICATION_SMTP_HOST", value("old.example.com"))
	record("NOTIFICATION_SMTP_HOST", value("smtp.example.com"))
	record("METIS_JOB_WORKERS", value("20"))
	record("METIS_JOB_WORKERS", nil)

	current := currentByName(t, ctx, repo.Current)
	if got := current["NOTIFICATION_SMTP_HOST"]; got.Value == nil || *got.Value != "smtp.example.com" {
		t.Fatalf("the current value is not the newest change: %+v", got)
	}
	if got := current["METIS_JOB_WORKERS"]; got.Value != nil {
		t.Fatalf("a cleared setting still has a value: %q", *got.Value)
	}

	history, err := repo.History(ctx, "NOTIFICATION_SMTP_HOST", 10)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 2 || *history[0].Value != "smtp.example.com" || history[0].SetByID != admin {
		t.Fatalf("the history is not every change, newest first, with who made it: %+v", history)
	}
}

// A secret is sealed in its row, opened for the server that runs on it, and
// never opened for a history.
func TestASecretIsSealedAtRestAndLeftOutOfItsHistory(t *testing.T) {
	gormDB, conn := testutils.SetupTestStore(t)
	repo := pg.NewInstallationSettingRepository(conn)
	ctx := entities.WithSystemContext(t.Context())
	const password = "mail-password-in-the-clear"

	if _, err := repo.Record(ctx, entities.InstallationSetting{
		Name: "NOTIFICATION_SMTP_PASSWORD", Value: value(password), Secret: true, SetBy: "admin", SetByID: uuid.New(),
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	var stored string
	if err := gormDB.Raw(`SELECT value FROM installation_settings WHERE name = 'NOTIFICATION_SMTP_PASSWORD'`).Scan(&stored).Error; err != nil {
		t.Fatalf("read the row: %v", err)
	}
	if stored == password || stored == "" {
		t.Fatalf("the secret is stored in the clear: %q", stored)
	}

	current := currentByName(t, ctx, repo.Current)
	if got := current["NOTIFICATION_SMTP_PASSWORD"]; got.Value == nil || *got.Value != password {
		t.Fatal("the server could not open its own secret")
	}
	history, err := repo.History(ctx, "NOTIFICATION_SMTP_PASSWORD", 10)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 1 || history[0].Value == nil || *history[0].Value != "" {
		t.Fatalf("a history must say a secret was set, never what it was: %+v", history)
	}
}

func currentByName(t *testing.T, ctx context.Context, read func(context.Context) ([]entities.InstallationSetting, error)) map[string]entities.InstallationSetting {
	t.Helper()
	current, err := read(ctx)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	byName := map[string]entities.InstallationSetting{}
	for _, s := range current {
		byName[s.Name] = s
	}
	return byName
}
