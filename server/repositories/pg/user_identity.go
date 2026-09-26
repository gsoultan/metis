package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/organization"
	"github.com/gsoultan/metis/server/repositories/store/user"
	"github.com/gsoultan/metis/server/repositories/store/userorganization"
	"github.com/gsoultan/storm/runtime"
)

// GetByIdentity resolves the account an identity provider's sign-in is linked
// to. Unscoped, as the lookup by username is: it runs before there is a tenant.
func (r *userRepository) GetByIdentity(ctx context.Context, issuer, subject string) (models.UserModel, error) {
	row, err := r.row(ctx, user.IdentityIssuer.Eq(issuer), user.IdentitySubject.Eq(subject))
	if err != nil {
		return models.UserModel{}, err
	}
	return r.hydrate(ctx, row)
}

// CreateLinked creates the account a first sign-in through an identity
// provider is given, and its memberships, together.
//
// No password: the hash is empty, which no password matches, so the account
// cannot be signed in to locally. The unique indexes are what refuse a clash —
// a username somebody holds, or an identity another sign-in has just linked —
// rather than a read before the write, which two sign-ins at once could both
// pass.
func (r *userRepository) CreateLinked(ctx context.Context, u models.UserModel, issuer, subject string) (models.UserModel, error) {
	roles, err := json.Marshal(u.Roles)
	if err != nil {
		return models.UserModel{}, fmt.Errorf("could not encode the account's roles: %w", err)
	}
	var created models.UserModel
	err = r.conn.conn.TransactMain(ctx, func(txCtx context.Context) error {
		ex, err := r.conn.conn.MainExecutor(txCtx)
		if err != nil {
			return err
		}
		ins := user.Create()
		if id := uuid.UUID(u.ID); id != uuid.Nil {
			ins.SetID(id)
		}
		ins.SetUsername(u.Username)
		ins.SetPasswordHash("")
		ins.SetFullName(u.FullName)
		ins.SetDisplayName(u.DisplayName)
		ins.SetOrganization(u.Organization)
		ins.SetEmail(u.Email)
		ins.SetRoles(roles)
		ins.SetIdentityIssuer(issuer)
		ins.SetIdentitySubject(subject)
		row, err := ins.Insert(txCtx, ex)
		if errors.Is(err, runtime.ErrUniqueViolation) {
			return fmt.Errorf("%w: %w", contracts.ErrAccountTaken, err)
		}
		if err != nil {
			return fmt.Errorf("could not create the account: %w", err)
		}
		for _, org := range u.Organizations {
			if err := r.AddOrganization(txCtx, row.ID, uuid.UUID(org.ID)); err != nil {
				return err
			}
		}
		created, err = r.hydrate(txCtx, row)
		return err
	})
	return created, err
}

// LiveOrganizations answers which of the ids name an organization that exists.
//
// Unscoped, because the question is asked to find out which tenants somebody
// may be placed in — it cannot be asked from inside one. It answers only about
// ids the caller already holds, and only whether each is there.
func (r *userRepository) LiveOrganizations(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := organization.New().
		Where(organization.ID.In(uuidsToRaw(ids)...)).
		Unordered().
		All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read the organizations: %w", err)
	}
	live := make(map[uuid.UUID]bool, len(rows))
	for _, row := range rows {
		live[row.ID] = true
	}
	// The order given is kept: the first organization somebody is placed in is
	// the one their requests land in unless they choose another.
	found := make([]uuid.UUID, 0, len(rows))
	for _, id := range ids {
		if live[id] {
			found = append(found, id)
		}
	}
	return found, nil
}

// SetOrganizations makes the account a member of exactly organizationIDs.
//
// The account row is locked first, so two sign-ins carrying different claims
// at once take turns: each leaves the set it was given, rather than the two
// interleaving into a set neither of them named.
func (r *userRepository) SetOrganizations(ctx context.Context, userID uuid.UUID, organizationIDs []uuid.UUID) (bool, error) {
	changed := false
	err := r.conn.conn.TransactMain(ctx, func(txCtx context.Context) error {
		ex, err := r.conn.conn.MainExecutor(txCtx)
		if err != nil {
			return err
		}
		_, found, err := user.New().Where(user.ID.Eq(userID)).ForUpdate().One(txCtx, ex)
		if err != nil {
			return fmt.Errorf("could not lock the account: %w", err)
		}
		if !found {
			return fmt.Errorf("%w: no such user", apierr.ErrNotFound)
		}
		current, err := userorganization.New().
			Where(userorganization.UserID.Eq(userID)).
			Unordered().
			All(txCtx, ex, nil)
		if err != nil {
			return fmt.Errorf("could not read the account's organizations: %w", err)
		}
		changed, err = r.replaceOrganizations(txCtx, userID, current, organizationIDs)
		return err
	})
	return changed, err
}

// replaceOrganizations removes the memberships not wanted and adds the missing
// ones, reporting whether it did either.
func (r *userRepository) replaceOrganizations(ctx context.Context, userID uuid.UUID, current []userorganization.Row, wanted []uuid.UUID) (bool, error) {
	keep := make(map[uuid.UUID]bool, len(wanted))
	for _, id := range wanted {
		keep[id] = true
	}
	held := make(map[uuid.UUID]bool, len(current))
	changed := false
	for _, membership := range current {
		held[membership.OrganizationID] = true
		if keep[membership.OrganizationID] {
			continue
		}
		if err := r.RemoveOrganization(ctx, userID, membership.OrganizationID); err != nil {
			return false, err
		}
		changed = true
	}
	for _, id := range wanted {
		if held[id] {
			continue
		}
		if err := r.AddOrganization(ctx, userID, id); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}
