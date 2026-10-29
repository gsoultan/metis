package pg

import (
	"context"
	"fmt"
	"time"

	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/compensatableactivity"
	"github.com/gsoultan/metis/server/repositories/store/deviationrequest"
	"github.com/gsoultan/metis/server/repositories/store/externaltask"
	"github.com/gsoultan/metis/server/repositories/store/incident"
	"github.com/gsoultan/metis/server/repositories/store/instancedeviation"
	"github.com/gsoultan/metis/server/repositories/store/job"
	"github.com/gsoultan/metis/server/repositories/store/notification"
	"github.com/gsoultan/metis/server/repositories/store/processdefinition"
	"github.com/gsoultan/metis/server/repositories/store/processinstance"
	"github.com/gsoultan/metis/server/repositories/store/retentionhold"
	"github.com/gsoultan/metis/server/repositories/store/servicecall"
	"github.com/gsoultan/metis/server/repositories/store/subscription"
	"github.com/gsoultan/metis/server/repositories/store/task"
	"github.com/gsoultan/metis/server/repositories/store/variablesnapshot"
	"github.com/gsoultan/storm/runtime"
)

// forgetBatch is one batch of the finished-instance sweep.
//
// An instance qualifies when it completed or was cancelled longer ago than
// its period, and nothing still needs it: no deviation was recorded or asked
// for on it (those are compliance records, kept with what they are about), no
// incident on it is open, its parent has finished too, and so have its
// children, and no retention hold keeps it.
//
// Its period is the one an administrator last gave its process (by key, for
// every version), and the installation's where the process has none, or was
// returned to the installation's. An instance whose process has no period
// while the installation has none either is kept. earliest is the earliest
// any period ends, so the scan starts from the instances that could qualify
// at all.
//
// Removed rows count as the sweep found them: a marked instance is forgotten
// like a live one, and a marked incident, child or version is still read.
//
// The audit trail is not touched: it is the record of who did what, and is
// kept for as long as the installation is.
type forgetBatch struct {
	finished []string
	earliest time.Time
	now      time.Time
	fallback int64 // days; zero when the installation keeps instances for ever
	size     int
}

// doomed is the batch's instances, locked: at most size of them, skipping any
// another transaction holds. Run it in the transaction that removes them,
// with the holds and periods locked.
func (b forgetBatch) doomed(ctx context.Context, ex runtime.Executor) ([][16]byte, error) {
	held, err := heldCases(ctx, ex)
	if err != nil {
		return nil, err
	}
	expired, err := b.expired(ctx, ex)
	if err != nil || len(expired) == 0 {
		return nil, err
	}
	// WhereIf: an empty list binds as null, and nothing is outside null.
	candidates := processinstance.New().WithDeleted().
		Where(processinstance.Status.In(b.finished...), processinstance.UpdatedAt.Lt(b.earliest)).
		WhereIf(len(held) > 0, processinstance.ID.NotIn(held...)).
		AnyOf(expired...).
		Order(processinstance.ID.Asc()).
		Limit(int64(b.size))
	var doomed [][16]byte
	for offset := int64(0); len(doomed) < b.size; offset += int64(b.size) {
		page, err := candidates.Offset(offset).AllLineage(ctx, ex)
		if err != nil {
			return nil, fmt.Errorf("could not read the finished instances: %w", err)
		}
		needed, err := b.stillNeeded(ctx, ex, page)
		if err != nil {
			return nil, err
		}
		for _, row := range page {
			if !needed[row.ID] && len(doomed) < b.size {
				doomed = append(doomed, row.ID)
			}
		}
		if len(page) < b.size {
			break
		}
	}
	if len(doomed) == 0 {
		return nil, nil
	}
	locked, err := processinstance.New().WithDeleted().
		Where(processinstance.ID.In(doomed...)).
		Limit(int64(len(doomed))).
		ForUpdateSkipLocked().
		AllRef(ctx, ex)
	if err != nil {
		return nil, fmt.Errorf("could not lock the finished instances: %w", err)
	}
	doomed = doomed[:0]
	for _, row := range locked {
		doomed = append(doomed, row.ID)
	}
	return doomed, nil
}

// expired is one group per period in force: the versions of the processes
// with that period, finished before it ends, and, when the installation has a
// period, every other version finished before that one ends.
func (b forgetBatch) expired(ctx context.Context, ex runtime.Executor) ([]processinstance.Grp, error) {
	periods, err := newestPeriods(ctx, ex)
	if err != nil {
		return nil, err
	}
	byDays := map[int32][][16]byte{}
	var own [][16]byte
	if len(periods) > 0 {
		projects := make([][16]byte, 0, len(periods))
		keys := make([]string, 0, len(periods))
		for p, days := range periods {
			if days.Valid {
				projects = append(projects, p.project)
				keys = append(keys, p.key)
			}
		}
		if len(keys) > 0 {
			versions, err := everyGroup(func(offset int64) ([]processdefinition.VersionRow, error) {
				return processdefinition.New().WithDeleted().
					Where(processdefinition.ProjectID.In(projects...), processdefinition.Key.In(keys...)).
					Order(processdefinition.ID.Asc()).
					Limit(everyRowBatch).Offset(offset).
					AllVersion(ctx, ex)
			})
			if err != nil {
				return nil, fmt.Errorf("could not read the versions with a retention period: %w", err)
			}
			for _, v := range versions {
				days := periods[processKey{v.ProjectID, v.Key}]
				if days.Valid {
					byDays[days.V] = append(byDays[days.V], v.ID)
					own = append(own, v.ID)
				}
			}
		}
	}
	groups := make([]processinstance.Grp, 0, len(byDays)+1)
	for days, versions := range byDays {
		groups = append(groups, processinstance.And(
			processinstance.DefinitionID.In(versions...),
			processinstance.UpdatedAt.Lt(b.now.AddDate(0, 0, -int(days)))))
	}
	if b.fallback > 0 {
		before := processinstance.UpdatedAt.Lt(b.now.AddDate(0, 0, -int(b.fallback)))
		if len(own) == 0 {
			groups = append(groups, processinstance.And(before))
		} else {
			groups = append(groups, processinstance.And(processinstance.DefinitionID.NotIn(own...), before))
		}
	}
	return groups, nil
}

// stillNeeded is which of page's instances something keeps: a deviation
// recorded or asked for, an open incident, a child or a parent that has not
// finished.
func (b forgetBatch) stillNeeded(ctx context.Context, ex runtime.Executor, page []processinstance.LineageRow) (map[[16]byte]bool, error) {
	needed := make(map[[16]byte]bool)
	if len(page) == 0 {
		return needed, nil
	}
	ids := make([][16]byte, 0, len(page))
	var parents [][16]byte
	for _, row := range page {
		ids = append(ids, row.ID)
		if row.ParentInstanceID.Valid {
			parents = append(parents, row.ParentInstanceID.V)
		}
	}
	limit := int64(len(ids))
	deviations, err := instancedeviation.New().
		Where(instancedeviation.InstanceID.In(ids...)).Limit(limit).
		AllByInstance(ctx, ex)
	if err != nil {
		return nil, fmt.Errorf("could not read the instances' deviations: %w", err)
	}
	for _, row := range deviations {
		needed[row.InstanceID] = true
	}
	requests, err := deviationrequest.New().
		Where(deviationrequest.InstanceID.In(ids...)).Limit(limit).
		AllByInstance(ctx, ex)
	if err != nil {
		return nil, fmt.Errorf("could not read the instances' deviation requests: %w", err)
	}
	for _, row := range requests {
		needed[row.InstanceID.V] = true
	}
	incidents, err := incident.New().
		Where(incident.InstanceID.In(ids...), incident.Status.NotEq(string(models.IncidentResolved))).Limit(limit).
		AllAnyByInstance(ctx, ex)
	if err != nil {
		return nil, fmt.Errorf("could not read the instances' open incidents: %w", err)
	}
	for _, row := range incidents {
		needed[row.InstanceID] = true
	}
	children, err := processinstance.New().
		Where(processinstance.ParentInstanceID.In(ids...), processinstance.Status.NotIn(b.finished...)).Limit(limit).
		AllChildrenByParent(ctx, ex)
	if err != nil {
		return nil, fmt.Errorf("could not read the instances' running children: %w", err)
	}
	for _, row := range children {
		needed[row.ParentInstanceID.V] = true
	}
	if len(parents) > 0 {
		running, err := processinstance.New().WithDeleted().
			Where(processinstance.ID.In(parents...), processinstance.Status.NotIn(b.finished...)).
			Limit(int64(len(parents))).
			AllRef(ctx, ex)
		if err != nil {
			return nil, fmt.Errorf("could not read the instances' parents: %w", err)
		}
		runningParent := make(map[[16]byte]bool, len(running))
		for _, row := range running {
			runningParent[row.ID] = true
		}
		for _, row := range page {
			if row.ParentInstanceID.Valid && runningParent[row.ParentInstanceID.V] {
				needed[row.ID] = true
			}
		}
	}
	return needed, nil
}

// heldCases is every instance a retention hold keeps: the whole case the
// held instance belongs to, which is the instance that started the chain of
// calls it is part of and every instance started from that one, at any depth.
// Somebody reading a held case finds the instance it was called from and the
// ones it called. The walk up to the first instance and back down starts from
// the holds, which are few, and not from the instances.
func heldCases(ctx context.Context, ex runtime.Executor) ([][16]byte, error) {
	holds, err := everyRow[retentionhold.Row](ctx, ex, retentionhold.New())
	if err != nil {
		return nil, fmt.Errorf("could not read the retention holds: %w", err)
	}
	seen := make(map[[16]byte]bool, len(holds))
	var held, frontier [][16]byte
	for _, h := range holds {
		if !seen[h.InstanceID] {
			seen[h.InstanceID] = true
			held = append(held, h.InstanceID)
			frontier = append(frontier, h.InstanceID)
		}
	}
	// Up: the instance each one was called from, to the first of the chain.
	for len(frontier) > 0 {
		rows, err := processinstance.New().WithDeleted().
			Where(processinstance.ID.In(frontier...), processinstance.ParentInstanceID.IsNotNull()).
			Limit(int64(len(frontier))).
			AllLineage(ctx, ex)
		if err != nil {
			return nil, fmt.Errorf("could not walk up the held cases: %w", err)
		}
		frontier = frontier[:0]
		for _, row := range rows {
			if parent := row.ParentInstanceID.V; !seen[parent] {
				seen[parent] = true
				held = append(held, parent)
				frontier = append(frontier, parent)
			}
		}
	}
	// Down: every instance started from one of them, at any depth.
	frontier = append(frontier, held...)
	for len(frontier) > 0 {
		parents := frontier
		frontier = nil
		children, err := everyGroup(func(offset int64) ([]processinstance.LineageRow, error) {
			return processinstance.New().WithDeleted().
				Where(processinstance.ParentInstanceID.In(parents...)).
				Order(processinstance.ID.Asc()).
				Limit(everyRowBatch).Offset(offset).
				AllLineage(ctx, ex)
		})
		if err != nil {
			return nil, fmt.Errorf("could not walk down the held cases: %w", err)
		}
		for _, row := range children {
			if !seen[row.ID] {
				seen[row.ID] = true
				held = append(held, row.ID)
				frontier = append(frontier, row.ID)
			}
		}
	}
	return held, nil
}

// forgetInstances removes the instances and the rows that exist only to run
// them, removed or not. Run it in the transaction that locked them, so a
// batch goes whole or not at all.
func forgetInstances(ctx context.Context, ex runtime.Executor, ids [][16]byte) (int64, error) {
	for _, step := range []struct {
		what   string
		delete func() (int64, error)
	}{
		{"tasks", func() (int64, error) {
			return task.New().Where(task.InstanceID.In(ids...)).HardDeleteAll(ctx, ex)
		}},
		{"jobs", func() (int64, error) {
			return job.New().Where(job.InstanceID.In(ids...)).HardDeleteAll(ctx, ex)
		}},
		{"external tasks", func() (int64, error) {
			return externaltask.New().Where(externaltask.InstanceID.In(ids...)).HardDeleteAll(ctx, ex)
		}},
		{"service calls", func() (int64, error) {
			return servicecall.New().Where(servicecall.InstanceID.In(ids...)).HardDeleteAll(ctx, ex)
		}},
		{"incidents", func() (int64, error) {
			return incident.New().Where(incident.InstanceID.In(ids...)).HardDeleteAll(ctx, ex)
		}},
		{"subscriptions", func() (int64, error) {
			return subscription.New().Where(subscription.InstanceID.In(ids...)).HardDeleteAll(ctx, ex)
		}},
		{"compensation records", func() (int64, error) {
			return compensatableactivity.New().Where(compensatableactivity.InstanceID.In(ids...)).HardDeleteAll(ctx, ex)
		}},
		{"variable snapshots", func() (int64, error) {
			return variablesnapshot.New().Where(variablesnapshot.InstanceID.In(ids...)).HardDeleteAll(ctx, ex)
		}},
		{"notifications", func() (int64, error) {
			return notification.New().Where(notification.InstanceID.In(ids...)).HardDeleteAll(ctx, ex)
		}},
	} {
		if _, err := step.delete(); err != nil {
			return 0, fmt.Errorf("could not forget the instances' %s: %w", step.what, err)
		}
	}
	removed, err := processinstance.New().Where(processinstance.ID.In(ids...)).HardDeleteAll(ctx, ex)
	if err != nil {
		return 0, fmt.Errorf("could not forget the instances: %w", err)
	}
	return removed, nil
}
