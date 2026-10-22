package payloads

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/crypto"
)

// ResealCount is what a reseal of the directory found.
type ResealCount struct {
	Moved, Current, Unreadable int
}

// Reseal seals every kept value again under the current key when only a
// previous key opens it, as the reseal of a column does. With check set it
// changes nothing and counts what would move. A value's name stays: the rows
// that refer to it name it, and it is still the same value.
func Reseal(check bool) (ResealCount, error) {
	var count ResealCount
	s := current()
	if s == nil {
		return count, nil
	}
	err := fs.WalkDir(s.root.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !validName(entry.Name()) {
			return nil
		}
		sealed, err := s.root.ReadFile(path)
		if err != nil {
			return err
		}
		resealed, moved, err := crypto.Reseal(string(sealed))
		switch {
		case errors.Is(err, crypto.ErrUnreadable):
			count.Unreadable++
			return nil
		case err != nil:
			return err
		case !moved:
			count.Current++
			return nil
		case check:
			count.Moved++
			return nil
		}
		if err := s.writeFile(filepath.Dir(path), path, []byte(resealed)); err != nil {
			return err
		}
		count.Moved++
		return nil
	})
	return count, err
}

// Existing answers which of ids still have an instance row.
type Existing func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error)

// sweepBatch is how many instance directories are checked against the
// database at a time.
const sweepBatch = 500

// SweepGone removes the directories of the instances of the database ctx is
// bound to that no longer have a row: finished instances a retention period
// removed, and instances whose creation was rolled back after a value was
// kept. A directory changed after notBefore is left, whatever the database
// says: its instance may be in a transaction that has not committed yet.
//
// An instance's directory goes with it and not before. A value a step
// replaced stays while the instance does, because the instance's snapshots
// may still refer to it.
func SweepGone(ctx context.Context, notBefore time.Time, existing Existing) (int, error) {
	s := current()
	if s == nil {
		return 0, nil
	}
	root := partition(ctx)
	entries, err := fs.ReadDir(s.root.FS(), root)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	removed := 0
	var batch []uuid.UUID
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		found, err := existing(ctx, batch)
		if err != nil {
			return err
		}
		for _, id := range batch {
			if found[id] {
				continue
			}
			if err := s.root.RemoveAll(filepath.Join(root, id.String())); err != nil {
				return err
			}
			removed++
		}
		batch = batch[:0]
		return nil
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return removed, ctx.Err()
		}
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		id, err := uuid.Parse(entry.Name())
		if err != nil {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(notBefore) {
			continue
		}
		batch = append(batch, id)
		if len(batch) == sweepBatch {
			if err := flush(); err != nil {
				return removed, err
			}
		}
	}
	return removed, flush()
}
