package contracts

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
)

// RuleImporter turns rules written for another rule engine into metis
// decisions and the process that consults them.
type RuleImporter interface {
	// ImportGruleRules translates Grule rules (GRL or Grule JSON). It always
	// reports what became of every rule; unless asked only to preview, it
	// stores the decisions, and when asked deploys the process, in one
	// transaction.
	ImportGruleRules(ctx context.Context, req entities.GruleImportRequest) (entities.GruleImport, error)
}
