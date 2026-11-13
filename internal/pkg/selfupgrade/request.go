package selfupgrade

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gsoultan/metis/internal/pkg/releasecheck"
)

// Request is what the Upgrades page leaves for the helper: the release to go
// to and who asked. The server writes it as the unprivileged service account;
// the helper runs as root and reads it. It carries nothing the helper takes on
// trust: the release is verified as if typed at the command line, and a
// downgrade cannot be asked for this way.
type Request struct {
	To          string    `json:"to"`
	RequestedBy string    `json:"requested_by"`
	RequestedAt time.Time `json:"requested_at"`
}

// maxRequest bounds the file the helper reads.
const maxRequest = 4 << 10

// WriteRequest writes a request atomically, so the helper never sees half of
// one, and refuses while one is already waiting.
func WriteRequest(path string, r Request) error {
	if _, ok := releasecheck.CompareTags(r.To, r.To); !ok {
		return fmt.Errorf("%q is not a release", r.To)
	}
	if _, err := os.Lstat(path); err == nil {
		return errors.New("an upgrade has already been asked for and has not started yet")
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".request-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// TakeRequest reads a request and removes it, so the trigger does not fire
// again for the same one. It reads as root a file another account wrote, so
// it follows no link, reads only a regular file of a few bytes, and never
// repeats what the file holds in an error.
func TakeRequest(path string) (Request, error) {
	f, err := os.OpenFile(filepath.Clean(path), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return Request{}, fmt.Errorf("open the upgrade request: %w", err)
	}
	defer func() { _ = f.Close() }()
	// Removed whatever it holds: a request that cannot be read is not retried.
	defer func() { _ = os.Remove(path) }()
	info, err := f.Stat()
	if err != nil {
		return Request{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxRequest {
		return Request{}, errors.New("the upgrade request is not a small regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxRequest))
	if err != nil {
		return Request{}, err
	}
	var r Request
	if err := json.Unmarshal(raw, &r); err != nil {
		return Request{}, errors.New("the upgrade request is not one the Upgrades page wrote")
	}
	if _, ok := releasecheck.CompareTags(r.To, r.To); !ok {
		return Request{}, errors.New("the upgrade request does not name a release")
	}
	if len(r.RequestedBy) > 200 {
		r.RequestedBy = r.RequestedBy[:200]
	}
	return r, nil
}

// PendingRequest reads a waiting request without taking it, for the page.
func PendingRequest(path string) (*Request, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r Request
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	return &r, nil
}
