package impl

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

var errNoServerConnections = errors.New("connecting servers is not available in this process")

type unavailableServerConnections struct{}

// NewUnavailableServerConnectionService refuses every call, for a facade
// built without a database.
func NewUnavailableServerConnectionService() contracts.ServerConnectionService {
	return unavailableServerConnections{}
}

func (unavailableServerConnections) ServerConnectionIdentity(context.Context) (contracts.ServerIdentity, error) {
	return contracts.ServerIdentity{}, errNoServerConnections
}

func (unavailableServerConnections) MakeServerConnectionCode(context.Context) (contracts.IssuedConnectionCode, error) {
	return contracts.IssuedConnectionCode{}, errNoServerConnections
}

func (unavailableServerConnections) CreateServerConnection(context.Context, string, string) (entities.ServerConnection, error) {
	return entities.ServerConnection{}, errNoServerConnections
}

func (unavailableServerConnections) AnswerServerHandshake(context.Context, contracts.ServerHandshake) (contracts.ServerHandshakeReply, error) {
	return contracts.ServerHandshakeReply{}, errNoServerConnections
}

func (unavailableServerConnections) ListServerConnections(context.Context) ([]entities.ServerConnection, error) {
	return nil, errNoServerConnections
}

func (unavailableServerConnections) AcceptServerConnection(context.Context, uuid.UUID) (entities.ServerConnection, error) {
	return entities.ServerConnection{}, errNoServerConnections
}

func (unavailableServerConnections) RemoveServerConnection(context.Context, uuid.UUID) error {
	return errNoServerConnections
}
