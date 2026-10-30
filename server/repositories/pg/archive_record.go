package pg

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/archivedinstance"
	"github.com/gsoultan/metis/server/repositories/store/auditentry"
	"github.com/gsoultan/metis/server/repositories/store/incident"
	"github.com/gsoultan/metis/server/repositories/store/notification"
	"github.com/gsoultan/metis/server/repositories/store/processdefinition"
	"github.com/gsoultan/metis/server/repositories/store/processinstance"
	"github.com/gsoultan/metis/server/repositories/store/servicecall"
	"github.com/gsoultan/metis/server/repositories/store/task"
	"github.com/gsoultan/metis/server/repositories/store/variablesnapshot"
	"github.com/gsoultan/storm/runtime"
)

// maxArchiveRecordBytes bounds how far an archived record is inflated when it
// is opened. A record is written by this server, so this is not a defence
// against a stranger; it keeps a corrupt row from inflating without end.
const maxArchiveRecordBytes = 512 << 20

// recordInDatabase is the RecordStore of a record kept in its own row.
const recordInDatabase = "database"

// archiveInstances writes the archive record of each of ids, then removes
// the audit entries the records now hold that name nobody: the engine's own
// account of the run. Entries that name somebody who acted stay in the live
// trail too, so the audit log of who did what is not cut short by the
// archive. Run it in the transaction that forgets the instances, so an
// instance is archived and forgotten together or not at all.
//
// An instance marked removed is not archived: somebody removed it, and the
// archive is no road back to it. It is forgotten as before.
func archiveInstances(ctx context.Context, ex runtime.Executor, ids [][16]byte, now time.Time) error {
	instances, err := processinstance.New().
		Where(processinstance.ID.In(ids...)).
		Limit(int64(len(ids))).
		All(ctx, ex, nil)
	if err != nil {
		return fmt.Errorf("could not read the instances to archive: %w", err)
	}
	if len(instances) == 0 {
		return nil
	}
	records, err := gatherRecords(ctx, ex, instances)
	if err != nil {
		return err
	}
	rows, err := archiveRows(ctx, ex, instances, records, now)
	if err != nil {
		return err
	}
	if _, err := archivedinstance.InsertAll(ctx, ex, rows); err != nil {
		return fmt.Errorf("could not write the archive records: %w", err)
	}
	archived := make([][16]byte, 0, len(instances))
	for _, row := range instances {
		archived = append(archived, row.ID)
	}
	if _, err := auditentry.New().
		Where(auditentry.InstanceID.In(archived...), auditentry.Actor.IsNull()).
		HardDeleteAll(ctx, ex); err != nil {
		return fmt.Errorf("could not move the archived instances' audit entries: %w", err)
	}
	return nil
}

// archiveRows is one archived_instances row per instance, its record sealed.
func archiveRows(ctx context.Context, ex runtime.Executor, instances []processinstance.Row,
	records map[[16]byte]*models.ArchiveRecord, now time.Time) ([]archivedinstance.Row, error) {
	labels, err := definitionLabels(ctx, ex, instances)
	if err != nil {
		return nil, err
	}
	rows := make([]archivedinstance.Row, 0, len(instances))
	for _, instance := range instances {
		sealed, err := sealRecord(records[instance.ID])
		if err != nil {
			return nil, fmt.Errorf("could not seal the archive record of instance %x: %w", instance.ID, err)
		}
		label := labels[instance.DefinitionID]
		if label.Version < 0 || label.Version > math.MaxInt32 {
			return nil, fmt.Errorf("instance %x runs on version %d, which the archive cannot record", instance.ID, label.Version)
		}
		rows = append(rows, archivedinstance.Row{
			ID:             instance.ID,
			CreatedAt:      now,
			UpdatedAt:      now,
			ProjectID:      instance.ProjectID,
			DefinitionID:   instance.DefinitionID,
			ProcessKey:     label.Key,
			ProcessName:    label.Name,
			ProcessVersion: int32(label.Version),
			BusinessKey:    instance.BusinessKey,
			Status:         instance.Status,
			StartedAt:      instance.CreatedAt,
			FinishedAt:     instance.UpdatedAt,
			RecordStore:    recordInDatabase,
			Record:         sealed,
		})
	}
	return rows, nil
}

// definitionLabels is what each instance's version is called. A version
// marked removed still names its instances.
func definitionLabels(ctx context.Context, ex runtime.Executor, instances []processinstance.Row) (map[[16]byte]processdefinition.LabelRow, error) {
	seen := make(map[[16]byte]bool, len(instances))
	var ids [][16]byte
	for _, row := range instances {
		if !seen[row.DefinitionID] {
			seen[row.DefinitionID] = true
			ids = append(ids, row.DefinitionID)
		}
	}
	rows, err := processdefinition.New().WithDeleted().
		Where(processdefinition.ID.In(ids...)).
		Limit(int64(len(ids))).
		AllLabel(ctx, ex)
	if err != nil {
		return nil, fmt.Errorf("could not read the archived instances' versions: %w", err)
	}
	labels := make(map[[16]byte]processdefinition.LabelRow, len(rows))
	for _, row := range rows {
		labels[row.ID] = row
	}
	return labels, nil
}

// gatherRecords reads everything the instances own, a statement per kind for
// the whole batch, and files each row under its instance.
func gatherRecords(ctx context.Context, ex runtime.Executor, instances []processinstance.Row) (map[[16]byte]*models.ArchiveRecord, error) {
	records := make(map[[16]byte]*models.ArchiveRecord, len(instances))
	ids := make([][16]byte, 0, len(instances))
	for _, row := range instances {
		instance, err := instanceFrom(ctx, row)
		if err != nil {
			return nil, err
		}
		records[row.ID] = &models.ArchiveRecord{Version: models.ArchiveRecordVersion, Instance: instance}
		ids = append(ids, row.ID)
	}
	for _, gather := range []func(context.Context, runtime.Executor, [][16]byte, map[[16]byte]*models.ArchiveRecord) error{
		gatherTasks, gatherServiceCalls, gatherIncidents, gatherNotifications, gatherVariableHistory, gatherAuditTrail,
	} {
		if err := gather(ctx, ex, ids, records); err != nil {
			return nil, err
		}
	}
	return records, nil
}

func gatherTasks(ctx context.Context, ex runtime.Executor, ids [][16]byte, records map[[16]byte]*models.ArchiveRecord) error {
	rows, err := everyRow[task.Row](ctx, ex, task.New().Where(task.InstanceID.In(ids...)))
	if err != nil {
		return fmt.Errorf("could not read the archived instances' tasks: %w", err)
	}
	for _, row := range rows {
		t, err := taskFrom(row)
		if err != nil {
			return err
		}
		r := records[row.InstanceID]
		r.Tasks = append(r.Tasks, t)
	}
	return nil
}

func gatherServiceCalls(ctx context.Context, ex runtime.Executor, ids [][16]byte, records map[[16]byte]*models.ArchiveRecord) error {
	rows, err := everyRow[servicecall.Row](ctx, ex, servicecall.New().Where(servicecall.InstanceID.In(ids...)))
	if err != nil {
		return fmt.Errorf("could not read the archived instances' service calls: %w", err)
	}
	for _, row := range rows {
		call, err := serviceCallFrom(row)
		if err != nil {
			return err
		}
		r := records[row.InstanceID]
		r.ServiceCalls = append(r.ServiceCalls, call)
	}
	return nil
}

func gatherIncidents(ctx context.Context, ex runtime.Executor, ids [][16]byte, records map[[16]byte]*models.ArchiveRecord) error {
	rows, err := everyRow[incident.Row](ctx, ex, incident.New().Where(incident.InstanceID.In(ids...)))
	if err != nil {
		return fmt.Errorf("could not read the archived instances' incidents: %w", err)
	}
	for _, row := range rows {
		r := records[row.InstanceID]
		r.Incidents = append(r.Incidents, incidentFrom(row))
	}
	return nil
}

func gatherNotifications(ctx context.Context, ex runtime.Executor, ids [][16]byte, records map[[16]byte]*models.ArchiveRecord) error {
	rows, err := everyRow[notification.Row](ctx, ex, notification.New().Where(notification.InstanceID.In(ids...)))
	if err != nil {
		return fmt.Errorf("could not read the archived instances' notifications: %w", err)
	}
	for _, row := range rows {
		r := records[row.InstanceID.V]
		r.Notifications = append(r.Notifications, notificationFrom(row))
	}
	return nil
}

func gatherVariableHistory(ctx context.Context, ex runtime.Executor, ids [][16]byte, records map[[16]byte]*models.ArchiveRecord) error {
	rows, err := everyRow[variablesnapshot.Row](ctx, ex, variablesnapshot.New().Where(variablesnapshot.InstanceID.In(ids...)))
	if err != nil {
		return fmt.Errorf("could not read the archived instances' variable history: %w", err)
	}
	for _, row := range rows {
		snapshot, err := snapshotFrom(ctx, row)
		if err != nil {
			return err
		}
		r := records[row.InstanceID]
		r.VariableHistory = append(r.VariableHistory, snapshot)
	}
	return nil
}

// gatherAuditTrail reads the trails in the order the instance's own page
// reads one: by created_at and then seq. One statement, as the audit
// repository reads a trail, because that order has ties a keyset walk cannot
// page through.
func gatherAuditTrail(ctx context.Context, ex runtime.Executor, ids [][16]byte, records map[[16]byte]*models.ArchiveRecord) error {
	rows, err := auditentry.New().
		Where(auditentry.InstanceID.In(ids...)).
		Order(oldestFirst()...).
		Limit(allRows).
		All(ctx, ex, nil)
	if err != nil {
		return fmt.Errorf("could not read the archived instances' audit trails: %w", err)
	}
	for _, row := range rows {
		entry, err := auditFrom(row)
		if err != nil {
			return err
		}
		r := records[row.InstanceID]
		r.AuditTrail = append(r.AuditTrail, entry)
	}
	return nil
}

// sealRecord encodes a record, compresses it, and seals it: the sealed text
// cannot be compressed by the database, and a record is mostly repeated
// names and the same variables snapshot after snapshot.
func sealRecord(record *models.ArchiveRecord) (string, error) {
	plain, err := json.Marshal(record)
	if err != nil {
		return "", fmt.Errorf("encode: %w", err)
	}
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write(plain); err != nil {
		return "", fmt.Errorf("compress: %w", err)
	}
	if err := zw.Close(); err != nil {
		return "", fmt.Errorf("compress: %w", err)
	}
	return sealedText(compressed.String())
}

// openRecord reverses sealRecord.
func openRecord(sealed string) (models.ArchiveRecord, error) {
	compressed, err := openedText(sealed)
	if err != nil {
		return models.ArchiveRecord{}, err
	}
	zr, err := gzip.NewReader(bytes.NewReader([]byte(compressed)))
	if err != nil {
		return models.ArchiveRecord{}, fmt.Errorf("decompress: %w", err)
	}
	plain, err := io.ReadAll(io.LimitReader(zr, maxArchiveRecordBytes+1))
	if err != nil {
		return models.ArchiveRecord{}, fmt.Errorf("decompress: %w", err)
	}
	if len(plain) > maxArchiveRecordBytes {
		return models.ArchiveRecord{}, fmt.Errorf("the record inflates past %d MiB", maxArchiveRecordBytes>>20)
	}
	var record models.ArchiveRecord
	if err := json.Unmarshal(plain, &record); err != nil {
		return models.ArchiveRecord{}, fmt.Errorf("decode: %w", err)
	}
	if record.Version != models.ArchiveRecordVersion {
		return models.ArchiveRecord{}, fmt.Errorf("the record is of shape %d, and this release reads shape %d", record.Version, models.ArchiveRecordVersion)
	}
	return record, nil
}
