# Integrating with Metis

Everything below is the real wire contract: each call is exercised end to end
by the Go SDK's `examples/quickstart`, which runs the whole journey against a
live server. If this document drifts from the API, that program is the test
that fails.

There are four ways an application integrates with the engine, and they
compose:

| You want to | Mechanism |
| :-- | :-- |
| Start work in Metis from your app | deploy a definition, start instances |
| Tell a running process something happened | messages and signals |
| Show Metis's human tasks in your own UI | the task API |
| Have *your* service do a process step | external-task workers |

## Authentication

Every call except `/api/v1/login` and the setup endpoints carries a bearer
token.

```bash
TOKEN=$(curl -s -X POST $GOBPM/api/v1/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"…"}' | jq -r .token)
```

Accounts that belong to several organizations choose one per request with the
`X-Organization-ID` header. The server validates the choice against the
caller's actual memberships — it is a selection, never an assertion.

## The Go SDK

```bash
go get github.com/gsoultan/metis-sdk
```

The SDK lives in its own repository,
[gsoultan/metis-sdk](https://github.com/gsoultan/metis-sdk), and has **no
dependencies outside the standard library** — importing it does not pull the
engine's dependency graph into your build. It versions independently of the
server; see its README for the full client reference.

```go
client := metis.NewClient("https://bpm.example.com")
if err := client.Login(ctx, "admin", password); err != nil { … }
// or, when the token comes from a secret store:
client = metis.NewClient(url, metis.WithToken(token))
```

## Deploy and start a process

```go
projects, _ := client.ListProjects(ctx)                       // find the project ID
defID, _ := client.ImportDefinition(ctx, projectID, bpmnXML)  // BPMN 2.0 XML
instanceID, _ := client.StartProcess(ctx, projectID, "refund",
    metis.Variables{"amount": 42.50})
```

```bash
curl -X POST $GOBPM/api/v1/definitions/import \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"project_id\":\"$PROJECT\",\"xml\":\"$(base64 < refund.bpmn)\"}"

curl -X POST $GOBPM/api/v1/process/start \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"project_id\":\"$PROJECT\",\"definition_key\":\"refund\",\"variables\":{\"amount\":42.5}}"
```

Redeploying the same process key creates the next version; running instances
keep the version they started with. The import **requires** `project_id` — a
definition without a project would be invisible to its own organization under
tenant scoping.

## Messages and signals

A **message** is addressed: the correlation key picks which waiting instance
receives it. A **signal** is broadcast to every instance in the project
waiting on it.

```go
err := client.SendMessage(ctx, projectID, "payment.received", orderID,
    metis.Variables{"paid": true})
err = client.BroadcastSignal(ctx, projectID, "quarter.closed", nil)
```

Careful with an empty correlation key: it matches every waiting subscription
for that message name. Pass one unless the name alone is genuinely
unambiguous.

## Human tasks in your own UI

```go
tasks, page, _ := client.ListTasks(ctx, metis.ListTasksOptions{PageSize: 50})
err := client.ClaimTask(ctx, task.ID)                 // so nobody works it twice
err = client.CompleteTask(ctx, task.ID, metis.Variables{"approved": true})
err = client.UnclaimTask(ctx, task.ID)                // or give it back
```

**Who acts is the token.** Claiming and completing take no user argument: the
server reads the acting user from the `Authorization` header and ignores any
override, so an application acting for many people needs a client per person
rather than one client passing user IDs around. To hand a task to somebody
specific there is `AssignTask`, allowed for an administrator or for whoever
currently holds the task.

Completing writes the variables back into the process and the instance moves
on. The instance's story is readable as plain language:

```go
entries, _ := client.GetTimeline(ctx, instanceID)
// "Task \"Approve the refund\" became available", "admin claimed …", …
```

## External-task workers: your service does the step

Mark a service task with a topic — in the designer, or in BPMN XML:

```xml
<serviceTask id="charge" name="Reverse the charge" topic="reverse-charge"/>
```

The engine then *publishes* that step as work instead of calling out, and your
service pulls it. The pull model means the engine never needs network access
to your workers — they can live behind any firewall that can reach the
server.

```go
worker := metis.NewWorker(client, "reverse-charge", "billing-service-1",
    metis.WorkerOptions{},          // sensible defaults; see WorkerOptions
    func(ctx context.Context, task *metis.ExternalTask) (metis.Variables, error) {
        // Typed accessors, because JSON has one number type: every number
        // the engine sends is a float64, so a .(int) assertion would panic.
        amount, ok := task.Variables.Float64("amount")
        if !ok {
            return nil, fmt.Errorf("task %s carries no amount", task.ID)
        }
        // … call your payment provider …
        return metis.Variables{"reversed": true}, nil
    })
log.Fatal(worker.Run(ctx))          // polls until ctx is cancelled
```

Semantics worth knowing before production:

- **The handler's budget is the lock.** Its context is cancelled when the
  lock would expire, because past that point another worker may hold the same
  task, and two workers charging the same card is the failure this model
  exists to prevent.
- **Handlers must be idempotent.** If the work succeeds but reporting back
  fails, the lock expires and the engine re-dispatches the task.
- A returned error fails the task with one retry spent; when retries run out
  it stays failed for an operator.
- A panicking handler fails that one task; the worker keeps serving the rest.

The raw protocol, for any language that speaks HTTP:

```bash
curl -X POST $GOBPM/api/v1/external-tasks/fetch-and-lock \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"topic":"reverse-charge","worker_id":"billing-1","max_tasks":5,"lock_duration_ms":60000}'

curl -X POST $GOBPM/api/v1/external-tasks/$TASK_ID/complete \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"worker_id":"billing-1","variables":{"reversed":true}}'

curl -X POST $GOBPM/api/v1/external-tasks/$TASK_ID/failure \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"worker_id":"billing-1","error_message":"card declined","retries":2,"retry_timeout_ms":10000}'
```

Durations on this API are always suffixed `_ms` — the unit is part of the
field name because an unsuffixed `lock_duration` has already been misread
once inside this codebase.

## Watching instances

```bash
curl -H "Authorization: Bearer $TOKEN" $GOBPM/api/v1/instances/$ID        # status + variables
curl -H "Authorization: Bearer $TOKEN" $GOBPM/api/v1/instances/$ID/audit  # the timeline
curl -H "Authorization: Bearer $TOKEN" $GOBPM/api/v1/instances/$ID/path   # execution path + frequencies
```

`GET /api/v1/events` is a server-sent-events stream for live updates, which
is how the built-in UI avoids polling.

## Retrying your own calls safely

The write endpoints accept an `Idempotency-Key` header, so a client that loses a
response can retry without wondering whether the first attempt landed.

```
POST /api/v1/process/start
Idempotency-Key: order-4471-start
```

Send any value that identifies *the command you are issuing*, not the attempt: a
retry must carry the same one. A repeated request returns the original response
with `Idempotency-Replayed: true` and does not execute again. Reusing one key
with a *different* body is refused with `409 Conflict` — that is almost always a
client bug, and executing it would make the key meaningless.

Records are kept for 15 minutes, which is a retry window rather than a
deduplication guarantee; a retry an hour later is a new command.

**Keys are scoped to you.** They are namespaced by tenant and user, so choosing
an obvious value — an order number, `1` — cannot collide with another customer's
key. Only reads are exempt: `GET` and `HEAD` ignore the header entirely.

**One caveat:** these records live in the serving process, not the database, so
they hold for a single engine replica — the supported topology today. See
[`recovery.md` §2.1](recovery.md).

## Your endpoint will be called twice, eventually

A service task's call and the transaction that records its result cannot share a
transaction: the call is network I/O, and the record takes a row lock on the
process instance. Holding that lock across someone else's API is how one slow
partner stalls a whole engine. So they are separate — and a process that is
interrupted between them retries, because from the engine's side a request that
never arrived and a response that was lost look identical.

The engine does three things about it, and needs one thing from you.

**It remembers.** Every outbound call is recorded before it is made and completed
with its response afterwards. A retry that finds a completed record reuses the
response and makes no call at all. This is the common case and it is handled
entirely on our side.

**It sends a key.** Every call carries an `Idempotency-Key` header:

```
Idempotency-Key: Metis-<32 characters>
```

The key is derived from the unit of work — the process instance, the node, and
the iteration for a node that runs once per item — not from the attempt. It is
identical on every retry, survives a restart, and is different for every other
call the engine makes.

**It says so.** A repeated call is logged as a warning naming the instance, the
node, the attempt count and the key.

**What we need from you:** treat two requests carrying the same
`Idempotency-Key` as one. Store the key with the result, and when you see it
again return the first result rather than doing the work twice. This matters most
for anything that moves money, sends a message or creates a record — for a read,
it costs nothing to ignore.

If your API already implements the Stripe-style idempotency convention, it works
with no changes.

## When your endpoint is down

Five consecutive failures against the same target — a connector instance, or a
host for a plain HTTP task — and the engine stops calling it for thirty seconds.
Calls in that window fail immediately rather than waiting for a timeout, so an
outage in one integration does not fill the job pool and stall every unrelated
process. After the cooldown one call goes through to find out whether you are
back; if it works the breaker closes, if it does not the cooldown starts again.

Instances still fail and still retry — starting at 30 seconds, doubling, capped
at 15 minutes, with a quarter of each delay randomised so two thousand instances
that failed against the same outage do not all come back at the same moment.
When the attempts run out the job raises an incident, which is visible in the UI
and resolvable there.

## Holding to your rate limit

Set `rate_limit_per_minute` on a connector instance's configuration — or on a
service task's properties, for a plain HTTP call — and the engine will not exceed
it. The limit is a token bucket: a minute's worth of burst, refilling steadily,
which is the shape API quotas are usually written in.

A call held back by the limit is **deferred, not failed**. The job is put back
with a time on it and its retry count is untouched, because being over a quota is
compliance rather than an error — counting it as a failure would exhaust an
instance's three attempts for the crime of being popular.

A connector instance's setting covers every node that uses that connection, so a
limit agreed with a partner is set once. Leave it unset — the default — and
nothing is limited.

## Receiving events: webhooks

A partner announcing that something happened — a payment cleared, a ticket
closed — posts to an address you register:

```
POST /api/v1/hooks/<token>
X-Signature-256: sha256=<hmac>
X-Delivery-Id: <the sender's id for this event>

{"order": {"id": "ORD-1"}}
```

The endpoint is public, because a partner's configuration screen has nowhere to
put a token this engine would recognise. What authenticates a delivery is the
signature: **HMAC-SHA256 over the exact bytes of the body**, hex-encoded, using
the secret you were given when the webhook was created.

```
signature = hex(hmac_sha256(secret, raw_request_body))
```

An `sha256=` prefix is accepted and ignored — the algorithm is ours, not the
caller's to declare. The header name is configurable per webhook, because there
is no standard: GitHub uses `X-Hub-Signature-256`, Stripe uses
`Stripe-Signature`.

**The secret is shown once**, when the webhook is created. It is encrypted at
rest and no read path returns it.

**Send a delivery ID.** Any of `X-Delivery-Id`, `X-GitHub-Delivery`,
`X-Request-Id` or `Idempotency-Key`. A delivery carrying an ID already seen is
answered `202` and not acted on again — senders retry, and without an ID a retry
cannot be told from a new event and will move the process twice. IDs are
remembered for 48 hours.

Each webhook names the BPMN message a delivery becomes, and optionally a FEEL
expression over the payload — `order.id` — picking the value that says which
waiting instance it concerns. Leave that empty and every delivery starts a
process instead of moving one.

Responses: `202` accepted, `401` for anything about who sent it (an unknown
address and a bad signature are deliberately indistinguishable), `400` for a
body that could not be used.

## Letting a decision table say who approves

An approval matrix — "under 10k the team lead, over 10k the CFO, anything from a
new supplier goes to compliance" — changes when the organisation changes, which
is far more often than the process does. Written into a diagram, moving a
threshold takes a modeller and a redeploy.

Give a user task an `assignment_decision_key` property naming a decision table,
and its outputs set who does the work:

| Output column | Sets |
| :-- | :-- |
| `assignee` | the person it goes to |
| `candidate_users` | who may claim it — a list, or one comma-separated cell |
| `candidate_groups` | which groups may claim it |
| `priority` | a number |
| `due_date` | an instant, or an ISO-8601 duration such as `PT4H` from when the task appears |

Only what the table actually returns is applied — a table that decides the group
and not the priority leaves the priority as the diagram set it, and a task whose
table decides nothing behaves exactly as before. An empty output is a table with
nothing to say, not an instruction to unassign.

If the table cannot be evaluated the diagram's own assignment stands and the
failure is logged: a process that stops because an approval matrix could not be
read is worse than one that routes to the default approver.

The choice lands on the instance timeline, naming the table, its version and the
line that applied — "why did this land on the CFO's desk?" is the question asked
about approvals more than any other.

## Looking something up in your own database

A decision is only as good as the data the process carries, and the data that
matters — a customer's tier, an account's balance, how many invoices are open —
usually lives in a database of your own. The **Database Lookup** connector reads
it directly, so a step can fetch it and the gateway or decision table after it
can use it, without an API written in front of the database first.

### Setting up the connection — an administrator, once per project

On the **Connectors** page, connect *Database Lookup*:

| Setting | |
| :-- | :-- |
| Database | `postgres`, `mysql` (MySQL or MariaDB), or `sqlserver` |
| Connection string | as that database's driver takes it — `postgres://user:pass@host/db`, `user:pass@tcp(host:3306)/db`, `sqlserver://user:pass@host:1433?database=db`. Stored encrypted, and never sent back to a browser. |
| Time limit | milliseconds, default 5000, at most 30000. The database stops a query that runs longer. |
| Most rows | default 500, at most 10000. |
| Most data | bytes, default 262144, at most 1048576. The answer is stored with the process. |

**Connect as a login that can read only the tables your lookups need.** The
queries are written by whoever designs the process, so that login's permissions
are the boundary that matters most — everything else below is a second line
behind it. On PostgreSQL:

```sql
CREATE ROLE metis_lookup LOGIN PASSWORD '...';
GRANT CONNECT ON DATABASE crm TO metis_lookup;
GRANT USAGE ON SCHEMA public TO metis_lookup;
GRANT SELECT ON customers, invoices TO metis_lookup;
```

A connection whose login can see Metis's own tables is refused before any
query runs: pointed at Metis's database, a lookup would read every project's
instances and every connection's settings.

**Test** on the Connectors page opens the connection with every check a lookup
gets — the host list, the settings it forces, the refusal above — and runs
nothing of anybody's. On the designer, **Try it** runs one step's real query
against the project's saved connection and shows what it would store; that
needs the Query author role, as deploying does.

### The step — whoever designs the process

Drag *Database Lookup* from the designer's **Connectors** group onto the canvas
and fill in:

- **Query** — one `SELECT` (or `WITH ... SELECT`). Write each value that comes
  from the process as `:name`:

  ```sql
  SELECT name, tier, credit_limit FROM customers WHERE id = :customer_id
  ```

- **Values** — for each `:name`, the process value it takes: a variable name,
  or a FEEL expression such as `order.customer.id`. The designer offers each
  `:name` the query uses as a one-click row. A value that is a list expands to
  one parameter per item, for `WHERE id IN (:ids)`; an empty list is refused
  rather than matching nothing, and a list may hold at most 1000 values.
- **Store the answer as** — one variable name, say `customer`.

The answer is one variable:

| | |
| :-- | :-- |
| `customer.row` | the first row — absent when nothing was found |
| `customer.rows` | every row |
| `customer.row_count` | how many |
| `customer.truncated` | whether a limit stopped the read |

So the gateway after it reads `customer.row.tier = "gold"`, and a decision
table takes `customer.row.credit_limit` as an input. Finding nothing is an
answer, not an incident: `customer.row_count = 0` is how a gateway asks.

Numbers stay numbers, so `credit_limit > 1000` compares numbers. An integer too
large for a JSON number, or a decimal of more than fifteen significant digits,
arrives as text rather than as a slightly different number. Dates arrive as
`2026-09-24`, timestamps as RFC 3339, and JSON columns as the structure they
hold.

### Who may deploy one

A lookup carries SQL its author wrote, so deploying a process with one in it
needs the **Query author** role, held beside Designer — an administrator has it
already. Grant it on the **Platform access** page. A designer without it can
still design the step; the deploy is refused, and says why.

### What is checked, and what that is worth

- Values from the process are only ever sent as parameters, never written into
  the query.
- The query must be one statement that reads. Anything that writes, changes a
  schema, runs other code, reads files or reaches another server is refused —
  anywhere in the query, not only at the start — and so are comments.
- On **PostgreSQL** and **MySQL** the query runs in a read-only transaction the
  database itself enforces, and one query can only ever be one statement.
- **SQL Server has no read-only transaction.** There the statement check and the
  login's own permissions are what stand in the way; every lookup's transaction
  is rolled back rather than committed, so a change to a table that got past
  both is undone, but one with effects outside the database is not. On SQL
  Server, a read-only login is not a recommendation.
- On SQL Server, the check for Metis's own tables sees only the database the
  connection opens. Do not give the lookup's login access to Metis's database on
  the same server.

### For whoever runs Metis

| Variable | Default | |
| :-- | :-- | :-- |
| `METIS_SQL_LOOKUP_MAX_CONNS` | 4 | connections one Metis node holds to one lookup database. Across a cluster, multiply by the nodes — that is the number to agree with whoever runs that database. |
| `METIS_SQL_LOOKUP_MAX_POOLS` | 32 | lookup databases one node keeps a pool open to. |
| `METIS_SQL_LOOKUP_ALLOWED_HOSTS` | any | comma-separated hosts a lookup may reach. On a shared installation the project administrator who sets up a connection is not whoever runs the servers; list the hosts to stop one project pointing a lookup at any database the server can reach. |

An idle node gives its connections back after two minutes.

## Writing a connector without writing Go

A connector is a document. It says what to call, how to authenticate, what goes
in, what comes back, and which failures are which:

```yaml
key: salesforce.create-lead
version: 2
name: Salesforce — Create Lead
auth:
  type: bearer
request:
  method: POST
  url: "{{config.instance_url}}/services/data/v60.0/sobjects/Lead"
  body:
    LastName: "{{input.last_name}}"
    Company:  "{{input.company}}"
response:
  success: "status >= 200 and status < 300"
  outputs:
    lead_id: "body.id"
errors:
  - when: "status = 401"
    bpmn_error: AUTH_FAILED
    retryable: false
  - when: "status = 429"
    bpmn_error: RATE_LIMITED
    retryable: true
    retry_after: "headers['Retry-After']"
```

**Authentication** is one of `none`, `basic`, `bearer`, `api_key` or
`oauth2_client_credentials`. The first four read what the tenant configured on
the connector instance (`username`/`password`, `token`, `api_key`). OAuth takes
its token URL from the manifest and its credentials from the instance:

```yaml
auth:
  type: oauth2_client_credentials
  token_url: "https://login.example.com/oauth2/token"
  scopes: [read, write]
```

with `client_id` and `client_secret` — plus `audience` for providers that need
one, and `scope` to narrow what the manifest asks for — configured on the
instance. Tokens are fetched once and reused until shortly before they expire,
and callers arriving during a refresh wait for it rather than each starting
their own. The token URL is deliberately not configurable: it is the connector
author describing the API, and letting an instance override it would let whoever
configures one redirect the credentials elsewhere.

`{{ … }}` is the only syntax this adds. Everything inside is FEEL — the same
language gateway conditions, input mappings and decision cells use.

**`config` is the tenant's, `input` is the node's.** Credentials come from
config and are unreachable from input, so a modeller cannot read one by mapping
it into a variable.

**A template that is entirely one expression keeps its type.** `{{input.amount}}`
is the number 500, not the text "500" — a body field arriving as a string is
rejected by most APIs that care. A template that resolves to nothing is omitted
rather than sent as null, because most APIs read an explicit null as "clear this
field".

**`errors[]` turns an HTTP failure into a BPMN error** a boundary event can
catch, so an integration failing becomes a modelled path rather than an incident
somebody has to read a stack trace to understand. Rules are checked before the
success condition, so a failure that arrives with a 200 and a body saying so is
still caught. `retry_after` is honoured — being asked to wait two minutes and
waiting two minutes is the difference between backing off and being blocked.

Manifests are consulted before the built-in connectors, so one can replace a
built-in without a redeploy, where the operator allows it (below). Genuinely
code-shaped connectors — an SDK, a stream, anything stateful — keep the Go
interface.

## Installing a connector

`POST /api/v1/connector-manifests` with `{"document": "...", "format": "manifest"}`
installs one; `"format": "openapi"` installs one per operation in a
specification. Both are in the UI, on the Connectors page.

**Who may.** A manifest is installation-wide: a step in any organization that
names its key runs it, with that organization's connection attached. So
installing, importing, switching and removing one changes what every
organization runs, and on an installation with more than one organization it
takes a **platform administrator** — an administrator whose account id whoever
operates the installation has listed in `METIS_PLATFORM_ADMINS`. Anybody else
is refused with a 403 that names the setting and gives them their account id to
pass on. An installation with one organization needs nothing configured: its
administrators may, as they always could.

The same goes for **connector templates** (`/api/v1/connectors`), for the same
reason. A template has no organization: its key is unique across the
installation, and every organization's connections are configured through its
schema, which is what marks a setting as a password.

A manifest is stored as its author wrote it and read back the same way, comments
and all. Installing an existing key **replaces** it, because installing again is
how a manifest is fixed. It keeps the switch it had: a connector somebody
switched off stays off when its document is fixed, and only a new one is
installed switched on. A document whose `version` is lower than the installed
one is refused with a 400 naming both — the same version again is a fix and a
higher one an upgrade, but going back is almost always a stale copy.

**A built-in's key.** A manifest under the key of a connector built into Metis —
`http-json`, `slack-message`, `email-smtp`, `sendgrid-email`,
`discord-message`, `ms-teams-message`, `rabbitmq-publish` or `sql-query` —
replaces that connector in every step, in every organization, that uses it. So
installing one is refused with a 400 naming the key unless the operator sets
`METIS_ALLOW_BUILTIN_CONNECTOR_OVERRIDE=true`. A manifest installed under a
built-in's key before this rule keeps answering; removing it hands the key back
to the built-in.

Manifests are read from the database on every call rather than cached, so a
connector installed on one replica is live on all of them immediately, and a
switched-off one stops being used everywhere at once. Switching off leaves the
document in place — deleting loses it.

Installing a connector adds it to the catalogue: on the Connectors page, where a
project connects it, and in the designer, where a step chooses it. A step
reaches a manifest only this way — it names a catalogue entry, the entry's
connection supplies `config`, and the entry's key finds the manifest. The
connection form asks for what the manifest reads from `config`: the credentials
its `auth` needs (`token`, `api_key`, `username` and `password`, or `client_id`
and `client_secret`), every property of `config_schema` — its `title`,
`description`, `type`, `enum`, `default` and `required` become the field — and
any other `{{config.…}}` a template reads. A setting that holds a credential is
kept from the browser by its **name**, so include `secret`, `password`, `token`
or `key` in it; `format: password` alone does not hide it, and the form does not
pretend otherwise.

Switching a connector off or removing it takes it out of the catalogue. The
steps and connections that use it are kept; while it is gone they fail saying
so, and they work again once it is switched back on or installed again. A
manifest under a built-in's key leaves the built-in's entry as it is. A manifest
installed before this behaviour arrived joins the catalogue the next time it is
installed — the same document again will do.

## Importing a connector from an OpenAPI document

Most APIs worth integrating with publish one, and a manifest is close enough to
one operation in that document that the translation is mechanical. Point the
importer at a spec and it produces one connector per operation:

- `/pets/{petId}` becomes `{{input.petId}}` in the URL, using the name the spec
  already uses;
- query and header parameters become templates, and every parameter joins the
  connector's input schema — which is what a form can be drawn from;
- a JSON request body becomes a body template, one level deep (a nested shape is
  left to you: guessing at it produces a template that looks right and is wrong);
- the success response's fields become outputs;
- the first security scheme becomes the auth;
- and both failures every API has — `AUTH_FAILED` and `RATE_LIMITED`, the second
  honouring `Retry-After` — are added as catchable BPMN errors.

The server URL becomes `{{config.base_url}}` when the spec gives none or gives
one with placeholders of its own, because the same spec is used against
production and a sandbox.

What comes out is a starting point, not a finished connector: you will rename
things and delete the nine operations in ten you do not want. But it already
calls the right endpoint with the right shape.

An operation the importer cannot read is skipped, not an error. What it did
generate is installed as one step: if any of it cannot be installed — an
operation you took further and gave a higher `version` than the import's 1, for
instance — none of it is, and the error names the operation that stopped it.

Each operation is its own entry in the catalogue, so a project connects each one
it uses — with the same `base_url` and credentials, which is one more reason to
delete the operations you will not call.

## Errors

Failures are JSON with an HTTP status: `{"error": "…"}`. The SDK surfaces
them as `*metis.APIError`, with four predicates that say what a status means
here rather than leaving you to compare codes:

```go
switch {
case metis.IsNotFound(err):     // gone — or, under tenant scoping, never yours
case metis.IsUnauthorized(err): // 401 or 403: expired, missing, or not allowed
case metis.IsInvalid(err):      // 400: the request was wrong; retrying will not help
case metis.IsServerError(err):  // 5xx: the engine faltered; worth retrying
}
```

Under tenant scoping, another organization's resource answers **404, not 403** —
"not yours" and "does not exist" are deliberately indistinguishable, because a
403 would confirm the thing exists. The engine does not answer 409.

## Run the whole journey

```bash
METIS_URL=http://localhost:8080 METIS_USERNAME=admin \
METIS_PASSWORD=… METIS_PROJECT="Default Project" \
  go run github.com/gsoultan/metis-sdk/examples/quickstart
```

It deploys a definition, starts an instance, serves its external task with a
worker, completes its human task, and prints the timeline.
