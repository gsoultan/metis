package settings_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/app"
	"github.com/gsoultan/metis/internal/pkg/health"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/endpoints"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
	"golang.org/x/crypto/bcrypt"
)

const (
	harnessPassword = "settings-test-password"
	settingsRoute   = "/api/v1/settings/server"
	mailPassword    = "the-mail-servers-password"
)

type settingsHarness struct {
	server *httptest.Server
	svc    services.ServiceFacade
	repo   repositories.Repository
	orgID  uuid.UUID
	hash   string
}

func newSettingsHarness(t *testing.T) *settingsHarness {
	t.Helper()
	conn := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(conn)
	sse := observersimpl.NewSSEObserver()
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), sse, "settings-test-secret", nil, nil, nil)
	handler, _ := app.BuildAPIHandler(svc, endpoints.MakeEndpoints(svc), sse, nil, map[string]health.Checker{}, conn)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	org, err := svc.CreateOrganization(context.Background(), "Settings Org", "")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(harnessPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	return &settingsHarness{server: server, svc: svc, repo: repo, orgID: org.ID, hash: string(hash)}
}

func (h *settingsHarness) signIn(t *testing.T, name string, roles ...string) string {
	t.Helper()
	account := entities.User{ID: uuid.Must(uuid.NewV7()), CreatedAt: time.Now(), Username: name, Roles: roles,
		Organizations: []*entities.Organization{{ID: h.orgID}}}
	if err := h.repo.User().Create(entities.WithSystemContext(context.Background()),
		adapters.UserModelAdapter{User: account}.ToModel(), h.hash); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	status, body := h.do(t, http.MethodPost, "", "/api/v1/login", map[string]string{"username": name, "password": harnessPassword})
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &out); status != http.StatusOK || err != nil || out.Token == "" {
		t.Fatalf("login %s: %d (%s)", name, status, body)
	}
	return out.Token
}

func (h *settingsHarness) do(t *testing.T, method, token, path string, body any) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, h.server.URL+path, &buf)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	var out bytes.Buffer
	_, _ = out.ReadFrom(resp.Body)
	return resp.StatusCode, out.String()
}

type settingRead struct {
	Name           string `json:"name"`
	Source         string `json:"source"`
	Value          string `json:"value"`
	IsSet          bool   `json:"is_set"`
	PendingRestart bool   `json:"pending_restart"`
	UpdatedBy      string `json:"updated_by"`
}

func (h *settingsHarness) read(t *testing.T, token string) (map[string]settingRead, string) {
	t.Helper()
	status, body := h.do(t, http.MethodGet, token, settingsRoute, nil)
	if status != http.StatusOK {
		t.Fatalf("reading the settings answered %d (%s)", status, body)
	}
	var got struct {
		Settings        []settingRead `json:"settings"`
		EnvironmentOnly []settingRead `json:"environment_only"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("read %s: %v", body, err)
	}
	byName := map[string]settingRead{}
	for _, s := range append(got.Settings, got.EnvironmentOnly...) {
		byName[s.Name] = s
	}
	return byName, body
}

func change(value any, reason string) map[string]any {
	return map[string]any{"value": value, "reason": reason}
}

// Only a platform administrator reads or changes the server's settings.
// Everybody else is refused, and on an installation of several organizations
// an administrator of one is refused too.
func TestOnlyAPlatformAdministratorReachesTheServerSettings(t *testing.T) {
	h := newSettingsHarness(t)
	host := settingsRoute + "/NOTIFICATION_SMTP_HOST"
	body := change("smtp.example.com", "the new mail relay")

	if status, answer := h.do(t, http.MethodGet, "", settingsRoute, nil); status != http.StatusUnauthorized {
		t.Fatalf("reading signed out answered %d (%s), want 401", status, answer)
	}
	for _, role := range []string{entities.RoleUser, entities.RoleOperator, entities.RoleDesigner} {
		token := h.signIn(t, "not-an-admin-"+role, role)
		if status, answer := h.do(t, http.MethodGet, token, settingsRoute, nil); status != http.StatusForbidden {
			t.Errorf("a %s reading the settings answered %d (%s), want 403", role, status, answer)
		}
		if status, answer := h.do(t, http.MethodPut, token, host, body); status != http.StatusForbidden {
			t.Errorf("a %s changing a setting answered %d (%s), want 403", role, status, answer)
		}
	}

	admin := h.signIn(t, "admin", entities.RoleAdmin)
	if status, answer := h.do(t, http.MethodPut, admin, host, body); status != http.StatusOK {
		t.Fatalf("the only organization's administrator changing a setting answered %d (%s), want 200", status, answer)
	}

	// A second organization: now the installation is shared, and an
	// administrator of one is not named in METIS_PLATFORM_ADMINS.
	if _, err := h.svc.CreateOrganization(context.Background(), "Second Org", ""); err != nil {
		t.Fatalf("create the second organization: %v", err)
	}
	if status, answer := h.do(t, http.MethodGet, admin, settingsRoute, nil); status != http.StatusForbidden {
		t.Errorf("an unnamed administrator of a shared installation reading the settings answered %d (%s), want 403", status, answer)
	}
	if status, answer := h.do(t, http.MethodPut, admin, host, body); status != http.StatusForbidden {
		t.Errorf("an unnamed administrator of a shared installation changing a setting answered %d (%s), want 403", status, answer)
	}
}

// A secret is saved, reported as set, and never returned: not by the list,
// not by the change, not by the history.
func TestASecretSettingIsNeverReturned(t *testing.T) {
	h := newSettingsHarness(t)
	admin := h.signIn(t, "admin", entities.RoleAdmin)
	route := settingsRoute + "/NOTIFICATION_SMTP_PASSWORD"

	status, answer := h.do(t, http.MethodPut, admin, route, change(mailPassword, "rotated by the mail team"))
	if status != http.StatusOK || strings.Contains(answer, mailPassword) {
		t.Fatalf("saving a secret answered %d (%s); want 200 without the secret in it", status, answer)
	}
	settings, body := h.read(t, admin)
	if strings.Contains(body, mailPassword) {
		t.Fatal("the settings list returned a secret")
	}
	if got := settings["NOTIFICATION_SMTP_PASSWORD"]; !got.IsSet || got.Source != "saved" || got.UpdatedBy != "admin" {
		t.Errorf("a saved secret reads %+v, want set, saved, by admin", got)
	}
	status, history := h.do(t, http.MethodGet, admin, route+"/history", nil)
	if status != http.StatusOK || strings.Contains(history, mailPassword) || !strings.Contains(history, "rotated by the mail team") {
		t.Errorf("the history answered %d (%s); want the change and its reason without the secret", status, history)
	}
}

// What the page cannot change it refuses, and says why.
func TestTheSettingsPageRefusesWhatItCannotChange(t *testing.T) {
	h := newSettingsHarness(t)
	admin := h.signIn(t, "admin", entities.RoleAdmin)
	t.Setenv("NOTIFICATION_SMTP_FROM", "ops@example.com")

	cases := map[string]struct {
		name string
		body any
		want int
	}{
		"set in the environment":   {"NOTIFICATION_SMTP_FROM", change("me@example.com", "x"), http.StatusForbidden},
		"a guardrail":              {"METIS_PLATFORM_ADMINS", change(uuid.NewString(), "x"), http.StatusForbidden},
		"the database":             {"DATABASE_URL", change("postgres://elsewhere/db", "x"), http.StatusForbidden},
		"not a setting":            {"METIS_NO_SUCH_SETTING", change("1", "x"), http.StatusNotFound},
		"out of bounds":            {"METIS_JOB_LEASE", change("30s", "x"), http.StatusBadRequest},
		"no reason":                {"METIS_JOB_WORKERS", change("20", ""), http.StatusBadRequest},
		"no value at all":          {"METIS_JOB_WORKERS", map[string]any{"reason": "x"}, http.StatusBadRequest},
		"an unknown field":         {"METIS_JOB_WORKERS", map[string]any{"value": "20", "reason": "x", "set_by": "someone"}, http.StatusBadRequest},
		"a header in a mail field": {"NOTIFICATION_SMTP_HOST", change("smtp.example.com\r\nBcc: x@example.com", "x"), http.StatusBadRequest},
	}
	for label, c := range cases {
		if status, answer := h.do(t, http.MethodPut, admin, settingsRoute+"/"+c.name, c.body); status != c.want {
			t.Errorf("%s answered %d (%s), want %d", label, status, answer, c.want)
		}
	}

	settings, _ := h.read(t, admin)
	if got := settings["NOTIFICATION_SMTP_FROM"]; got.Source != "environment" || got.Value != "ops@example.com" {
		t.Errorf("a setting the environment sets reads %+v, want it shown as the environment's", got)
	}
	if got := settings["DATABASE_URL"]; got.Value != "" {
		t.Error("an environment-only secret was returned")
	}
}

// A setting read at start-up says it waits for a restart; clearing it is a
// change like any other, and kept.
func TestARestartSettingSaysItWaitsForARestart(t *testing.T) {
	h := newSettingsHarness(t)
	admin := h.signIn(t, "admin", entities.RoleAdmin)
	route := settingsRoute + "/METIS_JOB_WORKERS"

	if status, answer := h.do(t, http.MethodPut, admin, route, change("20", "more throughput")); status != http.StatusOK {
		t.Fatalf("saving answered %d (%s)", status, answer)
	}
	settings, _ := h.read(t, admin)
	if got := settings["METIS_JOB_WORKERS"]; got.Value != "20" || got.Source != "saved" || !got.PendingRestart {
		t.Errorf("a saved restart setting reads %+v, want 20, saved, waiting for a restart", got)
	}

	if status, answer := h.do(t, http.MethodPut, admin, route, change(nil, "back to the default")); status != http.StatusOK {
		t.Fatalf("clearing answered %d (%s)", status, answer)
	}
	settings, _ = h.read(t, admin)
	if got := settings["METIS_JOB_WORKERS"]; got.Source != "default" || got.IsSet {
		t.Errorf("a cleared setting reads %+v, want the default", got)
	}
	status, history := h.do(t, http.MethodGet, admin, route+"/history", nil)
	if status != http.StatusOK || !strings.Contains(history, "back to the default") || !strings.Contains(history, "more throughput") {
		t.Errorf("the history answered %d (%s), want both changes", status, history)
	}
}
