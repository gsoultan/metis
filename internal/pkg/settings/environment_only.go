package settings

import "slices"

const (
	reasonUnlocksStorage = "Needed to reach and decrypt the database the saved settings are kept in."
	reasonSigningKey     = "A signing key. Kept in the database, anyone who could write one row could mint an administrator's token."
	reasonPool           = "Sizes the database pool, which is open before the saved settings can be read."
	reasonListener       = "One wrong value saved from the UI would make the UI unreachable, with no way back through it."
	reasonBreakGlass     = "A development or recovery switch, which has to work when the UI does not."
	reasonWhoIsAdmin     = "Decides who may change installation-wide settings, or relaxes the second-approver rule. An administrator could otherwise widen their own power."
	reasonEgress         = "The fence that keeps process definitions and connectors off the internal network."
	reasonPerimeter      = "Decides which clients and browser origins are trusted."
	reasonRelaxation     = "Relaxes a rule that refuses by default. Switching it on is the operator's decision, not an administrator's."
	reasonLeavesInstall  = "Sends a project's work to a broker outside Metis: whether work leaves the installation is the operator's decision."
	reasonLinks          = "Task links carry sign-in tokens; the address they point at is the operator's to choose."
	reasonDeprecated     = "Deprecated: register each receiver as a project's outgoing webhook instead."
)

// environmentOnly is every setting the server reads that the Settings page
// does not change, and why. The page lists them read-only, so an
// administrator can see what the server runs with and where to change it.
var environmentOnly = []Fixed{
	{Name: "DATABASE_URL", Class: ClassBootstrap, Reason: reasonUnlocksStorage, Secret: true},
	{Name: "ENCRYPTION_KEY", Class: ClassBootstrap, Reason: reasonUnlocksStorage, Secret: true},
	{Name: "ENCRYPTION_KEY_PREVIOUS", Class: ClassBootstrap, Reason: reasonUnlocksStorage, Secret: true},
	{Name: "JWT_SECRET", Class: ClassBootstrap, Reason: reasonSigningKey, Secret: true},
	{Name: "METIS_PEOPLE_TOKEN_KEY", Class: ClassBootstrap, Reason: reasonSigningKey, Secret: true},
	{Name: "METIS_PEOPLE_TOKEN_KEY_PREVIOUS", Class: ClassBootstrap, Reason: reasonSigningKey, Secret: true},
	{Name: "METIS_SETUP_TOKEN", Class: ClassBootstrap, Reason: "Opens the first-run wizard, before anybody can sign in.", Secret: true},
	{Name: "METIS_DB_MAX_OPEN_CONNS", Class: ClassBootstrap, Reason: reasonPool},
	{Name: "METIS_DB_MAX_IDLE_CONNS", Class: ClassBootstrap, Reason: reasonPool},
	{Name: "METIS_DB_CONN_MAX_LIFETIME", Class: ClassBootstrap, Reason: reasonPool},
	{Name: "METIS_DB_CONN_MAX_IDLE_TIME", Class: ClassBootstrap, Reason: reasonPool},
	{Name: "METIS_HTTP_ADDRESS", Class: ClassBootstrap, Reason: reasonListener},
	{Name: "METIS_GRPC_ADDRESS", Class: ClassBootstrap, Reason: reasonListener},
	{Name: "METIS_METRICS_ENABLED", Class: ClassBootstrap, Reason: reasonListener},
	{Name: "METIS_METRICS_ADDRESS", Class: ClassBootstrap, Reason: reasonListener},
	{Name: "METIS_PPROF_ENABLED", Class: ClassBootstrap, Reason: reasonListener},
	{Name: "METIS_PPROF_ADDRESS", Class: ClassBootstrap, Reason: reasonListener},
	{Name: "METIS_PAYLOAD_DIR", Class: ClassBootstrap, Reason: "A directory on each server's own disk, read before the database is open."},
	{Name: "METIS_PAYLOAD_THRESHOLD_BYTES", Class: ClassBootstrap, Reason: "Configured with the payload directory, before the database is open."},
	{Name: "METIS_ALLOW_WEAK_SECRETS", Class: ClassBootstrap, Reason: reasonBreakGlass},
	{Name: "METIS_REFUSE_SCHEMA_DRIFT", Class: ClassBootstrap, Reason: "Decides whether the server starts at all, before the saved settings are read."},
	{Name: "METIS_NEW_PASSWORD", Class: ClassBootstrap, Reason: reasonBreakGlass, Secret: true},

	{Name: "METIS_PLATFORM_ADMINS", Class: ClassGuardrail, Reason: reasonWhoIsAdmin},
	{Name: "METIS_SOLE_ADMINISTRATOR_ORGANIZATIONS", Class: ClassGuardrail, Reason: reasonWhoIsAdmin},
	{Name: "METIS_HTTP_ALLOW_PRIVATE_NETWORKS", Class: ClassGuardrail, Reason: reasonEgress},
	{Name: "METIS_HTTP_ALLOWED_HOSTS", Class: ClassGuardrail, Reason: reasonEgress},
	{Name: "METIS_SQL_LOOKUP_ALLOWED_HOSTS", Class: ClassGuardrail, Reason: reasonEgress},
	{Name: "METIS_TRUSTED_PROXIES", Class: ClassGuardrail, Reason: reasonPerimeter},
	{Name: "METIS_CORS_ORIGINS", Class: ClassGuardrail, Reason: reasonPerimeter},
	{Name: "METIS_PUBLIC_URL", Class: ClassGuardrail, Reason: reasonLinks},
	{Name: "METIS_ALLOW_IMPLICIT_DEFAULT_FLOW", Class: ClassGuardrail, Reason: reasonRelaxation},
	{Name: "METIS_ALLOW_UNASSIGNED_TASK_CLAIMS", Class: ClassGuardrail, Reason: reasonRelaxation},
	{Name: "METIS_ALLOW_UNDECLARED_TASK_VARIABLES", Class: ClassGuardrail, Reason: reasonRelaxation},
	{Name: "METIS_ALLOW_BUILTIN_CONNECTOR_OVERRIDE", Class: ClassGuardrail, Reason: reasonRelaxation},
	{Name: "METIS_TASKS_ALLOW_PLATFORM_ACTORS", Class: ClassGuardrail, Reason: reasonRelaxation},
	{Name: "METIS_FEATURE_STRICT_TENANT_SCOPE", Class: ClassGuardrail, Reason: reasonRelaxation},
	{Name: "METIS_FEATURE_JAVASCRIPT_CONDITIONS", Class: ClassGuardrail, Reason: reasonRelaxation},
	{Name: "METIS_RABBITMQ_BRIDGES", Class: ClassGuardrail, Reason: reasonLeavesInstall, Secret: true},
	{Name: "METIS_RABBITMQ_CONSUMERS", Class: ClassGuardrail, Reason: reasonLeavesInstall, Secret: true},
	{Name: "WEBHOOK_ENDPOINTS", Class: ClassGuardrail, Reason: reasonDeprecated},
}

// EnvironmentOnly returns the settings the Settings page shows and never
// changes.
func EnvironmentOnly() []Fixed { return slices.Clone(environmentOnly) }
