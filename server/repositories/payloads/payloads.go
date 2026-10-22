// Package payloads keeps an instance's large variable values out of its row.
//
// An instance's variables are one sealed column, rewritten whole on every step
// and copied whole into a snapshot by every step. A value of a few megabytes —
// a document a service returned, a list a worker sent back — was therefore
// written again, and kept again, every time the instance moved, whether or
// not it had changed. With a payload directory configured, a value larger
// than the threshold is sealed once into a file of its instance's, named by
// its content, and the row holds a small reference to it. A step that leaves
// the value as it was writes the same reference and no file.
//
// References are resolved when a row is read, so nothing above the
// repositories sees one: an expression, a form or an API answer gets the
// value. A reference whose file cannot be read is an error, never a missing
// variable: an instance that decided without a value it had would be wrong
// without saying so.
//
// Off unless METIS_PAYLOAD_DIR is set. Every replica of an installation must
// see the same directory.
package payloads

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/crypto"
	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/server/repositories/db"
)

const (
	// EnvDir turns the store on: the directory the values are kept in.
	EnvDir = "METIS_PAYLOAD_DIR"
	// EnvThreshold is the size in bytes of a value's JSON above which it is
	// kept in the directory.
	EnvThreshold = "METIS_PAYLOAD_THRESHOLD_BYTES"

	// DefaultThreshold keeps an ordinary instance's variables in its row: a
	// value this large is a document, not a field.
	DefaultThreshold = 64 << 10
	// MinThreshold refuses a setting that would send every small value to a
	// file read on every load.
	MinThreshold = 1 << 10
)

// The shapes a top-level variable takes in a stored row when it is not the
// value itself.
//
// refKey marks a reference: {"$metisPayload": "<name>", "bytes": <size>}.
// literalKey wraps a value somebody set that has one of these keys itself, so
// a caller cannot set a variable that reads back as a reference — one that
// pointed at nothing would make the instance unreadable.
const (
	refKey     = "$metisPayload"
	sizeKey    = "bytes"
	literalKey = "$metisLiteral"
)

// ErrNotConfigured is what reading a reference answers when no directory is
// set: the values were kept in one, and this server cannot see it.
var ErrNotConfigured = errors.New("this instance holds a value kept in the payload directory, and " + EnvDir + " is not set")

type store struct {
	root      string
	threshold int
}

var (
	mu     sync.RWMutex
	active *store
)

// Configure turns the store on with dir, or off when dir is empty. The
// directory is created if it is not there, and must be writable.
func Configure(dir string, threshold int) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		mu.Lock()
		active = nil
		mu.Unlock()
		return nil
	}
	if threshold < MinThreshold {
		return fmt.Errorf("%s must be at least %d bytes, not %d", EnvThreshold, MinThreshold, threshold)
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("%s: %w", EnvDir, err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("%s: could not create %s: %w", EnvDir, root, err)
	}
	probe, err := os.CreateTemp(root, ".probe-*")
	if err != nil {
		return fmt.Errorf("%s: %s is not writable: %w", EnvDir, root, err)
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	mu.Lock()
	active = &store{root: root, threshold: threshold}
	mu.Unlock()
	return nil
}

// ConfigureFromEnv reads EnvDir and EnvThreshold.
func ConfigureFromEnv() error {
	threshold := DefaultThreshold
	if raw := strings.TrimSpace(envvar.Get(EnvThreshold)); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("%s must be a whole number of bytes, not %q", EnvThreshold, raw)
		}
		threshold = n
	}
	return Configure(envvar.Get(EnvDir), threshold)
}

// Enabled reports whether large values are being kept out of rows.
func Enabled() bool {
	return current() != nil
}

// Root is the configured directory, or "" when the store is off.
func Root() string {
	if s := current(); s != nil {
		return s.root
	}
	return ""
}

// ResetForTest turns the store off.
func ResetForTest() {
	_ = Configure("", 0)
}

func current() *store {
	mu.RLock()
	defer mu.RUnlock()
	return active
}

// Offload answers values as they are stored for instance: each value above
// the threshold kept in the directory and referred to, and each that has one
// of the store's own keys wrapped so it reads back as itself. values is not
// changed. It runs whether or not the store is on, so the wrapping is always
// done.
func Offload(ctx context.Context, instance uuid.UUID, values map[string]any) (map[string]any, error) {
	if len(values) == 0 {
		return values, nil
	}
	s := current()
	var out map[string]any
	set := func(key string, value any) {
		if out == nil {
			out = maps.Clone(values)
		}
		out[key] = value
	}
	for key, value := range values {
		if object, ok := value.(map[string]any); ok && hasStoreKey(object) {
			set(key, map[string]any{literalKey: value})
			continue
		}
		if s == nil || !mayBeLarge(value) {
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("could not encode variable %q: %w", key, err)
		}
		if len(encoded) <= s.threshold {
			continue
		}
		name, err := s.put(ctx, instance, encoded)
		if err != nil {
			return nil, fmt.Errorf("could not keep variable %q in the payload directory: %w", key, err)
		}
		set(key, map[string]any{refKey: name, sizeKey: len(encoded)})
	}
	if out == nil {
		return values, nil
	}
	return out, nil
}

// Resolve answers values as they were before Offload: references read back
// from the directory and wrapped values unwrapped. values is not changed.
func Resolve(ctx context.Context, instance uuid.UUID, values map[string]any) (map[string]any, error) {
	var out map[string]any
	for key, value := range values {
		object, ok := value.(map[string]any)
		if !ok || !hasStoreKey(object) {
			continue
		}
		if out == nil {
			out = maps.Clone(values)
		}
		if wrapped, ok := object[literalKey]; ok && len(object) == 1 {
			out[key] = wrapped
			continue
		}
		name, ok := object[refKey].(string)
		if !ok || !validName(name) {
			return nil, fmt.Errorf("variable %q is stored as a reference that names no value", key)
		}
		s := current()
		if s == nil {
			return nil, fmt.Errorf("variable %q: %w", key, ErrNotConfigured)
		}
		resolved, err := s.get(ctx, instance, name)
		if err != nil {
			return nil, fmt.Errorf("could not read variable %q from the payload directory: %w", key, err)
		}
		out[key] = resolved
	}
	if out == nil {
		return values, nil
	}
	return out, nil
}

func hasStoreKey(object map[string]any) bool {
	_, ref := object[refKey]
	_, literal := object[literalKey]
	return ref || literal
}

// mayBeLarge skips encoding what cannot pass any threshold: numbers, booleans
// and short strings.
func mayBeLarge(value any) bool {
	switch v := value.(type) {
	case nil, bool, float64, float32, int, int64, int32, json.Number:
		return false
	case string:
		return len(v) > MinThreshold
	}
	return true
}

func validName(name string) bool {
	if len(name) != 64 {
		return false
	}
	_, err := hex.DecodeString(name)
	return err == nil
}

// partition is the directory of the database the work is bound to, so that
// each environment's instances are kept, and swept, apart from the others'.
func partition(ctx context.Context) string {
	if id, ok := db.EnvironmentFrom(ctx); ok {
		return "environment-" + id.String()
	}
	return "main"
}

func (s *store) instanceDir(ctx context.Context, instance uuid.UUID) string {
	return filepath.Join(s.root, partition(ctx), instance.String())
}

// put seals encoded into the instance's directory under a name for its
// content, unless it is there already, and answers the name.
func (s *store) put(ctx context.Context, instance uuid.UUID, encoded []byte) (string, error) {
	name, err := crypto.Fingerprint(encoded)
	if err != nil {
		return "", err
	}
	dir := s.instanceDir(ctx, instance)
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err == nil {
		return name, nil
	}
	sealed, err := crypto.Encrypt(string(encoded))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := writeFile(dir, path, []byte(sealed)); err != nil {
		return "", err
	}
	return name, nil
}

// writeFile writes data to path whole or not at all: to a file beside it,
// synced, then renamed over it. A reader never sees half a value, and a
// crash leaves at most a temporary file.
func writeFile(dir, path string, data []byte) error {
	temp, err := os.CreateTemp(dir, ".write-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(temp.Name()) }()
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return err
	}
	if handle, err := os.Open(dir); err == nil {
		_ = handle.Sync()
		_ = handle.Close()
	}
	return nil
}

func (s *store) get(ctx context.Context, instance uuid.UUID, name string) (any, error) {
	sealed, err := os.ReadFile(filepath.Join(s.instanceDir(ctx, instance), name))
	if err != nil {
		return nil, err
	}
	plain, err := crypto.Decrypt(string(sealed))
	if err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal([]byte(plain), &value); err != nil {
		return nil, fmt.Errorf("the stored value is not JSON: %w", err)
	}
	return value, nil
}
