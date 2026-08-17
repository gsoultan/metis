package gorms

import (
	"context"
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/repositories/contracts"
	"github.com/gsoultan/gobpm/server/repositories/models"
	"gorm.io/gorm"
)

type auditRepository struct {
	db *gorm.DB
}

func NewAuditRepository(db *gorm.DB) contracts.AuditRepository {
	return &auditRepository{db: db}
}

func (r *auditRepository) Create(ctx context.Context, entry models.AuditModel) error {
	return GetTx(ctx, r.db).Create(&entry).Error
}

// tableAuditLogs is the SQL table behind AuditModel, needed by name so the
// tenant scope can build its JOIN.
const tableAuditLogs = "audit_logs"

// ListByInstance returns an instance's audit trail, scoped to the caller's
// tenant. Columns are table-qualified because the scope joins projects, which
// carries a created_at of its own.
func (r *auditRepository) ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.AuditModel, error) {
	var modelsList []models.AuditModel
	err := tenantScopeDB(ctx, GetTx(ctx, r.db), tableAuditLogs).
		Where("audit_logs.instance_id = ?", instanceID).
		Order("audit_logs.created_at desc").
		Find(&modelsList).Error
	if err != nil {
		return nil, err
	}
	return modelsList, nil
}

// ListByProject returns a project's audit trail, scoped to the caller's tenant
// so a project ID from another organization returns nothing.
func (r *auditRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.AuditModel, error) {
	var modelsList []models.AuditModel
	err := tenantScopeDB(ctx, GetTx(ctx, r.db), tableAuditLogs).
		Where("audit_logs.project_id = ?", projectID).
		Order("audit_logs.created_at desc").
		Find(&modelsList).Error
	if err != nil {
		return nil, err
	}
	return modelsList, nil
}
