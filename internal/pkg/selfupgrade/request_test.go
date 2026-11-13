package selfupgrade

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestARequestIsTakenOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade-request.json")
	want := Request{To: "v0.8.0", RequestedBy: "admin", RequestedAt: time.Unix(1, 0).UTC()}
	if err := WriteRequest(path, want); err != nil {
		t.Fatal(err)
	}
	if err := WriteRequest(path, want); err == nil {
		t.Fatal("a second request was written over a waiting one")
	}
	if pending, err := PendingRequest(path); err != nil || pending == nil || pending.To != "v0.8.0" {
		t.Fatalf("pending %+v, %v", pending, err)
	}
	got, err := TakeRequest(path)
	if err != nil || got != want {
		t.Fatalf("took %+v, %v", got, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("the request was left to fire again")
	}
	if pending, _ := PendingRequest(path); pending != nil {
		t.Fatal("a taken request still shows as pending")
	}
}

func TestARequestOnlyNamesARelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade-request.json")
	if err := WriteRequest(path, Request{To: "--allow-downgrade"}); err == nil {
		t.Fatal("a request that is not a release was written")
	}
	if err := os.WriteFile(path, []byte(`{"to":"main"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := TakeRequest(path); err == nil {
		t.Fatal("a request for a branch was taken")
	}
}

// The service account writes the request and root reads it; a link planted
// in its place must not make root read, and repeat, another file.
func TestARequestThatIsALinkIsRefused(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "shadow")
	if err := os.WriteFile(secret, []byte(`{"to":"v0.8.0","requested_by":"root:$6$hash"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "upgrade-request.json")
	if err := os.Symlink(secret, path); err != nil {
		t.Fatal(err)
	}
	_, err := TakeRequest(path)
	if err == nil {
		t.Fatal("a request read through a link")
	}
	if strings.Contains(err.Error(), "hash") {
		t.Fatalf("the error repeats the file: %v", err)
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatal("the link's target was removed")
	}
}
