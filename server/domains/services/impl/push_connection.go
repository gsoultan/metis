package impl

import (
	"cmp"
	"context"
	"slices"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/impl/connectors"
)

// connectionLister is the part of the connector service push reads.
type connectionLister interface {
	ListConnectorInstances(ctx context.Context, projectID uuid.UUID) ([]entities.ConnectorInstance, error)
}

// pushConnectionKey returns the service account key of the project's push
// connection: a Firebase connection with "Use for push" on.
//
// More than one so marked is an administrator's mistake the form does not yet
// prevent; the one changed most recently is used, so the answer is the same on
// every node and every send, and is the one somebody most likely meant.
func pushConnectionKey(ctx context.Context, lister connectionLister, projectID uuid.UUID) (string, bool, error) {
	instances, err := lister.ListConnectorInstances(ctx, projectID)
	if err != nil {
		return "", false, err
	}
	instances = slices.DeleteFunc(instances, func(instance entities.ConnectorInstance) bool {
		return !isPushConnection(instance)
	})
	if len(instances) == 0 {
		return "", false, nil
	}
	newest := slices.MaxFunc(instances, func(a, b entities.ConnectorInstance) int {
		return cmp.Or(a.UpdatedAt.Compare(b.UpdatedAt), cmp.Compare(a.ID.String(), b.ID.String()))
	})
	key, _ := connectors.TextSetting(newest.Config, connectors.FCMServiceAccountKey)
	return key, key != "", nil
}

func isPushConnection(instance entities.ConnectorInstance) bool {
	if instance.Connector == nil {
		return false
	}
	isFCM := instance.Connector.Key == connectors.FCMConnectorKey || instance.Connector.ID == connectors.FCMCatalogueEntry().ID
	use, _ := connectors.TextSetting(instance.Config, connectors.FCMUseForPush)
	return isFCM && use == "true"
}
