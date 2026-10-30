package archivestore

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/envvar"
)

// EnvDir is the directory records are kept in by the directory store.
const EnvDir = "METIS_ARCHIVE_DIR"

// recordSuffix ends a record's file name.
const recordSuffix = ".record"

// directoryStore keeps each record in a file, under a directory every
// replica sees: a shared volume or a network file system.
type directoryStore struct {
	dir string
	// root is every file operation's way in: a name that would lead out of
	// the directory is refused by it.
	root *os.Root
}

func directoryFromEnv(context.Context) (Store, error) {
	dir := strings.TrimSpace(envvar.Get(EnvDir))
	if dir == "" {
		return nil, nil
	}
	return OpenDirectory(dir)
}

// OpenDirectory opens dir as a store, creating it if it is not there, and
// checks it can be written.
func OpenDirectory(dir string) (Store, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", EnvDir, err)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("%s: could not create %s: %w", EnvDir, abs, err)
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, fmt.Errorf("%s: could not open %s: %w", EnvDir, abs, err)
	}
	probe := ".probe-" + uuid.NewString()
	if err := root.WriteFile(probe, nil, 0o600); err != nil {
		return nil, errors.Join(fmt.Errorf("%s: %s is not writable: %w", EnvDir, abs, err), root.Close())
	}
	if err := root.Remove(probe); err != nil {
		return nil, errors.Join(fmt.Errorf("%s: could not remove a file from %s: %w", EnvDir, abs, err), root.Close())
	}
	return &directoryStore{dir: abs, root: root}, nil
}

func (s *directoryStore) Kind() string  { return KindDirectory }
func (s *directoryStore) Where() string { return s.dir }

// Put writes the record to a file of its own beside it and renames it into
// place, so a reader never sees half of one.
func (s *directoryStore) Put(_ context.Context, name string, record []byte) error {
	file := name + recordSuffix
	if err := s.root.MkdirAll(path.Dir(file), 0o700); err != nil {
		return fmt.Errorf("could not make a place for archive record %s: %w", name, err)
	}
	partial := file + ".partial-" + uuid.NewString()
	if err := s.root.WriteFile(partial, record, 0o600); err != nil {
		return fmt.Errorf("could not write archive record %s: %w", name, err)
	}
	if err := s.root.Rename(partial, file); err != nil {
		return errors.Join(fmt.Errorf("could not write archive record %s: %w", name, err), s.root.Remove(partial))
	}
	return nil
}

func (s *directoryStore) Get(_ context.Context, name string) ([]byte, error) {
	record, err := s.root.ReadFile(name + recordSuffix)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	if err != nil {
		return nil, fmt.Errorf("could not read archive record %s: %w", name, err)
	}
	return record, nil
}

func (s *directoryStore) Delete(_ context.Context, name string) error {
	err := s.root.Remove(name + recordSuffix)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("could not remove archive record %s: %w", name, err)
	}
	return nil
}
