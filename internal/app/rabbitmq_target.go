package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/domains/services/impl/connectors"
)

// rabbitMQTarget is the project a bridge or consumer acts for, and the
// connection on that project it reaches its broker by.
//
// A connection rather than a broker URL in the environment: the connection is
// where the project already keeps its broker's address and credentials,
// encrypted at rest and masked whenever the API returns it, and where an
// administrator rotates them. A URL in the environment would be a second copy
// of the same password, in the one place that does neither.
type rabbitMQTarget struct {
	project    uuid.UUID
	connection uuid.UUID
}

func parseRabbitMQTarget(project, connection string) (rabbitMQTarget, error) {
	projectID, err := parseConfiguredID("project", project)
	if err != nil {
		return rabbitMQTarget{}, err
	}
	connectionID, err := parseConfiguredID("connection", connection)
	if err != nil {
		return rabbitMQTarget{}, err
	}
	return rabbitMQTarget{project: projectID, connection: connectionID}, nil
}

// connectionError says why the connection could not be read.
func (t rabbitMQTarget) connectionError(err error) error {
	if errors.Is(err, apierr.ErrNotFound) {
		return fmt.Errorf("connection %s does not exist in the organization project %s belongs to; "+
			"the Connectors page shows a connection's id in Expert Mode", t.connection, t.project)
	}
	return fmt.Errorf("could not read connection %s: %w", t.connection, err)
}

// brokerOf checks that connection is a RabbitMQ connection of this target's
// project, and reads where its broker is.
//
// The project is checked because the read is scoped to the organization, not
// the project: without it, a bridge configured for one project could publish
// through another project's broker.
func (t rabbitMQTarget) brokerOf(connection entities.ConnectorInstance) (rabbitMQBroker, error) {
	if connection.Project == nil || connection.Project.ID != t.project {
		return rabbitMQBroker{}, fmt.Errorf("connection %s belongs to another project, not to project %s",
			t.connection, t.project)
	}
	if connection.Connector == nil || connection.Connector.Key != serviceimpl.RabbitMQConnectorKey {
		return rabbitMQBroker{}, fmt.Errorf("connection %s is not a RabbitMQ connection", t.connection)
	}
	url, _ := connectors.TextSetting(connection.Config, "url")
	if strings.TrimSpace(url) == "" {
		return rabbitMQBroker{}, fmt.Errorf("connection %s has no RabbitMQ URL; set one on the Connectors page",
			t.connection)
	}
	broker, err := rabbitMQBrokerAt(url)
	if err != nil {
		return rabbitMQBroker{}, fmt.Errorf("connection %s: %w", t.connection, err)
	}
	return broker, nil
}
