package impl

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic"
	"github.com/gsoultan/metis/server/repositories/models"
)

// maxDecisionTablesPerPlan is how many decision tables one plan reads. Which
// decisions a process consults, and which those require, is its author's to
// write, so the reading has to end somewhere whatever was written. Past it a
// decision is reported unread, which a plan warns of.
const maxDecisionTablesPerPlan = 64

// decisionLookup says what the decisions a process consults read, for one
// plan: its reads method is the decisionReads the scan of the definition asks.
//
// Each table is read once however many steps consult it, and no more than
// maxDecisionTablesPerPlan are read in all.
type decisionLookup struct {
	// load reads one version of a decision — the one in force for version 0 —
	// and reports whether there is one.
	load func(key string, version int) (table entities.DecisionDefinition, found bool, err error)
	// read is what each table read so far reads, by key and version.
	read map[[2]string]decisionTableRead
	// left is how many more tables this plan may read, and refused how many
	// it was asked for after that. pastBound is the decisions whose answer
	// was cut short for it, by key and version.
	left      int
	refused   int
	pastBound map[[2]string]struct{}
	// err is the first failure to read a table. The answers given after it
	// are not to be used: a store that failed is not a decision nobody stored.
	err error
}

// decisionTableRead is what one table reads by itself, before the decisions
// it requires are followed.
type decisionTableRead struct {
	names, requires []string
	// analysable is false when part of the table could not be read, and when
	// the table was not read at all because the plan had read its fill.
	analysable bool
	// found is false when there is no such decision, or no such version.
	found bool
}

func newDecisionLookup(load func(key string, version int) (entities.DecisionDefinition, bool, error)) *decisionLookup {
	return &decisionLookup{load: load, read: map[[2]string]decisionTableRead{}, left: maxDecisionTablesPerPlan,
		pastBound: map[[2]string]struct{}{}}
}

// decisionLookup reads decisions from the store, in the project an instance
// belongs to: the version a step pins, or the one in force, as
// decisionService.evaluateRecursive resolves them.
func (s *instanceDeviationService) decisionLookup(ctx context.Context, projectID uuid.UUID) *decisionLookup {
	return newDecisionLookup(func(key string, version int) (entities.DecisionDefinition, bool, error) {
		var stored models.DecisionDefinitionModel
		var err error
		if version > 0 {
			stored, err = s.repo.Decision().GetByKeyAndVersion(ctx, projectID, key, version)
		} else {
			stored, err = s.repo.Decision().GetLiveByKey(ctx, projectID, key)
		}
		if errors.Is(err, apierr.ErrNotFound) {
			return entities.DecisionDefinition{}, false, nil
		}
		if err != nil {
			return entities.DecisionDefinition{}, false, fmt.Errorf("reading the decision %q a step consults: %w", key, err)
		}
		return adapters.DecisionEntityAdapter{Model: stored}.ToEntity(), true, nil
	})
}

// reads is what one version of a decision reads from the variables it is
// evaluated with: its own columns and cells, and those of every decision it
// requires. found is false when there is no such decision or version.
//
// The answer is not vouched for (analysable false) when any table on the way
// could not be read in full. The names that could be read are returned all
// the same.
//
// It walks the requirements each time it is asked. The scan of a definition
// asks once for each decision and version, however many steps consult it
// (decisionScan.decision), and a plan makes one scan: that is the one place
// that keeps this to a walk per decision per plan.
func (l *decisionLookup) reads(key string, version int) (names []string, analysable, found bool) {
	refusedBefore := l.refused
	defer func() {
		if l.refused > refusedBefore {
			l.pastBound[[2]string{key, strconv.Itoa(version)}] = struct{}{}
		}
	}()
	root := l.table(key, version)
	if !root.found {
		return nil, false, false
	}
	read := map[string]struct{}{}
	addNames(read, root.names)
	analysable = l.addRequired(key, root.requires, read) && root.analysable
	if len(read) == 0 {
		return nil, analysable, true
	}
	return sortedKeys(read), analysable, true
}

// addRequired adds to read what the decisions a decision requires read, and
// what those require in turn, and reports whether all of it could be told.
//
// Each is read at the version in force, which is how the engine evaluates a
// requirement whatever version of the first decision is pinned. It could not
// be told when a required decision is not there or cannot be read in full,
// and when the requirements come back to a decision still being read: the
// engine refuses to evaluate such a ring, so nothing can be said of what it
// would have read.
//
// The requirements are followed with a list of what is still to read and a
// record of what has been, not by recursion, and stop at the plan's bound.
func (l *decisionLookup) addRequired(key string, requires []string, read map[string]struct{}) (analysable bool) {
	const reading, done = 1, 2
	type step struct {
		key      string
		requires []string
	}
	analysable = true
	state := map[string]int{key: reading}
	path := []step{{key, requires}}
	for len(path) > 0 {
		at := &path[len(path)-1]
		if len(at.requires) == 0 {
			state[at.key] = done
			path = path[:len(path)-1]
			continue
		}
		required := at.requires[0]
		at.requires = at.requires[1:]
		switch state[required] {
		case reading:
			analysable = false
			continue
		case done:
			continue
		}
		table := l.table(required, 0)
		addNames(read, table.names)
		analysable = analysable && table.found && table.analysable
		state[required] = reading
		path = append(path, step{required, table.requires})
	}
	return analysable
}

// addNames adds names to a set of them.
func addNames(set map[string]struct{}, names []string) {
	for _, name := range names {
		set[name] = struct{}{}
	}
}

// leftUnread reports whether a decision's answer was cut short because the
// plan had read its fill of tables: it, or one it requires, was not read.
func (l *decisionLookup) leftUnread(key string, version int) bool {
	_, cut := l.pastBound[[2]string{key, strconv.Itoa(version)}]
	return cut
}

// table is what one version of one decision reads by itself, read from the
// store the first time it is asked for. Once the plan has read its fill a
// table not yet read is answered as there and unread: whether it exists is
// not known, and "not known" must not read as "not there".
func (l *decisionLookup) table(key string, version int) decisionTableRead {
	ref := [2]string{key, strconv.Itoa(version)}
	if known, isKnown := l.read[ref]; isKnown {
		return known
	}
	if l.left == 0 {
		l.refused++
		return decisionTableRead{found: true}
	}
	l.left--
	var read decisionTableRead
	stored, found, err := l.load(key, version)
	switch {
	case err != nil:
		if l.err == nil {
			l.err = err
		}
		read = decisionTableRead{found: true}
	case found:
		read.found = true
		read.names, read.requires, read.analysable = logic.DecisionTableReads(stored)
	}
	l.read[ref] = read
	return read
}
