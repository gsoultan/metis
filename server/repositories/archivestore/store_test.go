package archivestore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Each store keeps a record by name, reads it back whole, answers a name it
// does not have as not found, and removes a record, twice without failing.
func TestEveryStoreKeepsReadsAndRemovesARecord(t *testing.T) {
	directory, err := OpenDirectory(t.TempDir())
	if err != nil {
		t.Fatalf("open the directory store: %v", err)
	}
	bucket := newFakeBucket(t)
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	s3, err := OpenS3(t.Context(), S3Options{Bucket: "archive", Prefix: "metis", Region: "us-east-1", Endpoint: bucket.URL, PathStyle: true})
	if err != nil {
		t.Fatalf("open the S3 store: %v", err)
	}
	for _, store := range []Store{directory, s3} {
		t.Run(store.Kind(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			const name = "project/instance-attempt"
			record := []byte("sealed record")
			if err := store.Put(ctx, name, record); err != nil {
				t.Fatalf("put: %v", err)
			}
			got, err := store.Get(ctx, name)
			if err != nil || !bytes.Equal(got, record) {
				t.Fatalf("get = %q, %v; want %q", got, err, record)
			}
			if _, err := store.Get(ctx, "project/never-written"); !errors.Is(err, ErrNotFound) {
				t.Errorf("a record never written reads with %v, want ErrNotFound", err)
			}
			for range 2 {
				if err := store.Delete(ctx, name); err != nil {
					t.Fatalf("delete: %v", err)
				}
			}
			if _, err := store.Get(ctx, name); !errors.Is(err, ErrNotFound) {
				t.Errorf("a removed record reads with %v, want ErrNotFound", err)
			}
		})
	}
}

// A name that would lead out of the directory is refused, not followed.
func TestTheDirectoryStoreStaysInsideItsDirectory(t *testing.T) {
	store, err := OpenDirectory(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := store.Put(t.Context(), "../escaped", []byte("x")); err == nil {
		t.Fatal("a name leading out of the directory was written")
	}
}

// METIS_ARCHIVE_STORE names a store whose settings are set, or the start
// stops; a store whose settings are set stays readable when another is
// chosen.
func TestTheChosenStoreMustBeConfigured(t *testing.T) {
	t.Cleanup(ResetForTest)
	t.Setenv(EnvStore, KindDirectory)
	t.Setenv(EnvDir, "")
	t.Setenv(EnvS3Bucket, "")
	if err := ConfigureFromEnv(t.Context()); err == nil {
		t.Fatal("a directory store with no directory was accepted")
	}
	t.Setenv(EnvStore, "tape")
	if err := ConfigureFromEnv(t.Context()); err == nil {
		t.Fatal("a store that does not exist was accepted")
	}
	t.Setenv(EnvStore, KindDatabase)
	t.Setenv(EnvDir, t.TempDir())
	if err := ConfigureFromEnv(t.Context()); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if Writer() != nil {
		t.Error("records go outside the database when the database was chosen")
	}
	if _, ok := Reader(KindDirectory); !ok {
		t.Error("the directory whose setting is set cannot be read")
	}
}

// fakeBucket answers PUT, GET and DELETE on path-style object keys, as an
// S3-compatible service does.
type fakeBucket struct {
	*httptest.Server
	mu      sync.Mutex
	objects map[string][]byte
}

func newFakeBucket(t *testing.T) *fakeBucket {
	t.Helper()
	b := &fakeBucket{objects: map[string][]byte{}}
	b.Server = httptest.NewServer(http.HandlerFunc(b.serve))
	t.Cleanup(b.Close)
	return b
}

func (b *fakeBucket) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
		http.Error(w, "unsigned", http.StatusForbidden)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	key := r.URL.Path
	switch r.Method {
	case http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		b.objects[key] = body
	case http.MethodGet:
		body, ok := b.objects[key]
		if !ok {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchKey</Code><Message>none</Message></Error>`)
			return
		}
		_, _ = w.Write(body)
	case http.MethodDelete:
		delete(b.objects, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "unsupported", http.StatusMethodNotAllowed)
	}
}
