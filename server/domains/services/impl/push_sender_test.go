package impl

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/fcm"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/impl/connectors"
)

// fakeConnections is a project's connections.
type fakeConnections []entities.ConnectorInstance

func (f fakeConnections) ListConnectorInstances(context.Context, uuid.UUID) ([]entities.ConnectorInstance, error) {
	return f, nil
}

// fakeDevices is the device table, enough of it for sending.
type fakeDevices struct {
	mu      sync.Mutex
	devices []entities.PersonDevice
	sent    map[uuid.UUID]bool
}

func (f *fakeDevices) Register(context.Context, entities.PersonDevice, string) (entities.PersonDevice, error) {
	return entities.PersonDevice{}, nil
}
func (f *fakeDevices) ListByParticipant(_ context.Context, participant uuid.UUID) ([]entities.PersonDevice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []entities.PersonDevice
	for _, d := range f.devices {
		if d.ParticipantID == participant {
			out = append(out, d)
		}
	}
	return out, nil
}
func (f *fakeDevices) Remove(context.Context, uuid.UUID, uuid.UUID) error { return apierr.ErrNotFound }
func (f *fakeDevices) ListInProject(context.Context, uuid.UUID, uuid.UUID) ([]entities.PersonDevice, error) {
	return nil, nil
}
func (f *fakeDevices) CountByProject(context.Context, uuid.UUID) (map[uuid.UUID]int, error) {
	return nil, nil
}
func (f *fakeDevices) RemoveInProject(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return nil
}
func (f *fakeDevices) Forget(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, d := range f.devices {
		if d.ID == id {
			f.devices = append(f.devices[:i], f.devices[i+1:]...)
			break
		}
	}
	return nil
}
func (f *fakeDevices) MarkSent(_ context.Context, id uuid.UUID, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent[id] = true
	return nil
}

// firebaseAnswering stands up a fake Firebase that answers each device token
// with the status given for it.
func firebaseAnswering(t *testing.T, status map[string]int) (*fcm.Pool, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	file, _ := json.Marshal(map[string]string{"project_id": "p", "client_email": "e@p.iam.gserviceaccount.com", "private_key": string(pemKey)})
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"access_token":"a","expires_in":3600}`)
	})
	mux.HandleFunc("/send", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Message struct {
				Token string `json:"token"`
			} `json:"message"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		code := status[body.Message.Token]
		w.WriteHeader(code)
		switch code {
		case http.StatusOK:
			_, _ = io.WriteString(w, `{"name":"m"}`)
		case http.StatusNotFound:
			_, _ = io.WriteString(w, `{"error":{"details":[{"errorCode":"UNREGISTERED"}]}}`)
		}
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return fcm.NewPool(fcm.WithEndpoints(server.URL+"/token", server.URL+"/send", server.Client())), string(file)
}

func pushConnection(key string, use string, updated time.Time) entities.ConnectorInstance {
	return entities.ConnectorInstance{
		ID:        uuid.Must(uuid.NewV7()),
		Connector: &entities.Connector{Key: connectors.FCMConnectorKey},
		Config:    map[string]any{connectors.FCMServiceAccountKey: key, connectors.FCMUseForPush: use},
		UpdatedAt: updated,
	}
}

func TestPushReachesEveryDeviceAndForgetsTheGoneOnes(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	project, ada := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	pool, key := firebaseAnswering(t, map[string]int{"phone": 200, "old-tablet": 404, "other-project": 200})
	phone := entities.PersonDevice{ID: uuid.Must(uuid.NewV7()), ProjectID: project, ParticipantID: ada, Token: "phone"}
	tablet := entities.PersonDevice{ID: uuid.Must(uuid.NewV7()), ProjectID: project, ParticipantID: ada, Token: "old-tablet"}
	stray := entities.PersonDevice{ID: uuid.Must(uuid.NewV7()), ProjectID: uuid.Must(uuid.NewV7()), ParticipantID: ada, Token: "other-project"}
	devices := &fakeDevices{devices: []entities.PersonDevice{phone, tablet, stray}, sent: map[uuid.UUID]bool{}}
	sender := NewPushSender(fakeConnections{pushConnection(key, "true", time.Now())}, devices, pool)

	result, err := sender.PushToPerson(ctx, project, ada, entities.PushContent{Title: "Approve PO", Body: "Waiting 3 days"})
	if err != nil {
		t.Fatalf("PushToPerson: %v", err)
	}
	if result != (entities.PushResult{Devices: 2, Delivered: 1, Forgotten: 1}) {
		t.Fatalf("result = %+v", result)
	}
	if !devices.sent[phone.ID] || devices.sent[stray.ID] {
		t.Fatalf("sent to %v; want the phone and not the other project's device", devices.sent)
	}
	if left, _ := devices.ListByParticipant(ctx, ada); len(left) != 2 {
		t.Fatalf("the gone tablet should be forgotten, left %+v", left)
	}
}

func TestPushWithNoConnectionOrNoDevicesIsAnAnswerNotAnError(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	pool, key := firebaseAnswering(t, nil)
	devices := &fakeDevices{sent: map[uuid.UUID]bool{}}
	cases := map[string]fakeConnections{
		"no connection":     nil,
		"push switched off": {pushConnection(key, "false", time.Now())},
		"no devices":        {pushConnection(key, "true", time.Now())},
	}
	for name, connections := range cases {
		t.Run(name, func(t *testing.T) {
			sender := NewPushSender(connections, devices, pool)
			result, err := sender.PushToPerson(ctx, uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), entities.PushContent{Title: "x"})
			if err != nil || result.Delivered != 0 {
				t.Fatalf("result %+v, err %v; want nothing delivered and no error", result, err)
			}
		})
	}
}

func TestPushConfiguredReadsTheNewestConnectionMarkedForPush(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	older := pushConnection("older-key", "true", time.Now().Add(-time.Hour))
	newer := pushConnection("newer-key", "true", time.Now())
	off := pushConnection("off-key", "false", time.Now().Add(time.Hour))
	key, found, err := pushConnectionKey(ctx, fakeConnections{older, off, newer}, uuid.Nil)
	if err != nil || !found || key != "newer-key" {
		t.Fatalf("key %q found %v err %v; want the newest connection marked for push", key, found, err)
	}
	configured, err := NewPushSender(fakeConnections{off}, &fakeDevices{}, nil).PushConfigured(ctx, uuid.Nil)
	if err != nil || configured {
		t.Fatalf("a connection with push off reads as configured: %v %v", configured, err)
	}
}

func TestPushStopsAtARefusedConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	project, ada := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	pool, key := firebaseAnswering(t, map[string]int{"a": http.StatusForbidden, "b": http.StatusForbidden})
	devices := &fakeDevices{sent: map[uuid.UUID]bool{}, devices: []entities.PersonDevice{
		{ID: uuid.Must(uuid.NewV7()), ProjectID: project, ParticipantID: ada, Token: "a"},
		{ID: uuid.Must(uuid.NewV7()), ProjectID: project, ParticipantID: ada, Token: "b"},
	}}
	_, err := NewPushSender(fakeConnections{pushConnection(key, "true", time.Now())}, devices, pool).
		PushToPerson(ctx, project, ada, entities.PushContent{Title: "x"})
	if err == nil || !strings.Contains(err.Error(), "refused the service account") {
		t.Fatalf("err = %v, want the refused connection", err)
	}
	if left, _ := devices.ListByParticipant(ctx, ada); len(left) != 2 {
		t.Fatal("a refused connection must not forget devices")
	}
}
