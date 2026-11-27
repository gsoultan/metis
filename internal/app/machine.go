package app

import (
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
)

// thisMachine is this process as the Servers page lists it. Its id is new
// each start, so a machine started again is listed again, and the old process
// goes quiet beside it until it is forgotten.
func thisMachine(role Role) entities.Machine {
	name, err := os.Hostname()
	if err != nil || name == "" {
		name = "unknown"
	}
	return entities.Machine{
		ID:        uuid.New(),
		Name:      name,
		Address:   resolveAddress(envHTTPAddress, defaultHTTPAddress),
		Role:      string(role),
		Version:   Version(),
		StartedAt: time.Now().UTC(),
		RoleFixed: roleFixed(),
	}
}
