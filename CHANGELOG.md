# Changelog

Notable changes to Metis BPM. Entries describe what changed for somebody
running this, and name the behaviour that changed rather than the files.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Security

- **Any signed-in account could make the server connect wherever it liked.**
  `POST /api/v1/connectors/execute` runs a connector with a configuration its
  caller writes, and it needed only a login. The SMTP and AMQP connectors dial
  their host directly, so an account that only ever opens the task inbox could
  point one at any host and port on the network Metis sits in — and read from
  the error whether something was listening. It now requires an administrator.
  The Connectors page's connection test, its only working caller, is an
  administrator's page already. The designer's "Try it" button was not working
  for any connector and is refused for non-administrators.
- The RabbitMQ connector kept one open connection per broker URL it was given,
  with no bound; it now keeps at most 32 and closes the rest.
- **A RabbitMQ connection's password was returned to every signed-in account.**
  The broker is configured with one `url` — `amqp://user:password@host` — and
  connection settings were masked by the names of their keys, which "url" is
  not. Listing a project's connections, which any signed-in account may do,
  returned it in clear. A URL carrying a password, or a query parameter named
  like a secret (`?api_key=`, `?token=`), is now masked whatever its key.
  Anyone with a RabbitMQ connection configured should rotate that broker
  password; saving the form unchanged keeps the stored URL.

### Added

- **Who holds which role, and what each role is for, on one screen.** The
  Platform access page has a Roles tab beside Accounts: every account in the
  organization against the four roles, and beside each role a button listing
  the actions it is required for — read from the checks the server enforces,
  so it cannot say a role allows something the server refuses
  (`GET /api/v1/roles`, which anybody signed in may read). An administrator
  grants or revokes a role by ticking its box, and each change is saved as it
  is made. A refusal — the organization's last administrator, or an account
  another organization shares — is shown in the server's words and the box
  stays as it was. Anybody else sees who holds what, with no boxes to tick.
- **The strict tenant scope's rollout is on the metrics endpoint.**
  `metis_strict_tenant_scope_enabled` says whether the flag is on, and
  `metis_strict_tenant_scope_denied_site` is one series per code path that
  reached a repository with no identity, labelled with that path. Staging soaks
  become a dashboard and an alert instead of reading logs for a line that
  appears once per site; `docs/strict-tenant-scope.md` has the alert rule.

- **Database Lookup.** A process step can read rows from your own PostgreSQL,
  MySQL or SQL Server database into one process variable, so the gateway or
  decision table after it can decide on data the process does not carry. An
  administrator connects the database once per project on the Connectors page;
  the step's query uses `:named` values mapped from process variables; the
  answer arrives as `x.row`, `x.rows`, `x.row_count` and `x.truncated`. See
  *Looking something up in your own database* in `docs/integration.md`.

  The query is written by whoever designs the process, so the connection's
  login is the boundary that matters: give it SELECT on the tables lookups need
  and nothing else. Every value is sent as a parameter; the query must be one
  read; PostgreSQL and MySQL run it in a read-only transaction the database
  enforces. **SQL Server has no read-only transaction** — there the login's own
  permissions are what stop a write, and every lookup is rolled back rather
  than committed. A connection whose login can see Metis's own tables is
  refused.

  New settings: `METIS_SQL_LOOKUP_MAX_CONNS` (default 4 per database per node),
  `METIS_SQL_LOOKUP_MAX_POOLS` (default 32), and
  `METIS_SQL_LOOKUP_ALLOWED_HOSTS` to hold lookups to a list of database hosts
  — worth setting on a shared installation, where the project administrator who
  sets up a connection is not whoever runs the servers.

- **A database lookup takes a list.** A value that is a list expands to one
  parameter per item, for `WHERE id IN (:ids)`, still bound rather than written
  into the query. An empty list is refused, not treated as matching nothing.
- **Testing a database lookup's connection works.** The Connectors page's
  "Test" answered that a lookup cannot run on its own; it now opens the
  connection with every check a lookup gets and runs nothing of anybody's.
- **The Query author role.** Deploying a process with a database lookup in it
  needs `QUERY_AUTHOR`, held beside Designer; administrators have it already.
  It is created on the next start with no migration. Grant it on the Platform
  access page to anybody who should deploy lookups.

### Security

- **A participant directory's database password was sent to the browser.** The
  participant sources page masked a source's settings by the names of their
  keys, and a PostgreSQL directory keeps its connection string — password
  included — under `dsn`, which no rule recognised. Connection strings are now
  masked however their key is spelled, and so are webhook URLs, which for
  Slack, Discord and Teams are the whole credential. Anyone who has configured
  a PostgreSQL participant directory should rotate that password: it has been
  readable by anybody who could open the page.

### Fixed

- **The Users page stopped at a thousand accounts.** An organization with more
  showed a thousand of them and said it was showing all, and the check that
  keeps one administrator, counting from the same list, could refuse to demote
  an administrator while another one existed past the thousandth. Every account
  is listed and counted now.
- **Refusing to change an account named no organization.** "ana is the last
  administrator of ; make somebody else an administrator first" now names the
  organization. For an account another organization shares, the refusal says
  "another organization" rather than naming one the administrator is not in.
- **The Operator role's description promised migrating running instances**,
  which only an administrator may do. It now says what an operator may do:
  resolve incidents, start ad hoc tasks and broadcast signals. The seeded role
  takes the new description on the next start.
- **Installing a connector's document again switched it back on.** An
  administrator who switched a connector off and then fixed its document found
  it running again. Installing over an installed manifest now keeps the switch
  it had; only a new one is installed switched on.
- **An older connector document could be installed over a newer one.** It
  replaced the newer version without a word, taking every step that uses the
  connector back to the older behaviour. A document whose `version` is lower
  than the installed one is now refused with a 400 that names both versions.
  The same version again, which is how a document is fixed, and higher ones
  install as before.
- **An OpenAPI import that failed part way left part of it installed.** The
  operations before the failure stayed installed and the ones after it did
  not, and the error did not say which operation had failed. The operations an
  import generates are now installed in one transaction: if one cannot be
  installed, none are, and the error names it. An operation the importer
  cannot read is still skipped rather than failing the import.
- **"Try it" on a connector step works.** It sent the connector's id where the
  server expected its key, and the step's mappings where it expected a
  connection, so it failed for every connector. It now runs the step once
  against the connection its project saved, with the step's mappings, and shows
  what the step would store (`POST /api/v1/connectors/try-step`, designers and
  administrators). It is real — a Slack step posts — and says so; nothing is
  recorded on any process. Trying a database lookup needs the Query author role,
  as deploying one does.
- **Mappings set on a connector step were ignored.** The designer's "If the
  names differ" tables saved them under names the server never read, so a step
  configured to rename a value did not. The engine now reads `input_mapping` and
  `output_mapping` on a connector step — the same target → source maps a
  decision uses, FEEL included — and with either set, only the listed fields are
  sent or kept, as an HTTP step's mappings have always worked. A definition
  already deployed is unaffected: its mappings move to the new names when it is
  opened in the designer, and take effect when it is next deployed.
- **Headers configured on an HTTP connection were never sent.** The Connectors
  page saves them as text and the connector read only an object, so an
  `Authorization` header typed into the form never left the server. A call that
  succeeded without it will now carry it.
- **A connector step dragged from the designer's palette had no circuit
  breaker and ignored the connection's rate limit.** A limit set with
  `rate_limit_per_minute` on a connection now applies to those steps too, so a
  process that was calling faster than its limit will now be held to it.
- A boolean setting on the connector form is a switch rather than a text box
  that had to be typed "true".

## [0.3.0] - 2026-09-14

A minor rather than a patch, and the largest release so far. It drops every
database engine except PostgreSQL, so read **Removed** before upgrading: an
installation on SQLite, MySQL or SQL Server cannot take this release as it
stands.

It also carries a privilege escalation fix. `PUT /api/v1/decisions/{id}` was
served without an authorization chain, so any authenticated account could
rewrite a decision table — proven on the shipped default configuration, leaving
the table with no rules. Anyone running 0.2.0 has it.

### Security

- **Five endpoints were routed and served without an authorization chain.**
  `MakeEndpoints` builds a struct of endpoints and the wiring then replaces each
  field with a wrapped copy that resolves the tenant and checks the role. A
  field nobody wraps is still routed: it simply reaches the repository with no
  identity and no role check, and nothing fails or logs.

  `PUT /api/v1/decisions/{id}` was the exploitable one. Creating a decision
  needed the designer role and deleting one did too, while rewriting one — the
  most powerful of the three — needed only a login. Proven against a running
  server on the shipped default configuration: the same account was refused a
  create with 401 and allowed an update with 200, leaving the decision table
  with zero rules and zero inputs. A DMN table with no rules matches nothing,
  and a decision point that matches nothing is an incident on every instance
  that reaches it.

  The connector template endpoints (`POST`, `PUT` and `DELETE
  /api/v1/connectors`) were the same shape: any authenticated account could add
  or rewrite what the engine calls out to and with which credentials, while
  creating an *instance* of the same connector already needed an administrator.
  `ExportOCEL` and the self-service password change were unwrapped too, though
  neither was exploitable.

  None of them was ever reachable anonymously — the transport chain still
  demanded a token — which is exactly why they survived: they pass any "is it
  behind a login?" check. Two further things hid them. `ExportOCEL` failed
  silently, answering 200 with an empty log on a project holding 105 events.
  And under `METIS_FEATURE_STRICT_TENANT_SCOPE` the unwrapped `UpdateDecision`
  returns 404 rather than 200, so the strict-scope soak masks the escalation
  instead of revealing it.

  A test now reads the source and fails on any declared endpoint with no chain,
  because hand-auditing this once is what let four of them ship.

- **The live event stream carried every organization's process variables to
  every signed-in browser.** The SSE client registry was a flat set with no
  record of who was listening, and a process event carries the instance's
  variables — an amount, an applicant's name, an approval decision. So anybody
  with the stream open received all of it, from every tenant, as it happened.
  Confirmed against the running handler before it was changed.

  Events are scoped now, by organization and by environment, compared for
  equality. There is no "unscoped means everybody": an event whose audience
  cannot be worked out reaches nobody, and the unscoped broadcast has been
  removed from the API so delivering to everyone is a compile error rather than
  an omission. The scope comes from the token and from the port a connection
  arrived on, never from anything a caller sends, and it travels with the event
  across the replica bus — a replica reading one back has no request context to
  recompute it from.


- **Connector credentials were returned to the browser and stored in clear
  text.** A connector instance's configuration holds whatever it needs to
  authenticate — a bearer token, an SMTP password, a signing secret — and the
  API returned that map verbatim. Opening the Connectors page put every
  third-party credential an organization had into the response and the browser's
  developer tools, and the column was plain JSON, so a database backup or a read
  replica carried them too.

  Secrets are now replaced with a placeholder at the API boundary and the column
  is encrypted at rest. Editing an unrelated field keeps the stored credential
  rather than overwriting it with the placeholder, clearing one is still
  possible, and the placeholder can never itself become a stored value. The
  executor is unaffected: it reads instances through the service, not the API.
  Existing rows are read as they are and re-encrypted on their next write, so no
  migration is needed.

- **A task could be completed by somebody who was not holding it.** The task
  endpoints took the acting user from a `user_id` field in the request body and
  the service then checked *that name* against the assignee. So any signed-in
  member of an organization could complete, claim or delegate a colleague's task
  by naming the colleague, and the audit trail recorded the colleague as having
  done it. In an engine that runs approvals and payments, that is impersonation
  with a forged record attached.

  The actor now comes from the verified token and any `user_id` on the wire is
  ignored. Handing a task to somebody else is still possible, and still a
  parameter, but only for the person holding it or an administrator. Proven end
  to end through the real HTTP chain.

- **The embedded UI is served with security headers.** A Content-Security-Policy
  written for exactly what the app loads, plus `X-Frame-Options`,
  `Referrer-Policy` and `X-Content-Type-Options`. There were none, on a UI whose
  session token lives in `localStorage`.

### Removed

- **PostgreSQL is the only supported database engine.** SQLite, MySQL and SQL
  Server are gone. An installation running one of them cannot upgrade to this
  release; move the data to PostgreSQL first.

  A config naming a removed driver is now an error at the point of opening. It
  used to fall back to SQLite for anything it did not recognise, which meant
  such a config would open a fresh empty file beside the real database and
  start serving from it — indistinguishable, to whoever restarted the service,
  from total data loss.

  One engine is also what makes the rest of this release checkable: a
  constraint has one spelling, and every test runs against what production
  runs.

### Added

- **The instance list filters in the database, so a project's failures are
  reachable.** The status filter narrowed the twenty-five rows already in the
  browser. A project with 500,000 instances and twelve failures therefore
  offered no way to reach those twelve and no hint that they existed — the list
  simply looked healthy, which is the one wrong answer this page must never
  give.

  `ListInstances` takes a state, a process and "needs attention", applied in
  SQL, and returns counts for the whole project beside the page. So "12 need
  attention" stays true while twenty-five completed runs are on screen, and the
  number is also the control that reaches them. A status it does not recognise
  is refused rather than dropped: dropping it answers "show me everything that
  failed" with every instance in the project.

  "Needs attention" is a separate axis from status because status cannot
  express it. A job that exhausts its retries raises an incident and stops, and
  the instance stays `active` — correctly, it has not failed, it is waiting for
  somebody. Nothing in the engine writes a failed status, so a list that looked
  for one would mark nothing, for ever.

  Two composite indexes come with it, built `CONCURRENTLY` so the upgrade does
  not hold a lock on the busiest table in the system while it runs.

- **A BPMN file exported from here now opens in other BPMN tools, and one
  imported from them keeps its layout.** Export wrote a `<definitions>` element
  with no namespace on it and no diagram interchange section at all. That is
  well-formed XML and this parser read it back, which is why the round trip
  looked healthy — but elements are resolved by namespace and bpmn-js needs a
  diagram to render anything, so every file this engine produced was a file only
  this engine could open. Import had the mirror problem: it read no geometry at
  all, so a diagram drawn in Camunda Modeler arrived with every shape at the
  origin and its author's layout was gone for good.

  Both directions carry the diagram now: shape bounds, whether a sub-process is
  drawn expanded, and the bends of every connector. A definition that was never
  drawn — created over the API, or by a test — is laid out in a row on the way
  out rather than stacked at the origin, so the file still opens legibly. The
  geometry has a field everywhere it has to survive: the node entity, the
  database model, the protobuf, and the designer's save request. That last one
  matters most, because without it the act that flattened an imported diagram
  was saving it.

- **Export stopped silently dropping parts of the process.** There was no case
  for a sub-process, so exporting a process containing one produced a valid file
  with the sub-process and every node inside it missing — and reported success.
  Pools, lanes, escalation and compensation throw events, and the terminate
  marker on an end event went the same way. Import gained the elements it could
  not read at all: `adHocSubProcess`, and escalation, compensation and
  conditional event definitions.

- **The attributes that decide what a process does now survive a round trip.** A
  gateway's `default` flow, a call activity's `calledElement`, a boundary event's
  `cancelActivity` and multi-instance loop characteristics were all dropped. The
  default flow is the one to notice: this engine refuses to guess at a decision
  point, so a gateway that had a default before an import raised an incident
  after it. External-task topics, assignees and form keys are written in the
  Camunda namespace, so a process exported from here is deployable by the engine
  most of these diagrams come from.

- **Conditional events.** A step can wait until something becomes true of the
  process's own data — "carry on once the total is over the limit" — with no
  clock and no inbound message. The condition is read when the token arrives, so
  one that already holds does not wait at all, and again every time the instance
  advances, because the only thing that can make it true is another part of the
  same process changing a variable. It is authorable in the designer and it
  round-trips through BPMN XML.

- **An ad-hoc sub-process can be built in the designer.** The engine has run
  these for a while — a group of steps a person drives in whatever order the
  work needs, until a completion condition says it is finished — but nothing in
  the designer could produce one, so the only way to get one was to import a
  file that already had one. A sub-process has a property panel now, and it
  refuses the two configurations that cannot work: an ad-hoc group with nothing
  in it, and one that is also event-triggered.

- **A project's history exports as an OCEL 2.0 event log**, at
  `GET /api/v1/projects/{id}/ocel`. The audit trail was already a complete record
  of what happened; what it was not was portable, so reading it meant using this
  application. OCEL 2.0 is the current standard for object-centric event data and
  is read by ProM, pm4py and the commercial mining tools. The activity is the
  node's name rather than the audit entry's kind, because a log whose every event
  is `node_reached` discovers a model with four boxes in it however large the
  process is. Instances relate to a definition *and its version*, so two versions
  are not mined as one. Process variables are **not** included unless
  `?include_variables=true` is asked for: an audit entry's data is the instance's
  business facts, and mining a control-flow model needs none of them.

- **A new version of a process no longer takes over the moment it is deployed.**
  Deploying used to make the new version live immediately, because "live" meant
  "the highest version number" and there was no way to say anything else. It is
  a choice now: promote it and let the old version drain — it keeps running the
  instances it has and takes nothing new until the last one finishes — or stage
  it and leave the old one live, or schedule the change for a chosen moment.

  Running instances are never moved. An instance finishes on the graph it
  started with, which is the only property an edit cannot break.

- **A project can have several environments, each with its own database and its
  own port.** Development, staging and production are configured from the UI and
  served on the same server. What one holds is absent from another rather than
  filtered out of it. Which runtime a request belongs to is decided by the port
  it arrived on, never by a header or a body — an environment a caller could
  name is one a staging user could set to production.

  A new environment is served from the next restart, which the settings page
  says.

- **Running instances can be moved onto another version, with a preview.** The
  supported way to change version is still to promote and drain; this is for the
  case drain cannot serve — work in flight on a version that must not continue.
  It shows what would move and where before it moves anything, refuses a mapping
  that would leave a task somewhere the new version has no node for, and is
  administrative, because every other action on that page decides what future
  instances do and this one rewrites instances that have already started.

- **The people who run Metis and the people a process assigns work to are now
  separate.** One table held both, so one role list served both and giving
  somebody a task inbox meant creating them an account on the platform.
  Participants belong to a project, have no roles, and arrive in bulk — from a
  CSV upload, an HTTP endpoint, or a PostgreSQL query, optionally on a schedule.
  An import is additive: somebody absent from the file is left alone rather than
  deactivated.

  Somebody can also be removed from a project's directory. The removal is
  reversible — importing a directory that names them again brings back the same
  person with their teams — and their open tasks stay where they are, because a
  task names its assignee rather than pointing at them.


- **The interface can be shown in another language.** There was no
  internationalization layer at all; every string was hardcoded English. There
  is now a small, tested one: interpolation and plurals, with plural categories
  taken from `Intl.PluralRules` so a language with four of them is handled by
  the platform rather than by a rule written here. English ships with the app
  and every other catalogue is fetched only when chosen, so a language nobody
  selects costs nothing at first paint. A missing key shows the key rather than
  falling back to English, which is what keeps a gap visible.

  **The shell is translated; the pages are not yet.** Navigation, the language
  menu and the offline and update messages go through it — the rest is still
  hardcoded English, which is the large majority of the strings. The machinery
  is proven by a second language that is not a copy of English, and
  `ui/src/i18n/README.md` states the convention and what remains.

- **Tasks can be completed and claimed offline.** They are kept on the device
  and sent when the connection returns. The idempotency key is generated when
  the button is pressed rather than when the request is sent, so a flush that
  succeeds on the server but loses the answer replays instead of completing the
  task twice. A queued action says "Saved on this device", never "Task
  completed". If the server refuses one on arrival — the task was claimed by
  somebody else while the phone was in a pocket — the person is told in a
  notification that does not disappear, and the entry is dropped rather than
  retried forever. Only completing and claiming queue; a write with wider
  consequences still fails outright.

  Offline *reading* is still limited to the application shell: the task list
  comes over Connect RPC, which is a POST, and the worker's cache is
  deliberately GET-only. `ui/src/pwa/README.md` says so and names the fix.

- **The task inbox says what a task is about.** Its widest column showed the
  first eight characters of the process instance's identifier — and those
  identifiers are time-ordered, so every approval created in the same period
  showed the same eight characters. It named nothing, and there was no way to
  tell one row from another. Rows now carry the description the process was
  started with ("Expense of GBP 1,750"), with a short reference code taken from
  the *end* of the identifier, where the characters actually differ. The same
  fix covers the All Tasks list, and a gateway's fallback badge on the canvas
  now says it has one rather than printing part of a flow's identifier.

- **A project is chosen for you.** Signing in left none selected, so the first
  thing after entering a password was "Choose a project to continue" — on every
  screen, every session, even for somebody who belongs to exactly one. The app
  now keeps whatever is already selected, falls back to the first available when
  there is nothing selected or the stored one has gone (deleted, or access
  withdrawn), and only asks when there is genuinely nothing to pick. The empty
  state says that instead of telling people to choose from a header with nothing
  in it.

- **The sample data is a working demo.** `./scripts/dev.sh --sample` created
  none of the groups its own approvals are offered to, so every seeded task sat
  unclaimed in a queue with no members: the inbox — the screen the demo most
  needs to show — was empty on a fresh install, while the script's own summary
  promised approvals waiting in it. It now creates the three groups, a person in
  each, and puts the admin in all of them. It also starts the supplier check,
  which fails on purpose, so the incident inbox has something in it too; the
  summary says the retries have to run out first rather than sending you to look
  at nothing.

- **Runbooks.** [`docs/runbooks.md`](docs/runbooks.md) covers every alerting
  rule plus the four incidents that were not written down: a stuck instance, a
  poison job, an expired connector credential and a database failover. Each
  entry gives the query to run rather than a description of one, and every
  destructive step is preceded by the `SELECT` that shows what it will touch.

- **PostgreSQL guidance.** [`docs/postgresql.md`](docs/postgresql.md): pool
  sizing against the in-flight limit, the three server timeouts that bound
  failures Metis cannot bound from outside (`statement_timeout`,
  `idle_in_transaction_session_timeout`, `lock_timeout`), what to watch, and a
  worked production secret.

- **Missing migrations are visible.** Schema drift is published as
  `metis_schema_drift_items` and alerted on by `MetisSchemaDrift`, with unit
  tests proving the rule fires and stays quiet. It was previously a log line
  only, on a failure that is silent by construction — `/readyz` only pings the
  database, so a feature returning 500 on every request never paged anybody.
  `METIS_REFUSE_SCHEMA_DRIFT=true` turns it into a refusal to start.

- **The supply chain is attestable.** The release now publishes a software bill
  of materials and a maximum-detail provenance attestation, and signs the image
  with cosign keyless — so there is no long-lived signing key to leak. CI scans
  the built image for critical vulnerabilities, and Dependabot proposes updates
  weekly rather than leaving them to be discovered during an incident.

- **The latency targets are measured on the database people deploy.** The SLO
  suite ran in a job with no DSN, so it measured SQLite; it now runs against
  PostgreSQL in the dialects job. The load test — a hundred thousand instances —
  had never run at all, and now runs weekly and on demand.

- **Job throughput is configurable, and a burst drains.** Five workers on a
  fixed two-second poll, claiming at most five jobs a tick, capped a replica at
  roughly 2.5 jobs a second regardless of the machine — and a burst of a
  thousand timers trickled out at that rate with the pool idle in between. The
  sizing is now `METIS_JOB_WORKERS`, `METIS_JOB_POLL_INTERVAL` and
  `METIS_JOB_LEASE`, the worker keeps claiming while rounds come back full, and
  the claim query is ordered oldest-due-first over a new composite index rather
  than returning whatever the database found convenient.

- **The connection pool is sized.** Only SQLite was ever configured. PostgreSQL,
  MySQL and SQL Server ran on `database/sql`'s defaults — unlimited open
  connections and two idle — so a burst could open more than PostgreSQL's
  default `max_connections` and fail every caller at once, while a steady load
  reconnected between bursts for no reason. See `METIS_DB_*` in the README.

- **Deployed process definitions are cached.** A definition is immutable once
  deployed but was read from the database and decoded on every job, every
  message, every timer and twice per task completion — each read a row whose
  node and flow columns are large JSON documents. The cache is bounded, evicts
  least-recently-used, drops on deletion, and is keyed by tenant so a cached
  copy cannot cross an organization boundary.

### Fixed

- **The RabbitMQ connector reported messages as sent that the broker never
  received.** Filling in a URL and a queue — the configuration the connector's
  own schema advertises as "Queue (Direct Publish)" — published to the default
  exchange with an *empty* routing key, which routes to nothing. The message was
  discarded, the service task returned `{"status": "published"}`, the token
  advanced, and the audit trail recorded a message that was sent. The fallback
  written to catch this was unreachable: it ran only when the publish returned an
  error, and an AMQP publish is fire-and-forget, so it never does.

  The queue is now used as the routing key it always was on the default
  exchange. Beyond that, publishes wait for a **publisher confirm** and are sent
  **mandatory**, so a broker that rejects a message, a connection that drops
  mid-publish, and a message nothing is bound to receive are all failures now
  rather than silent successes — which is what lets the retry and the incident
  do their jobs. This is a deliberate behaviour change: a publish that used to
  "succeed" into the void now fails and says why.

  Found by pointing the connector at a real broker for the first time. The suite
  is `tests/connector/broker_test.go`, gated on `METIS_TEST_RABBITMQ_URL`, with
  the service added to CI — the build fails on any skipped test, so the gate
  cannot quietly stop running.

- **The SMTP connector is tested against something that speaks SMTP.** Nothing
  proved an email was ever handed over or what was in it, so the envelope and the
  message could have been wrong in any way and the suite would have been green.
  `tests/connector/smtp_test.go` runs an in-process server and asserts the
  envelope sender, the recipient, the subject header and the body. It needs no
  broker and no gating, so it always runs.

- **A catch event with nothing to wait for hung the instance in silence.** The
  handler returned success while leaving the token where it was, so nothing in
  the system would ever move it: no incident, no log line, and the only symptom
  was a process that stopped. A catch event exists to wait for something, so one
  that names nothing to wait for is a modelling error, and it is refused by name
  now. The same branch handed a *condition* to the timer service as though the
  condition were a duration, which is what conditional events replace.

- **A user's inbox ignored the paging it was asked for.**
  `GET /api/v1/tasks/assignee/{assignee}` declared `page` and `page_size` and
  the endpoint handed them to `ListTasksByAssigneePaged`, but the decoder read
  only the path — so the listing most likely to outgrow one page answered its
  first and nothing could ask for the second. The same omission as the instance
  listing below, in the place it costs most.

- **The instance listing ignored the paging it was asked for.**
  `GET /api/v1/instances` declared `page` and `page_size`, and the query behind
  it ordered and windowed correctly — but the HTTP decoder read only
  `project_id`, so the parameters never reached the endpoint. Every caller got
  the first page at the server default, and a project with more instances than
  that page holds had no way to reach the rest. The decoder now reads them, as
  the task listing's already did.

### Added

- **`GET /api/v1/tasks?instance_id=` filters tasks to one process instance.**
  "What is this run waiting on" previously had no answer: the listing took a
  project and nothing else, so a client holding an instance id had to page the
  whole project and match, which worked only while the task it wanted was still
  on a reachable page. The filter is tenant-scoped like the other request-driven
  reads, so an id from another organization finds nothing rather than somebody
  else's work. It takes precedence over `project_id`, which an instance already
  implies, and a malformed one is refused rather than quietly widening the
  listing to the project.

- **Task responses carry the node's type.** Every task named its node by id
  alone, so a client could not tell a user task from a manual task from the
  node — only from `Task.Type`, and only by knowing that `Node.Type`, the
  obvious place, was never filled in. The type is on the task row already.

  `Node.Name` is deliberately still empty: `UpdateTask` can rename a task, after
  which its name is no longer the diagram's label for that node, and filling the
  field from it would hand callers the newer of the two with no way to tell.
  `Task.Name` is the label to display.

### Changed

- **Soft deletion is declared in the schema rather than remembered at each
  query.** Every read of a table that soft-deletes now carries `deleted_at IS
  NULL` because the model says so, not because the query did. One consequence is
  worth knowing before adding a table: a unique key on such a table covers only
  the live rows by default, so the value frees up when a row is removed. That is
  right for a connector key and wrong for a version number, an idempotency key or
  a participant's username, where reissuing the value would let a second subject
  inherit the first one's history — those declare that they cover the deleted
  rows too, and say on the line above why.

  A schema that has drifted from the model is reported at startup rather than
  altered. Existing tables belong to the numbered migrations; two things deciding
  the shape of one table means the one that ran last wins.


- **The Go SDK moved to its own repository.** It was already its own module —
  a client for an HTTP API has no business making consumers inherit GORM, goja,
  RabbitMQ and OpenTelemetry — and it is now published from
  [gsoultan/metis-sdk](https://github.com/gsoultan/metis-sdk), so it versions
  independently of the engine it talks to and its own CI fails the build if
  `go.mod` ever grows a `require` block.

  For anyone importing it, the package name and every exported symbol are
  unchanged; only the path moves:

  ```go
  github.com/gsoultan/metis/sdk  →  github.com/gsoultan/metis-sdk
  ```

  There is no fallback for this one — a nested module path cannot redirect — so
  it is an edit to make now rather than one with an expiry.
  [`docs/upgrading.md`](docs/upgrading.md) says so alongside the other renames.

### Fixed

- **The UI was served uncompressed and uncacheable.** The embedded assets went
  out through a bare file server: no compression and no `Cache-Control`, and the
  image has no reverse proxy to add either. A first paint was about 1.25 MB on
  the wire where the same bytes gzip to about 330 kB, and every reload refetched
  the whole application. Assets are now compressed once at startup, hashed
  bundles are immutable for a year, the shell revalidates, and conditional
  requests get a 304.

- **A corrupt config file booted the server onto an empty database.** An
  unreadable `config.yaml` was a warning, then the server fell back to the
  environment — and with no `DATABASE_URL` set, that meant creating a *fresh,
  empty SQLite file* and serving from it. The process passed its own readiness
  probe while every list in the product was empty and every write went somewhere
  nobody would look. It now refuses to start and says which file it could not
  read. Unknown keys are refused too: `databse:` used to parse cleanly as "no
  database configured".

- **Every deploy froze in-flight jobs for five minutes.** Shutdown cancelled the
  worker's context and returned. Anything already running was abandoned, and its
  final status write rode that cancelled context and failed — so the row stayed
  marked running, holding this worker's lock, until the lease expired. The
  shipped manifest uses a Recreate strategy, so this happened on every rollout.
  The worker now stops claiming and waits for what it holds
  (`METIS_SHUTDOWN_DRAIN`, 20s), and the status write is made on a context that
  survives the cancellation.

- **Every authenticated request cost about six queries before it started.**
  Validating a token read the account twice — once for the credential cutoff and
  once to build the caller — and each read preloaded the user's organizations
  and projects. One cached read now serves both. The lifetime is five seconds by
  default and deliberately not longer: the cached value carries the cutoff that
  ends sessions, so a stale entry would extend a compromised one. Password, role
  and membership changes drop the entry immediately, which the existing
  password-change tests prove by failing without it.

- **The interface failed WCAG AA on every screen.** Secondary text measured
  3.15:1 against the page background where AA asks for 4.5:1 — Mantine's default
  grey, chosen against pure white, on a page that is slightly grey. Badges and
  filled buttons were short too, and the shell emitted two `banner` landmarks
  and two `navigation` landmarks, one nested inside the other, so a screen
  reader offered indistinguishable duplicates and lost the ability to jump to
  the header. Section headings jumped from `h1` to `h4` or `h5`, breaking the
  outline people navigate by.

  Measured with axe across eleven pages: 12 to 33 violations each before, zero
  after. The colour values are asserted by a unit test against the same surfaces
  they are used on, so a future palette change fails a test rather than shipping.

- **The signed-webhook feature returned 500 on every installation.** Its two
  tables were declared by models and given a repository, but no migration
  created them, so every install newer than the versioning baseline answered the
  feature's endpoints with "no such table". `/readyz` only pings the database,
  so nothing ever paged. Migration 12 creates them.

- **The designer deployed processes that could not run.** Three separate ways,
  each silent: labelling a gateway path deployed the label as the path's
  *condition*, so a path captioned "Yes" carried the unbound condition `Yes` and
  was never taken; a "call a web address" step wrote its address and its
  external topic under names nothing reads, so the step deployed and did
  nothing at all; and the checks that would have caught an undecidable gateway
  lived in a module nothing imported, while the Deploy button consulted a much
  weaker one. Deploy is now held on a gateway whose paths carry no conditions
  and no fallback, and every message names the step and says what to do.

- **Unsaved work in the designer was thrown away.** The canvas autosaved to the
  browser every few seconds and never read it back, while the header said "last
  saved" — so a closed tab lost everything since the last *deploy*, which was
  the only way to save. The draft is offered back on open, Ctrl-S keeps it
  rather than publishing, and deploying clears it.

- **An outbound reply could exhaust the server's memory.** Service tasks and
  connectors read a whole HTTP response into memory with no ceiling, and the URL
  comes from a user-authored definition. Replies are now bounded and an
  oversized one is refused rather than truncated.

- **The expression cache stopped caching instead of evicting.** Past 4096
  distinct expressions nothing new was ever remembered again, so one tenant
  deploying that many conditions turned parsing back on for the whole
  installation, permanently and silently. It evicts least-recently-used now.

- **The decision editor had a dead end for a first-time author.** A new result
  column is created without a process variable, which is an error that disables
  Save — and the only control that fixed it was hidden behind Advanced → Expert.
  The variable is always visible now and fills itself in from the column
  heading. Saving also keeps you in the editor rather than returning to the
  list, so the try-fix-try loop is possible, and a trial run that matched
  nothing is no longer reported under a green tick.


- **Asking for something that is not there returned 500.** A well-formed
  identifier naming nothing reached GORM, came back as `ErrRecordNotFound`, and
  the HTTP encoder turns anything it does not recognise into a server error. So
  following a bookmark to a deleted instance, or asking for a task that had been
  completed and cleaned up, spent the roadmap's 0.1% 5xx budget and would page
  whoever is on call for a request the server had answered correctly.

  Measured: six of nine read endpoints did this. They return 404 now, and a
  caller can tell an empty answer from a missing one.

  This is the sibling of the malformed-identifier fix, which turned a caller's
  typo from a 500 into a 400. Absent and malformed are both the caller's
  business, and neither is the server failing.


## [0.2.0] - 2026-09-04

A minor rather than a patch because it changes which topologies are supported:
rate limits are now enforced across replicas, so raising the replica count is a
decision to make rather than something the product forbids. Circuit breakers
remain per-process, deliberately, and `docs/recovery.md` §2.1 says what that
costs.

### Added

- **Rate limits are enforced across replicas rather than per process.** Held in
  memory, N replicas each admitted the whole limit, so the installation admitted
  N times what was configured and a partner's per-minute quota was spent N times
  over. That was the specific cost that made a single replica the supported
  topology.

  Replicas now count locally and exchange totals every five seconds through a
  `shared_counters` table. The trade is a **bounded overshoot rather than an
  exact limit** — between exchanges a replica does not know what the others have
  counted, so up to one interval's worth per replica can slip through, roughly a
  twelfth of a per-minute limit. Reading a shared counter on every request would
  put a database round trip on the hottest path in the product, which would cost
  more than the limit protects.

  Measured across SQLite, PostgreSQL and MySQL: two replicas splitting one
  client's traffic admit 12 requests against a limit of 10, where before they
  admitted 20.

  **Circuit breakers remain per-process, deliberately.** They open on
  *consecutive* failures rather than on a rate — a downstream failing one call in
  ten is flaky, not down — and sharing a count would turn that back into a rate.

### Fixed

- **Feature flags are read at startup**, so the log states which are not at
  their default. They resolved lazily on first use, and their only two uses are
  deep in request paths, so an installation where nothing went wrong never
  logged what it had been configured with — and an operator turning on the
  strict tenant scope could not tell success from a mistyped variable name.


### Security

- **UI dependencies are audited in CI.** The Go side has had `govulncheck` from
  the beginning and the UI side had nothing. A first run reported 23 advisories,
  thirteen of them high — and every one was fixable within its existing range,
  which is the point: they were not unfixable, nobody had looked. Most were
  build-time (eslint, babel, vite) rather than shipped code, which lowers the
  severity and not the argument, since a compromised build tool ships
  compromised assets.

### Added

- **A security policy** ([`SECURITY.md`](SECURITY.md)). This repository is
  public and had no private way to report a vulnerability, so a finder's only
  option was an issue — which is a disclosure. It also states what counts as
  untrusted input, which deliberate decisions look alarming and are not, and
  what has already been audited.


## [0.1.4] - 2026-09-04

Two security fixes, both found by auditing rather than reported. The first is
the most serious this project has had: **upgrade past 0.1.3 if you accept
process definitions from anyone you would not give an approver's session to.**

### Security

- **A process definition could run JavaScript in the browser of whoever opened
  its task.** A user task's `form_definition` is a property of a node in a
  deployed definition, and its fields carry `logic.hiddenIf`,
  `logic.disabledIf`, `{{ … }}` defaults and a `validation.customJs` rule. All
  four were handed to `new Function` inside a `with` block and executed.

  The victim is normally an approver — by the nature of approval, someone with
  more authority than whoever modelled the form — and the code ran in their
  session, where the auth store and its token are reachable. Demonstrated
  against the previous build: a `hiddenIf` of
  `(globalThis.x = token, false)` read the session token, and the form rendered
  as though nothing had happened.

  The server has refused authored JavaScript in gateway conditions by default
  since FEEL landed, for exactly this reason. The browser is not a safer place
  to run it; it is where the session is.

  Form logic now goes through a bounded evaluator that can express comparisons,
  boolean logic and arithmetic over `data` and `vars`, and cannot express a
  function call, a property outside those two objects, an assignment, or any
  route to a global. A rule it cannot parse is refused and treated as false —
  the field stays visible and editable — rather than falling back to something
  that can run it. A test fails the build if `new Function` or `eval` reappears
  anywhere in the UI.

  **A `customJs` rule that is a comparison keeps working. One that is a program
  does not**, and the browser console says which rule was refused.


### Security

- **A connector's credentials could be written into an incident in plaintext.**
  A failed connector call carries the URL it was calling, and Go's `*url.Error`
  includes the query string — so a manifest that passes its API key as a query
  parameter, which many APIs require and which manifests are built to template,
  wrote that key into the incident table and the log on every connection
  failure. Incidents are kept and shown in the UI.

  The job service now redacts before storing or logging. The redactor already
  existed, already had patterns for `api_key=` and for URL userinfo, and was
  already applied to transport responses and setup — it was simply not called on
  the one path where a connector error becomes durable.


## [0.1.3] - 2026-09-03

Both entries below were found reviewing this session's own changes rather than
by a report — the setup one is a defect introduced by the secret validation in
0.1.2, and it is worth upgrading past.

### Fixed

- **The setup wizard could write a configuration the server refused to start
  with.** It applied a weaker rule than startup — sixteen characters for the
  encryption key, and nothing at all for the JWT secret beyond being present —
  so a wizard run could report success, save the config, and leave an
  installation that never came back after its first restart. Configured
  successfully and permanently unable to boot is a worse outcome than the weak
  secret the check was meant to prevent.

  Both paths now share one rule, and the wizard applies it *before* anything is
  encrypted with the key — which is the only moment the key can still be
  changed freely. `METIS_ALLOW_WEAK_SECRETS` is honoured in both places, so they
  cannot disagree in the other direction either.

- **Publishing an SSE event no longer starts a goroutine per event.** Fine while
  the database keeps up and unbounded when it does not: measured, five thousand
  events against a slow bus produced five thousand goroutines, each holding a
  payload and a pending write. The conditions that make a database slow are the
  conditions that make an engine busy, so it arrived when there was least room
  for it. Events now go through a bounded queue drained by a small pool, and
  what does not fit is dropped and counted — the same trade the local delivery
  already makes for a slow browser, and reported in the log rather than silently.


## [0.1.2] - 2026-09-03

### Security

- **Changing a password now ends every existing session.** It did not. The old
  password stopped working while every token minted with it stayed valid for the
  rest of its 24-hour life — so somebody changing their password *because they
  believed they were compromised* achieved nothing against the attacker already
  holding a session, and was told the change succeeded. Confirmed against the
  previous build: a token captured before the change still returned 200
  afterwards.

  Tokens issued before an account's last credential change are refused, for
  both the self-service change and `--reset-password`. The signed-in user is
  signed out too and asked to sign in again — that is the point rather than a
  side effect.

  Accounts that have not changed their password since upgrading are unaffected:
  the migration leaves the cutoff empty rather than filling it in, because
  filling it in would sign out every user on the installation at the moment of
  the upgrade.

### Added

- **The alerting rules are unit-tested.** `promtool check rules` proves the
  PromQL parses, not that it matches anything — a label that does not exist or a
  threshold on the wrong side parses perfectly and stays silent forever. Each
  rule is now asserted to fire on data that should trigger it and stay quiet on
  data that should not.

  Writing those tests found `MetisDown` matching `up{job=~".*metis.*"}`, which
  means an operator whose scrape job is named anything else gets an alert that
  never fires. `MetisMetricsMissing` is the backstop: it keys on a metric only
  Metis exports, so it holds whatever the job is called. **If it fires while
  `MetisDown` is silent, your scrape job is misnamed and `MetisDown` is not
  protecting you.**


## [0.1.1] - 2026-09-02

### Fixed

- **The published image is now `linux/amd64` and `linux/arm64`.** 0.1.0 shipped
  amd64 only, so Graviton and Ampere nodes — ordinary Kubernetes hardware — and
  every Apple Silicon developer had nothing to pull. The Dockerfile
  cross-compiles rather than emulating, so the second architecture costs almost
  nothing to build, and CI now builds it on every merge rather than leaving it
  to be discovered at tag time.

- **The release workflow could not publish at all.** It asked buildx for a
  provenance attestation, which the default `docker` driver cannot produce, so
  0.1.0 tagged and published nothing until the driver was set up. The workflow
  had never run before that tag: a release pipeline is not verified by existing.

## [0.1.0] - 2026-09-02

The first tagged release, and the first published image
(`ghcr.io/gsoultan/metis:0.1.0`).

**0.1.0 rather than 1.0.0, deliberately.** A single engine replica is the
supported topology — HTTP rate limiting and connector rate limits/circuit
breakers still hold per-process state, so with N replicas each of those limits
applies N times over ([`docs/recovery.md` §2.1](docs/recovery.md)). The strict
tenant scope, which makes a query carrying no identity return nothing instead of
everything, ships off pending a staged rollout
([`docs/strict-tenant-scope.md`](docs/strict-tenant-scope.md)). Both are written
down rather than discovered, and both are reasons the HTTP API and the metric
names are not yet being promised as stable.

What that number does *not* mean is untested. Read latency was measured at
500,000 instances and 500,000 tasks across 50 tenants and stayed under 5.2ms
against a 150ms target; the backup and restore procedure was rehearsed end to
end, including reading an encrypted variable back after a `DROP SCHEMA CASCADE`;
and every merge runs the suite against SQLite, PostgreSQL, MySQL and SQL Server,
builds the production image and boots it.

Everything below this heading shipped in it.

### Added

- **Signed-in users can change their own password**, from Profile → Change
  Password (`POST /api/v1/users/me/password`). Previously the only way was
  `--reset-password` from a shell on the server, so every rotation on a running
  installation needed access to the machine — including the administrator
  account the setup wizard creates, which could never be rotated by its owner.

  It asks for the current password, and that is the point rather than a
  formality: without it a stolen session token would be enough to lock the real
  owner out of their own account permanently. Accounts that sign in through
  OIDC are refused — their password lives at the identity provider, and Metis
  holds no hash their login consults, so "changing" it here would report
  success while altering nothing.

- **CI builds the image and boots it.** Nothing built the Dockerfile before,
  though the README calls it the supported artifact. The job now starts the
  built image against a real PostgreSQL with a read-only root filesystem and
  waits on `/readyz` — because the image is distroless, the failures that matter
  (a dynamically linked binary, a missing CA bundle, a moved entrypoint) all
  produce something that builds cleanly and exits on first run.

### Changed

- **GoBPM is now Metis.** The module path is `github.com/gsoultan/metis`, the
  binary and container entrypoint are `metis`, and the repository moved with
  them. Existing installations keep running without reconfiguration: `GOBPM_*`
  environment variables are still read (with a warning naming the replacement),
  an existing `gobpm.db` is still opened, and browser sessions are migrated
  rather than dropped. See [`docs/upgrading.md`](docs/upgrading.md) for what to
  change and when the fallbacks go.

  The one thing that does need editing is the import path, and the Go client's
  package name along with it — `gobpm.NewClient` is now `metis.NewClient`.

  **Metrics and traces are renamed with no fallback.** They are now
  `metis_http_requests_total`, `metis_http_request_duration_seconds`,
  `metis_http_requests_in_flight`, and the OTel service, span and attributes are
  `metis.*`. A dashboard or alert rule still written against `gobpm_` does not
  error — it matches nothing and stops firing, which is harder to notice than a
  break. Grep your alerting rules before upgrading.

  The `Idempotency-Key` sent on outbound service calls still begins with
  `gobpm-`, deliberately and permanently. It is derived fresh on every retry, so
  it is the only thing telling a downstream system that a retry is the request
  it already handled; renaming it would make a job caught mid-retry by the
  upgrade arrive looking new, and charge the card twice.

### Security

- **A weak `ENCRYPTION_KEY` or `JWT_SECRET` is refused at startup.** Both were
  accepted on the single condition of being non-empty, so `JWT_SECRET=secret`
  started a server whose administrator tokens could be forged offline.

  Both must now be at least 32 characters — what `openssl rand -hex 16`
  produces, which is what the documentation has always recommended — and must
  not be one of the placeholders published in this repository. That last check
  matters more than the length one: the evaluation `ENCRYPTION_KEY` in
  `docker-compose.yml` is exactly 32 characters, so it passes every length rule
  while being known to anyone with the repository, and a compose file is the
  easiest thing to carry from an evaluation into production.

  **Upgrading with a weak key is a real possibility, and `ENCRYPTION_KEY` cannot
  simply be rotated** — changing it does not re-encrypt anything, it makes
  existing variables unreadable. `METIS_ALLOW_WEAK_SECRETS=true` starts the
  server anyway, warning on every boot, so the remedy is a planned
  re-encryption rather than an outage.


- **Service tasks could reach internal addresses through a hostname.** The
  egress guard checked IP ranges, but only when a URL literally contained an IP:
  for a hostname `net.ParseIP` returned nil and the checks were skipped
  entirely. A process definition — untrusted input — naming a host whose address
  record pointed at `169.254.169.254` reached the cloud metadata endpoint, and
  one pointing at `127.0.0.1` reached anything bound to loopback. Confirmed
  against the previous code by reading the body of a loopback-only server
  through a public hostname.

  The check now runs on the resolved address immediately before connect, which
  also closes DNS rebinding: there is no second lookup between the check and the
  connection. `100.64.0.0/10` is refused as well — Go's `IsPrivate` does not
  cover it, and several Kubernetes CNIs put internal service networks there.

  A host named in `METIS_HTTP_ALLOWED_HOSTS` still reaches its private address:
  that is what the setting is for.


- **The HTTP rate limiter could be bypassed by setting a header.** The limiter
  keyed its buckets on `X-Forwarded-For` and believed it unconditionally. That
  header is set by the client, so sending a different value on every request
  bought a fresh allowance every time: measured against the previous code, one
  address took 30 requests through a limit of 3, and only stopped because the
  test stopped.

  The header is now consulted only when the request arrived from a peer allowed
  to set it — loopback and private space by default, configurable with
  `METIS_TRUSTED_PROXIES`, and `none` for a directly-exposed server. Anything
  else is charged to the address it actually connected from.

  **If your load balancer sits in public address space, set
  `METIS_TRUSTED_PROXIES` to its range**, or every client behind it will share
  one bucket.


- **`js:` gateway conditions are refused by default.** The JavaScript runtime
  behind them cannot be memory-bounded — a single native call ran for 37s
  against a 200ms budget, allocating throughout — so a default installation was
  one deployed definition away from memory exhaustion. FEEL has evaluated
  conditions natively since the expression layer landed.

  **Upgrading:** a definition still using `js:` will refuse to route, loudly,
  with an error naming the condition. `GET /api/v1/definitions/javascript-conditions`
  lists every affected definition. Set `METIS_FEATURE_JAVASCRIPT_CONDITIONS=true`
  to keep the old behaviour while migrating.

- **Idempotency keys are scoped to the caller.** The cache was keyed on method,
  path and the header value alone. Because clients choose their own keys and
  choose obvious ones, two tenants using the same value shared a cache entry:
  the second was served the first's response body and its own write never ran.
  Keys are now namespaced by tenant and user.

- **A malformed identifier is a 400, not a 500.** Forty-two endpoints returned
  the raw parse error for a bad UUID, which the transport mapped to a server
  error — spending the 0.1% 5xx error budget on client typos and paging whoever
  was on call for requests answered correctly.

### Fixed

- **FEEL's `matches()` was not a regular expression.** It matched literally, so
  a decision table written with `matches(code, "^ERR-[0-9]+$")` did not error
  and did not warn — it answered false for every input and the process took the
  other branch. A rule that never fires is a wrong answer delivered
  confidently, not a missing feature.

  It matched literally to avoid catastrophic backtracking on a pattern taken
  from a deployed definition. That reasoning does not apply to Go, whose
  `regexp` is RE2 and does not backtrack: the textbook `^(a+)+$` case measures
  119µs rather than hanging.

  **Patterns containing no metacharacters behave exactly as before** — Go's
  matching is unanchored, so a literal pattern is still a substring test. A
  definition using a real pattern starts working, which is a behaviour change if
  you were relying on the rule never firing. An uncompilable pattern is now an
  error rather than a silent false.


- **A client retry landing on another replica no longer re-executes the write.**
  Idempotency records were held in the serving process, so a second replica
  found an empty cache and ran the work again — a duplicate business action,
  which is what the header exists to prevent. Records now live in
  `idempotency_records` (migration 8), claimed with a single conditional insert
  so exactly one replica executes and the rest replay its answer. Proven across
  SQLite, PostgreSQL and MySQL.

  This closes the only multi-replica gap that could corrupt data. Four remain
  and are degradations rather than corruption — see `docs/recovery.md` §2.1.

- **The PostgreSQL advisory lock never released.** Advisory locks are
  session-scoped and the locker held a connection *pool*, so the release ran on
  a session that held nothing, reported success, and left the lock held on an
  idle pooled connection. It now pins a session per lock.

- **A job could be claimed and then stranded.** The claim wrote a five-minute
  lease before taking the distributed lock, so a refused lock left a job marked
  running that nobody was running. The lock is now taken first, and released if
  the row race is then lost.

- **The bundle budget was calibrated on the wrong platform.** The same tree
  measures ~2.4 kB larger on Linux than macOS, against a budget with 1.5 kB of
  headroom — so the check passed on a laptop and blocked the release image.

### Added

- **Connector manifests can authenticate with OAuth client credentials.** The
  schema accepted `oauth2_client_credentials` and the runtime then refused it,
  so a manifest could be written, validated and installed and would fail on its
  first call. Tokens are fetched once and reused until shortly before expiry;
  concurrent callers share one fetch rather than each starting their own.

- **A production image.** Three stages (bun builds the UI, Go builds a static
  binary, distroless runs it as non-root on a read-only root filesystem), plus
  a compose file for evaluation. `make docker`, `make docker-run`.

- **`/healthz` reports the running build**, so "which version is this" no longer
  depends on finding a startup log line.

- **Backup and restore scripts** (`scripts/backup.sh`, `scripts/restore.sh`)
  implementing the procedures in `docs/recovery.md`. The backup refuses to run
  without `ENCRYPTION_KEY`; the restore refuses to start until the operator
  confirms the engine is stopped.

- **The strict tenant scope says which path forgot an identity.** It answers an
  unidentified query with nothing rather than an error, which is the right
  contract but made the flag hard to adopt: a background path that forgets to
  mark itself does not fail, it goes quiet, and an operator evaluating it in
  staging had to notice an absence. Each denial now logs the repository method
  and its caller, once per site, so the rollout is reading a list instead.

- **CI now runs every dialect-gated suite, and a guard keeps it that way.**
  `tests/migrations`, `tests/replicas` and `tests/outage` need a real database
  but were absent from the dialects job, so they skipped for want of a DSN,
  reported ok, and were never run against PostgreSQL or MySQL by anything —
  including the conditional-insert claim that had a MySQL-only bug when it
  landed. `tests/ci` now fails when a dialect-gated package is missing from that
  job, so the next one cannot be forgotten silently.

- **Connector contract tests** (`tests/connector/contract_test.go`) — the test
  pyramid's last missing tier. They pin what each connector puts on the wire and
  how it reads what comes back, including that a manifest's error rules decide
  before its success condition, and that the outbound egress policy refuses a
  private address by default.

- **SLO tests** (`tests/slo`). The roadmap's targets now fail a build when
  missed. Measured against PostgreSQL 17: reads p95 11.1ms (target 150ms),
  workflow actions p95 13.8ms (target 500ms), 0.000% 5xx (budget 0.1%),
  170,569 process starts/min (target 10,000/min).

- **Strict-tenant-scope integration coverage** (`tests/strictscope`), entering
  through the real HTTP chain and job worker, so the flag can be turned on in
  staging with evidence behind it. `make strict-scope`.

### Changed

- **The supported topology is a single engine replica**, now stated in
  `docs/recovery.md` §2.1 along with what breaks with two and why. Job claiming,
  migrations, correlation and outbound-call idempotency are already
  replica-safe; rate limiting, the idempotency cache, SSE delivery, connector
  breakers and the AMQP bridge are not.

- The designer's condition builder emits FEEL rather than JavaScript, and shows
  an expression too complex for its three fields instead of silently replacing
  it.
