package impl

import (
	"context"
	"errors"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

// NewUnavailableRuleImporter returns an importer that refuses every call, for
// a facade put together without one.
func NewUnavailableRuleImporter() contracts.RuleImporter { return unavailableRuleImporter{} }

type unavailableRuleImporter struct{}

func (unavailableRuleImporter) ImportGruleRules(context.Context, entities.GruleImportRequest) (entities.GruleImport, error) {
	return entities.GruleImport{}, errors.New("importing rules is not available on this server")
}
