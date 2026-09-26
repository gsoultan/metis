package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// rabbitMQConnections is how the RabbitMQ runner reads the connection a bridge
// or consumer names. The service facade is one; so is a test's map.
//
// Through the connector service rather than the API, so the URL arrives as it
// is stored, not masked.
type rabbitMQConnections interface {
	GetConnectorInstance(ctx context.Context, id uuid.UUID) (entities.ConnectorInstance, error)
}
