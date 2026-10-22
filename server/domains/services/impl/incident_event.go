package impl

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// announceIncident tells the observers that an incident was raised at a step,
// in the transaction that raised it: an outgoing webhook asking for
// incident.raised is owed one exactly when the incident committed.
//
// instance is the one the incident holds, as its raiser read it. Its process
// and the step's name are read here, so a receiver is told which process and
// which step without the raiser having had to load the definition. That read
// runs under a savepoint (attempt): a definition that cannot be read leaves
// the event less complete, never the incident unraised, because on
// PostgreSQL a failed statement would abort the transaction around it.
//
// audited says the raiser has written its own trail entry for what raised the
// incident, as a hold does, so the audit observer writes no second one.
func announceIncident(
	ctx context.Context,
	engine servicecontracts.ExecutionEngine,
	attempt func(ctx context.Context, fn func(ctx context.Context) error) error,
	instance entities.ProcessInstance,
	nodeID string,
	incidentID uuid.UUID,
	audited bool,
) {
	if engine == nil {
		return
	}
	node := &entities.Node{ID: nodeID}
	if instance.Definition != nil && instance.Definition.ID != uuid.Nil && instance.Definition.Key == "" {
		err := attempt(ctx, func(ctx context.Context) error {
			def, err := engine.GetProcessDefinition(ctx, instance.Definition.ID)
			if err != nil {
				return err
			}
			instance.Definition = def
			return nil
		})
		if err != nil {
			log.Warn().Err(err).
				Str("instance_id", instance.ID.String()).
				Str("incident_id", incidentID.String()).
				Msg("Could not read the process of a raised incident; it is announced without its process and step names")
		}
	}
	if instance.Definition != nil {
		if found := instance.Definition.FindNode(nodeID); found != nil {
			node = &entities.Node{ID: found.ID, Name: found.Name}
		}
	}
	engine.DispatchEvent(ctx, entities.ProcessEvent{
		Type:       entities.EventIncidentRaised,
		Instance:   &instance,
		Project:    instance.Project,
		Node:       node,
		IncidentID: incidentID,
		Timestamp:  time.Now().Unix(),
		Audited:    audited,
	})
}
