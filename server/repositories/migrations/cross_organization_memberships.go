package migrations

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// deleteCrossOrganizationMemberships removes every group membership whose
// account is not a member of the group's organization — the rule AddMembership
// holds a new one to — and returns what it removed.
const deleteCrossOrganizationMemberships = `
DELETE FROM memberships m
 WHERE NOT EXISTS (
	SELECT 1
	  FROM groups g
	  JOIN user_organizations uo ON uo.organization_id = g.organization_id
	 WHERE g.id = m.group_id
	   AND uo.user_id = m.user_id)
RETURNING m.group_id, m.user_id`

// removedMembership is one membership migration 24 took out.
type removedMembership struct {
	GroupID uuid.UUID `gorm:"column:group_id"`
	UserID  uuid.UUID `gorm:"column:user_id"`
}

// removeCrossOrganizationMemberships is migration 24.
//
// An administrator could put another organization's account into one of their
// groups until AddMembership refused it, and the member list then showed that
// person's username, name and email to everybody in the group's organization.
// The refusal stops new ones; this takes out the ones written before it, which
// RemoveMembership stayed lenient for but which nobody could find to remove.
//
// Every removal is logged by group and account id, so an operator has a record
// of what went and can put back one that was meant — after making the account a
// member of the group's organization, which is what would have made it valid.
func removeCrossOrganizationMemberships(ctx context.Context, db *gorm.DB) error {
	// PostgreSQL only, as 21 to 23: anywhere else the baseline has just built
	// these tables empty.
	if db.Name() != "postgres" {
		return nil
	}
	// A fresh installation runs the schema migrations before storm creates
	// user_organizations — setup does, and so does the first boot — and it
	// has no memberships to check.
	for _, table := range []string{"memberships", "groups", "user_organizations"} {
		if !db.WithContext(ctx).Migrator().HasTable(table) {
			return nil
		}
	}

	var removed []removedMembership
	if err := db.WithContext(ctx).Raw(deleteCrossOrganizationMemberships).Scan(&removed).Error; err != nil {
		return fmt.Errorf("remove the group memberships that cross organizations: %w", err)
	}
	logRemovedMemberships(removed)
	return nil
}

// logRemovedMemberships is the operator's record: a line per removal, then the
// count, which is there even when it is zero so that nothing found reads
// differently from never checked.
func logRemovedMemberships(removed []removedMembership) {
	if len(removed) == 0 {
		log.Info().Int("removed", 0).Msg("No group membership crossed organizations; none was removed")
		return
	}
	for _, membership := range removed {
		log.Warn().
			Str("group_id", membership.GroupID.String()).
			Str("account_id", membership.UserID.String()).
			Msg("Removed a group membership whose account is not a member of the group's organization")
	}
	log.Warn().Int("removed", len(removed)).
		Msg("Removed the group memberships that crossed organizations; each is logged above by group and account")
}
