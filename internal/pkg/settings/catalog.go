package settings

import (
	"slices"
	"time"
)

const (
	day = 24 * time.Hour
	// maxDays is the longest retention the server reads, in days.
	maxDays = 36_500
	// maxWorkers bounds a worker count: the server ignores more than this.
	maxWorkers = 100_000
)

// catalog is every setting the Settings page may change.
//
// What is not here stays in the environment, for the reason EnvironmentOnly
// gives against each. Adding a setting here is a decision that an
// administrator signed in to the UI may change it: see EnvironmentOnly before
// making it.
var catalog = []Definition{
	// Email: where notifications and People's task links are sent from. The
	// mail channel is built once at start, so a change waits for a restart.
	{Name: "NOTIFICATION_SMTP_HOST", Group: GroupEmail, Label: "Mail server",
		Help: "The SMTP server notifications and task links are sent through. Email is off until this and the sender are set.",
		Kind: KindText, Applies: AtRestart},
	{Name: "NOTIFICATION_SMTP_PORT", Group: GroupEmail, Label: "Mail server port",
		Help: "Usually 587 for STARTTLS or 465 for TLS.", Default: "587",
		Kind: KindInteger, Min: 1, Max: 65_535, Applies: AtRestart},
	{Name: "NOTIFICATION_SMTP_USERNAME", Group: GroupEmail, Label: "Mail username",
		Help: "Left empty, mail is sent without signing in to the server.", Kind: KindText, Applies: AtRestart},
	{Name: "NOTIFICATION_SMTP_PASSWORD", Group: GroupEmail, Label: "Mail password",
		Help: "Stored encrypted, and never shown again once saved.", Kind: KindText, Secret: true, Applies: AtRestart},
	{Name: "NOTIFICATION_SMTP_FROM", Group: GroupEmail, Label: "Sender address",
		Help: "The From address of every message, such as metis@example.com.", Kind: KindText, Applies: AtRestart},
	{Name: "NOTIFICATION_WEBHOOK_URL", Group: GroupEmail, Label: "Notification webhook",
		Help: "An address every notification is also posted to.", Kind: KindURL, Applies: AtRestart},
	{Name: "METIS_TASK_LINK_LIFETIME", Group: GroupEmail, Label: "Task link lifetime",
		Help: "How long a task link mailed to a person is good for.", Default: "168h",
		Kind: KindDuration, MinDuration: time.Hour, MaxDuration: 90 * day, Applies: AtRestart},

	// Single sign-on.
	{Name: "OIDC_ISSUER", Group: GroupSignIn, Label: "Identity provider",
		Help: "The OpenID Connect issuer address. Sign-in through it is on once this and the client id are set.",
		Kind: KindURL, Applies: AtRestart},
	{Name: "OIDC_CLIENT_ID", Group: GroupSignIn, Label: "Client id",
		Help: "This server's client id at the identity provider.", Kind: KindText, Applies: AtRestart},
	{Name: "METIS_OIDC_ORGANIZATION_CLAIM", Group: GroupSignIn, Label: "Organization claim",
		Help: "The ID-token claim that lists the organizations a person belongs to. Without it nobody signing in through the provider can be placed in one.",
		Kind: KindText, Applies: AtRestart},

	// Retention: the opt-in sweeps.
	{Name: "METIS_RETENTION_FINISHED_INSTANCES_DAYS", Group: GroupRetention, Label: "Finished instances",
		Help: "Days a completed or cancelled instance is kept before it is removed. 0 keeps them for ever.", Default: "0 (kept for ever)",
		Kind: KindDays, Min: 0, Max: maxDays, Applies: Live},
	{Name: "METIS_RETENTION_VARIABLE_SNAPSHOTS_DAYS", Group: GroupRetention, Label: "Variable history",
		Help: "Days a variable's earlier values are kept. 0 keeps them for ever.", Default: "0 (kept for ever)",
		Kind: KindDays, Min: 0, Max: maxDays, Applies: Live},
	{Name: "METIS_RETENTION_COMPLETED_JOBS_DAYS", Group: GroupRetention, Label: "Completed jobs",
		Help: "Days a finished background job is kept. 0 keeps them for ever.", Default: "0 (kept for ever)",
		Kind: KindDays, Min: 0, Max: maxDays, Applies: Live},

	// Workers.
	{Name: "METIS_JOB_WORKERS", Group: GroupWorkers, Label: "Job workers",
		Help: "Background jobs each replica runs at once.", Default: "10",
		Kind: KindInteger, Min: 1, Max: maxWorkers, Applies: AtRestart},
	{Name: "METIS_JOB_POLL_INTERVAL", Group: GroupWorkers, Label: "Job poll interval",
		Help: "How often an idle worker looks for work.", Default: "2s",
		Kind: KindDuration, MinDuration: 100 * time.Millisecond, MaxDuration: time.Minute, Applies: AtRestart},
	{Name: "METIS_JOB_LEASE", Group: GroupWorkers, Label: "Job lease",
		Help: "How long a worker holds a job before another may take it. At least 2m, so a call still in flight is never run twice.", Default: "5m",
		Kind: KindDuration, MinDuration: 2 * time.Minute, MaxDuration: 24 * time.Hour, Applies: AtRestart},
	{Name: "METIS_OUTGOING_WEBHOOK_WORKERS", Group: GroupWorkers, Label: "Webhook workers",
		Help: "Outgoing webhook deliveries each replica sends at once.", Default: "8",
		Kind: KindInteger, Min: 1, Max: maxWorkers, Applies: AtRestart},
	{Name: "METIS_OUTGOING_WEBHOOK_POLL_INTERVAL", Group: GroupWorkers, Label: "Webhook poll interval",
		Help: "How often the webhook sender looks for deliveries.", Default: "2s",
		Kind: KindDuration, MinDuration: 100 * time.Millisecond, MaxDuration: time.Minute, Applies: AtRestart},
	{Name: "METIS_OUTGOING_WEBHOOK_TIMEOUT", Group: GroupWorkers, Label: "Webhook timeout",
		Help: "How long one delivery may take before it is retried.", Default: "10s",
		Kind: KindDuration, MinDuration: time.Second, MaxDuration: 5 * time.Minute, Applies: AtRestart},
	{Name: "METIS_RABBITMQ_CONFIRM_TIMEOUT", Group: GroupWorkers, Label: "Broker confirm timeout",
		Help: "How long a publish to RabbitMQ waits for the broker to confirm it.", Default: "10s",
		Kind: KindDuration, MinDuration: time.Second, MaxDuration: 5 * time.Minute, Applies: AtRestart},
	{Name: "METIS_SHUTDOWN_DRAIN", Group: GroupWorkers, Label: "Shutdown drain",
		Help: "How long a stopping replica waits for the jobs it is running.", Default: "20s",
		Kind: KindDuration, MinDuration: 0, MaxDuration: 10 * time.Minute, Applies: AtRestart},

	// Limits and timeouts.
	{Name: "METIS_HTTP_TIMEOUT", Group: GroupLimits, Label: "Outbound call timeout",
		Help: "How long a service task's call may take.", Default: "30s",
		Kind: KindDuration, MinDuration: time.Second, MaxDuration: 10 * time.Minute, Applies: AtRestart},
	{Name: "METIS_HTTP_MAX_RESPONSE_BYTES", Group: GroupLimits, Label: "Largest outbound reply",
		Help: "Bytes of one reply a service task reads before refusing it.", Default: "8388608 (8 MiB)",
		Kind: KindInteger, Min: 1024, Max: 1 << 30, Applies: Live},
	{Name: "METIS_HTTP_RATE_LIMIT", Group: GroupLimits, Label: "Requests per minute",
		Help: "Requests one client may make a minute, across every replica.", Default: "240",
		Kind: KindInteger, Min: 1, Max: 100_000, Applies: AtRestart},
	{Name: "METIS_HTTP_MAX_IN_FLIGHT", Group: GroupLimits, Label: "Requests at once",
		Help: "Requests each replica serves at once before it queues the rest.", Default: "128",
		Kind: KindInteger, Min: 1, Max: 100_000, Applies: AtRestart},
	{Name: "METIS_HTTP_MAX_QUEUED", Group: GroupLimits, Label: "Requests queued",
		Help: "Requests each replica queues before it refuses more.", Default: "256",
		Kind: KindInteger, Min: 1, Max: 100_000, Applies: AtRestart},
	{Name: "METIS_SCRIPT_TIMEOUT", Group: GroupLimits, Label: "Script time budget",
		Help: "How long a script task, gateway condition or decision cell may run.", Default: "5s",
		Kind: KindDuration, MinDuration: 10 * time.Millisecond, MaxDuration: time.Minute, Applies: AtRestart},
	{Name: "METIS_SCRIPT_CONCURRENCY", Group: GroupLimits, Label: "Scripts at once",
		Help: "Scripts each replica runs at once.", Default: "4",
		Kind: KindInteger, Min: 1, Max: 1024, Applies: AtRestart},
	{Name: "METIS_MAX_EXECUTION_DEPTH", Group: GroupLimits, Label: "Deepest path",
		Help: "Steps one execution may go through one after another before it stops with an incident.", Default: "200",
		Kind: KindInteger, Min: 10, Max: 100_000, Applies: Live},
	{Name: "METIS_MAX_EXECUTION_NODES", Group: GroupLimits, Label: "Most steps",
		Help: "Steps one execution may enter in all before it stops with an incident.", Default: "10000",
		Kind: KindInteger, Min: 100, Max: 10_000_000, Applies: Live},
	{Name: "METIS_SQL_LOOKUP_MAX_POOLS", Group: GroupLimits, Label: "Database lookup pools",
		Help: "External databases each replica keeps connections to at once.", Default: "32",
		Kind: KindInteger, Min: 1, Max: 1024, Applies: AtRestart},
	{Name: "METIS_SQL_LOOKUP_MAX_CONNS", Group: GroupLimits, Label: "Connections per lookup database",
		Help: "Connections each replica opens to one external database.", Default: "4",
		Kind: KindInteger, Min: 1, Max: 256, Applies: AtRestart},
	{Name: "METIS_DEVIATION_APPROVAL_TTL", Group: GroupLimits, Label: "Second approval wait",
		Help: "How long a change waiting for a second administrator stays open.", Default: "72h",
		Kind: KindDuration, MinDuration: time.Hour, MaxDuration: 720 * time.Hour, Applies: AtRestart},

	// Caches.
	{Name: "METIS_AUTH_CACHE_TTL", Group: GroupCaches, Label: "Sign-in cache",
		Help: "How long a signed-in account's roles are reused before they are read again. 0 reads them every time.", Default: "5s",
		Kind: KindDuration, MinDuration: 0, MaxDuration: 5 * time.Minute, Applies: AtRestart},
	{Name: "METIS_AUTH_CACHE_SIZE", Group: GroupCaches, Label: "Sign-in cache size",
		Help: "Accounts each replica keeps in the sign-in cache.", Default: "10000",
		Kind: KindInteger, Min: 1, Max: 1_000_000, Applies: AtRestart},
	{Name: "METIS_DEFINITION_CACHE_SIZE", Group: GroupCaches, Label: "Process cache size",
		Help: "Process versions each replica keeps decoded.", Default: "256",
		Kind: KindInteger, Min: 1, Max: 100_000, Applies: AtRestart},
	{Name: "METIS_INSTANCE_COUNT_CACHE_TTL", Group: GroupCaches, Label: "Instance count cache",
		Help: "How long the instance list's counts are reused.", Default: "1s",
		Kind: KindDuration, MinDuration: 0, MaxDuration: time.Minute, Applies: AtRestart},

	// Observability.
	{Name: "METIS_OTLP_ENDPOINT", Group: GroupObservability, Label: "Trace collector",
		Help: "Host and port of an OpenTelemetry collector, such as collector:4318. Tracing is off until it is set.",
		Kind: KindHostPort, Applies: AtRestart},
	{Name: "METIS_OTLP_INSECURE", Group: GroupObservability, Label: "Send traces without TLS",
		Help: "Only for a collector on a private network.", Default: "false",
		Kind: KindBoolean, Applies: AtRestart},
	{Name: "METIS_OTLP_SAMPLE_RATIO", Group: GroupObservability, Label: "Traces sampled",
		Help: "The fraction of requests traced, from 0 to 1.", Default: "0.05",
		Kind: KindRatio, Applies: AtRestart},
}

// All returns every setting the Settings page may change, in the order the
// page shows them.
func All() []Definition { return slices.Clone(catalog) }

// Find returns the definition of the setting named name.
func Find(name string) (Definition, bool) {
	i := slices.IndexFunc(catalog, func(d Definition) bool { return d.Name == name })
	if i < 0 {
		return Definition{}, false
	}
	return catalog[i], true
}
