package fcm

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func testKeyFile(t *testing.T) (string, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	file, err := json.Marshal(map[string]string{
		"type":         "service_account",
		"project_id":   "acme-approvals",
		"client_email": "push@acme-approvals.iam.gserviceaccount.com",
		"private_key":  string(pemKey),
		"token_uri":    "https://attacker.example/token",
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(file), key
}

// fakeFirebase answers sign-in and send like Google does.
type fakeFirebase struct {
	key        *rsa.PrivateKey
	signIns    atomic.Int32
	sendStatus int
	sendBody   string
	lastSend   atomic.Value // string
}

func (f *fakeFirebase) serve(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", f.token(t))
	mux.HandleFunc("/send", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer access-1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		f.lastSend.Store(string(body))
		w.WriteHeader(f.sendStatus)
		_, _ = io.WriteString(w, f.sendBody)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func (f *fakeFirebase) token(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.signIns.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Errorf("sign-in form: %v", err)
		}
		claims := jwt.MapClaims{}
		_, err := jwt.ParseWithClaims(r.PostForm.Get("assertion"), claims, func(*jwt.Token) (any, error) {
			return &f.key.PublicKey, nil
		}, jwt.WithValidMethods([]string{"RS256"}))
		if err != nil || claims["scope"] != messagingScope || claims["iss"] != "push@acme-approvals.iam.gserviceaccount.com" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"access-1","expires_in":3600}`)
	}
}

func clientFor(t *testing.T, fake *fakeFirebase) *Client {
	t.Helper()
	file, key := testKeyFile(t)
	fake.key = key
	server := fake.serve(t)
	pool := NewPool(WithEndpoints(server.URL+"/token", server.URL+"/send", server.Client()))
	client, err := pool.Client(file)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestSendDeliversAndSignsInOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	fake := &fakeFirebase{sendStatus: http.StatusOK, sendBody: `{"name":"projects/acme-approvals/messages/42"}`}
	client := clientFor(t, fake)

	for range 3 {
		id, err := client.Send(ctx, Message{Token: "device-1", Title: "Approve PO", Body: "Waiting 3 days", Link: "https://metis.example/p/t/1"})
		if err != nil {
			t.Fatalf("Send: %v", err)
		}
		if id != "projects/acme-approvals/messages/42" {
			t.Fatalf("message id = %q", id)
		}
	}
	if got := fake.signIns.Load(); got != 1 {
		t.Fatalf("signed in %d times for three sends, want once", got)
	}
	sent := fake.lastSend.Load().(string)
	for _, want := range []string{`"token":"device-1"`, `"title":"Approve PO"`, `"link":"https://metis.example/p/t/1"`, `"fcm_options"`} {
		if !strings.Contains(sent, want) {
			t.Errorf("sent body %s lacks %s", sent, want)
		}
	}
}

func TestSendClassifiesFirebaseRefusals(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"uninstalled app", 404, `{"error":{"message":"not found","details":[{"errorCode":"UNREGISTERED"}]}}`, ErrUnregistered},
		{"token from another project", 403, `{"error":{"details":[{"errorCode":"SENDER_ID_MISMATCH"}]}}`, ErrUnregistered},
		{"apns key missing", 401, `{"error":{"details":[{"errorCode":"THIRD_PARTY_AUTH_ERROR"}]}}`, ErrCredentials},
		{"bad message", 400, `{"error":{"details":[{"errorCode":"INVALID_ARGUMENT"}]}}`, ErrRejected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			client := clientFor(t, &fakeFirebase{sendStatus: tc.status, sendBody: tc.body})
			_, err := client.Send(ctx, Message{Token: "device-1", Title: "x"})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestServerErrorIsRetryableNotClassified(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	client := clientFor(t, &fakeFirebase{sendStatus: 503, sendBody: `{"error":{"message":"unavailable"}}`})
	_, err := client.Send(ctx, Message{Token: "device-1", Title: "x"})
	if err == nil || errors.Is(err, ErrUnregistered) || errors.Is(err, ErrCredentials) || errors.Is(err, ErrRejected) {
		t.Fatalf("err = %v, want a plain retryable error", err)
	}
}

func TestParseServiceAccountRefusesWithoutEchoingTheKey(t *testing.T) {
	cases := map[string]string{
		"not json":     "hello",
		"wrong type":   `{"type":"authorized_user","project_id":"p","client_email":"e","private_key":"k"}`,
		"missing bits": `{"type":"service_account","private_key":"SECRET-MATERIAL"}`,
		"not rsa":      `{"project_id":"p","client_email":"e","private_key":"SECRET-MATERIAL"}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseServiceAccount(raw)
			if !errors.Is(err, ErrInvalidServiceAccount) {
				t.Fatalf("err = %v, want ErrInvalidServiceAccount", err)
			}
			if strings.Contains(err.Error(), "SECRET-MATERIAL") {
				t.Fatalf("the error repeats the private key: %v", err)
			}
		})
	}
}

func TestClientIgnoresTheFilesTokenURI(t *testing.T) {
	file, _ := testKeyFile(t)
	account, err := ParseServiceAccount(file)
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(account)
	if client.tokens.tokenURL != GoogleTokenURL {
		t.Fatalf("token URL = %q, want Google's", client.tokens.tokenURL)
	}
	if want := "https://fcm.googleapis.com/v1/projects/" + url.PathEscape("acme-approvals") + "/messages:send"; client.sendURL != want {
		t.Fatalf("send URL = %q", client.sendURL)
	}
}
