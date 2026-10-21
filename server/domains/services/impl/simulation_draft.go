package impl

import (
	"context"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/validation"
)

// draftVersionFloor and draftVersionSpread place a draft's version far above
// any version a deploy will allocate.
//
// The engine starts an instance by key and version and reads every later step
// back from the definitions table, so a draft has to be a row there for the
// length of the run. It is written inside the run's transaction and rolled back
// with it, so nobody else ever sees it — but on PostgreSQL an uncommitted row
// still holds its slot in the (project, key, version) unique index, and a real
// deploy allocating that same number would wait on the simulation to finish.
// Real versions count up from 1; a billion is out of their reach. The spread
// keeps two drafts of the same process, simulated at once, from waiting on
// each other in the same way.
//
// Positive, because the engine reads a version of zero or less as "whatever is
// live" — a draft numbered -1 would quietly run the deployed model instead.
const (
	draftVersionFloor  = 1_000_000_000
	draftVersionSpread = 100_000_000
)

// stageDraft writes the request's draft as a throwaway version and points the
// request at it.
//
// It must run inside the simulation's unit of work: the row is only safe
// because that transaction is always rolled back. The checks are the ones a
// deploy makes, in the same order, so a draft that simulates is a draft that
// deploys — and one that would be refused is refused here, with the same
// words, rather than run in a shape production would never accept.
func (s *simulationService) stageDraft(ctx context.Context, req *entities.SimulationRequest) error {
	def, err := draftDefinition(req)
	if err != nil {
		return err
	}

	validator := validation.NewVisitor()
	def.Accept(validator)
	if !validator.IsValid() {
		return apierr.Invalidf("this draft could not be deployed, so it cannot be simulated: %s",
			strings.Join(validator.Errors(), "; "))
	}
	if err := authorizeLookups(ctx, def); err != nil {
		return err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("could not generate a draft definition id: %w", err)
	}
	def.ID = id
	def.Project = &entities.Project{ID: req.ProjectID}
	def.Version = draftVersion(id)

	if err := s.repo.Definition().Create(ctx, adapters.DefinitionModelAdapter{Definition: def}.ToModel()); err != nil {
		return fmt.Errorf("could not stage the draft for this run: %w", err)
	}

	req.DefinitionKey = def.Key
	req.Version = def.Version
	return nil
}

// draftVersion spreads drafts across the range from the id's random tail,
// which a UUIDv7 already carries — no second source of randomness needed.
func draftVersion(id uuid.UUID) int {
	return draftVersionFloor + int(binary.BigEndian.Uint32(id[12:])%draftVersionSpread)
}

// draftDefinition reads the draft from whichever form the caller sent it in,
// and settles its key against the one the request names.
//
// Both naming a key and disagreeing is refused rather than resolved: either
// choice would run something under a name the caller did not mean.
func draftDefinition(req *entities.SimulationRequest) (*entities.ProcessDefinition, error) {
	if req.Draft != nil && req.DraftBPMN != "" {
		return nil, apierr.Invalidf("send the draft once — as a definition or as BPMN XML, not both")
	}

	def := req.Draft
	if def == nil {
		parsed, err := (&BPMNXMLParser{}).Parse(strings.NewReader(req.DraftBPMN))
		if err != nil {
			return nil, apierr.Invalidf("the draft's BPMN XML could not be read: %v", err)
		}
		def = parsed
	}

	switch {
	case def.Key == "":
		def.Key = req.DefinitionKey
	case req.DefinitionKey != "" && req.DefinitionKey != def.Key:
		return nil, apierr.Invalidf("the draft is process %q but the request names %q", def.Key, req.DefinitionKey)
	}
	if def.Key == "" {
		return nil, apierr.Invalidf("the draft needs a process key")
	}
	return def, nil
}
