package selfupgrade

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestProbeBase(t *testing.T) {
	cases := map[string]string{
		":8080":          "http://127.0.0.1:8080",
		"0.0.0.0:9000":   "http://127.0.0.1:9000",
		"[::]:9000":      "http://127.0.0.1:9000",
		"10.0.0.5:8080":  "http://10.0.0.5:8080",
		"localhost:8273": "http://localhost:8273",
	}
	for in, want := range cases {
		if got, err := ProbeBase(in); err != nil || got != want {
			t.Errorf("ProbeBase(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ProbeBase("8080"); err == nil {
		t.Error("a bare port was accepted")
	}
}

func TestLocalServerReadsTheVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			_, _ = w.Write([]byte(`{"status":"ok","version":"v0.8.0"}`))
		case "/readyz":
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()
	s := LocalServer{Client: srv.Client(), Base: srv.URL}
	if v, err := s.Version(t.Context()); err != nil || v != "v0.8.0" {
		t.Fatalf("version %q, %v", v, err)
	}
	if s.Ready(t.Context()) {
		t.Fatal("a 503 from /readyz read as ready")
	}
}

func TestDownloadsOnlyReleaseFiles(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		_, _ = w.Write([]byte("content"))
	}))
	defer srv.Close()
	g := GitHubReleases{Client: srv.Client(), Base: srv.URL + "/dl/"}
	if body, err := g.Asset(t.Context(), "v0.8.0", "checksums.txt"); err != nil || string(body) != "content" {
		t.Fatalf("%q, %v", body, err)
	}
	for _, bad := range [][2]string{{"../../etc", "x"}, {"v0.8.0", "../passwd"}, {"main", "checksums.txt"}} {
		if _, err := g.Asset(t.Context(), bad[0], bad[1]); err == nil {
			t.Errorf("%v was downloaded", bad)
		}
	}
	if !slices.Equal(asked, []string{"/dl/v0.8.0/checksums.txt"}) {
		t.Fatalf("asked %v", asked)
	}
}

func TestTheBackupGetsTheConnectionInItsEnvironment(t *testing.T) {
	env, err := libpqEnvironment("postgres://metis:s3cret@db.internal:6543/metis?sslmode=verify-full")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"PGHOST=db.internal", "PGPORT=6543", "PGUSER=metis", "PGPASSWORD=s3cret", "PGDATABASE=metis", "PGSSLMODE=verify-full"} {
		if !slices.Contains(env, want) {
			t.Errorf("%s missing from %v", want, env)
		}
	}
	if got := sslMode("host=db user=metis sslmode='require'"); got != "require" {
		t.Errorf("keyword sslmode %q", got)
	}
}
