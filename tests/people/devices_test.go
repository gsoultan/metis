package people_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/gsoultan/metis/server/domains/entities"
)

const adaPhoneToken = "fcm-token-ada-iphone-7Hq2Ka91F"

func (w world) signInAs(t *testing.T, username string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(personPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.conn.Main().Exec(t.Context(),
		"UPDATE workflow_users SET password_hash = $1 WHERE project_id = $2 AND username = $3", string(hash), w.project, username); err != nil {
		t.Fatal(err)
	}
	code, body := w.do(t, http.MethodPost, "/api/v1/people/login", "", map[string]string{
		"project_id": w.project.String(), "username": username, "password": personPassword,
	})
	if code != http.StatusOK {
		t.Fatalf("%s signs in: %d %s", username, code, body)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	return out.Token
}

func (w world) register(t *testing.T, token, deviceToken, platform string) (int, entities.PersonDevice) {
	t.Helper()
	code, body := w.do(t, http.MethodPost, "/api/v1/people/devices", token,
		map[string]string{"token": deviceToken, "platform": platform, "label": "iPhone 15"})
	var out struct {
		Device entities.PersonDevice `json:"device"`
	}
	if code == http.StatusOK {
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatal(err)
		}
	}
	return code, out.Device
}

func (w world) devices(t *testing.T, token string) []entities.PersonDevice {
	t.Helper()
	code, body := w.do(t, http.MethodGet, "/api/v1/people/devices", token, nil)
	if code != http.StatusOK {
		t.Fatalf("list devices: %d %s", code, body)
	}
	if strings.Contains(body, adaPhoneToken) {
		t.Fatalf("the device list %s shows a token", body)
	}
	var out struct {
		Devices []entities.PersonDevice `json:"devices"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	return out.Devices
}

// TestAPersonRegistersTheirPhoneForPush: a person's app registers a device
// with their sign-in or their API key, registering it again changes nothing,
// and the list says which device is which without giving the token back.
func TestAPersonRegistersTheirPhoneForPush(t *testing.T) {
	w := newWorld(t)
	session := w.signIn(t)
	_, appKey := w.makeKey(t, session, "approvals app")

	code, phone := w.register(t, appKey, adaPhoneToken, "iOS")
	if code != http.StatusOK || phone.Platform != entities.DevicePlatformIOS || phone.TokenTail != "a91F" {
		t.Fatalf("the app registers ada's phone with her key: %d %+v", code, phone)
	}
	if code, again := w.register(t, session, adaPhoneToken, "ios"); code != http.StatusOK || again.ID != phone.ID {
		t.Fatalf("registering the same phone again = %d %+v, want the same device %s", code, again, phone.ID)
	}
	if listed := w.devices(t, session); len(listed) != 1 || listed[0].ID != phone.ID {
		t.Fatalf("ada's devices = %+v, want her one phone", listed)
	}

	for name, body := range map[string]map[string]string{
		"no token":         {"platform": "ios"},
		"token with space": {"token": "a b", "platform": "ios"},
		"unknown platform": {"token": "t", "platform": "blackberry"},
	} {
		if code, reply := w.do(t, http.MethodPost, "/api/v1/people/devices", session, body); code != http.StatusBadRequest {
			t.Errorf("%s = %d %s, want 400", name, code, reply)
		}
	}
	if code, reply := w.do(t, http.MethodDelete, "/api/v1/people/devices/"+phone.ID.String(), appKey, nil); code != http.StatusOK {
		t.Fatalf("the app removes the phone on sign-out: %d %s", code, reply)
	}
	if listed := w.devices(t, session); len(listed) != 0 {
		t.Fatalf("after removal ada has %+v", listed)
	}
}

// TestADeviceBelongsToWhoeverRegisteredItLast: a phone that changes hands
// stops receiving the first person's pushes, and nobody can remove another
// person's device.
func TestADeviceBelongsToWhoeverRegisteredItLast(t *testing.T) {
	w := newWorld(t)
	ada := w.signIn(t)
	w.seedPerson(t, "bob", "bob@example.com")
	bob := w.signInAs(t, "bob")

	_, phone := w.register(t, ada, adaPhoneToken, "android")
	if code, reply := w.do(t, http.MethodDelete, "/api/v1/people/devices/"+phone.ID.String(), bob, nil); code != http.StatusNotFound {
		t.Fatalf("bob removing ada's phone = %d %s, want 404", code, reply)
	}
	if code, _ := w.register(t, bob, adaPhoneToken, "android"); code != http.StatusOK {
		t.Fatalf("bob signs in on the phone ada used: %d", code)
	}
	if listed := w.devices(t, ada); len(listed) != 0 {
		t.Fatalf("ada still has %+v after the phone passed to bob", listed)
	}
	if listed := w.devices(t, bob); len(listed) != 1 {
		t.Fatalf("bob's devices = %+v, want the phone", listed)
	}
}

// TestAnAdministratorSeesAndRemovesAPersonsDevices: the People page counts a
// project's devices and removes a lost one. A person's token does not reach
// those routes.
func TestAnAdministratorSeesAndRemovesAPersonsDevices(t *testing.T) {
	w := newWorld(t)
	session := w.signIn(t)
	adaID := w.personID(t, "ada")
	_, phone := w.register(t, session, adaPhoneToken, "ios")
	w.register(t, session, "fcm-token-ada-pixel-7cQ2", "android")

	base := "/api/v1/participants/" + adaID.String() + "/devices"
	query := "?project_id=" + w.project.String()
	code, body := w.do(t, http.MethodGet, "/api/v1/participants/device-counts"+query, w.userToken, nil)
	if code != http.StatusOK || !strings.Contains(body, `"`+adaID.String()+`":2`) {
		t.Fatalf("device counts = %d %s, want ada with 2", code, body)
	}
	if code, body := w.do(t, http.MethodGet, base+query, w.userToken, nil); code != http.StatusOK || strings.Contains(body, adaPhoneToken) {
		t.Fatalf("ada's devices for dana = %d %s", code, body)
	}
	if code, body := w.do(t, http.MethodGet, base+query, session, nil); code != http.StatusUnauthorized {
		t.Errorf("a person reading the administrator's route = %d %s, want 401", code, body)
	}
	if code, body := w.do(t, http.MethodGet, base, w.userToken, nil); code != http.StatusBadRequest {
		t.Errorf("no project = %d %s, want 400", code, body)
	}
	if code, body := w.do(t, http.MethodDelete, base+"/"+phone.ID.String()+query, w.userToken, nil); code != http.StatusOK {
		t.Fatalf("dana removes ada's lost phone: %d %s", code, body)
	}
	if listed := w.devices(t, session); len(listed) != 1 {
		t.Fatalf("ada has %+v after dana removed one of two", listed)
	}
}
