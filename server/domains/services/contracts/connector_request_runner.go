package contracts

import "context"

// ConnectorRequestRunner runs a connector call that carries a step's own
// request.
//
// Deliberately not part of ConnectorService. The service facade embeds that,
// so every endpoint can reach it, and it already offers ExecuteConnector with a
// configuration the caller supplies. Offering this beside it would put "run this
// statement against this connection" one endpoint away from any caller. The only
// consumer is the job service, running a step that was deployed.
type ConnectorRequestRunner interface {
	ExecuteConnectorRequest(ctx context.Context, connectorKey string, config map[string]any, req ConnectorRequest) (map[string]any, error)
}
