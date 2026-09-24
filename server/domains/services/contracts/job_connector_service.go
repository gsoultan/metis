package contracts

// JobConnectorService is what the job service needs of connectors: resolving the
// connection a step names, and running a call that carries the step's own
// request.
//
// Composed here, for the one consumer that needs both, rather than by widening
// ConnectorService — which the service facade embeds and every endpoint can
// reach. See ConnectorRequestRunner.
type JobConnectorService interface {
	ConnectorService
	ConnectorRequestRunner
}
