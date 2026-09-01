package impl

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// TestRequireOwnOrganizations is the guard against an administrator of one
// tenant creating records inside another.
//
// The organization arrives in the request body, so the repository tenant scope
// cannot help: a user row created in the victim's organization is well-formed
// and correctly scoped — to the victim. Only this layer can tell the difference
// between the tenant the caller is in and a tenant they merely named.
func TestRequireOwnOrganizations(t *testing.T) {
	own, foreign := uuid.New(), uuid.New()

	tests := []struct {
		name    string
		tenant  string
		orgs    []*entities.Organization
		wantErr error
	}{
		{
			name:   "own organization is allowed",
			tenant: own.String(),
			orgs:   []*entities.Organization{{ID: own}},
		},
		{
			name:    "another tenant's organization is refused",
			tenant:  own.String(),
			orgs:    []*entities.Organization{{ID: foreign}},
			wantErr: ErrForeignOrganization,
		},
		{
			name:    "a foreign organization hidden among own ones is refused",
			tenant:  own.String(),
			orgs:    []*entities.Organization{{ID: own}, {ID: foreign}},
			wantErr: ErrForeignOrganization,
		},
		{
			name:   "naming no organization is allowed",
			tenant: own.String(),
			orgs:   nil,
		},
		{
			name:   "nil entries are skipped rather than dereferenced",
			tenant: own.String(),
			orgs:   []*entities.Organization{nil, {ID: own}},
		},
		{
			// Setup seeds the first administrator before any tenant exists.
			name:   "system work with no tenant context passes",
			tenant: "",
			orgs:   []*entities.Organization{{ID: foreign}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			if tc.tenant != "" {
				ctx = entities.WithTenantContext(ctx, entities.TenantContext{TenantID: tc.tenant})
			}

			err := requireOwnOrganizations(ctx, tc.orgs)

			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
