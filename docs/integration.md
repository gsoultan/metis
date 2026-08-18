# Integrating with gobpm

Everything below is the real wire contract: each call is exercised end to end
by `sdk/examples/quickstart`, which runs the whole journey against a live
server. If this document drifts from the API, that program is the test that
fails.

There are four ways an application integrates with the engine, and they
compose:

| You want to | Mechanism |
| :-- | :-- |
| Start work in gobpm from your app | deploy a definition, start instances |
| Tell a running process something happened | messages and signals |
| Show gobpm's human tasks in your own UI | the task API |
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
go get github.com/gsoultan/gobpm/sdk
```

The SDK is a separate module with **no dependencies outside the standard
library** — importing it does not pull the engine's dependency graph into
your build.

```go
client := gobpm.NewClient("https://bpm.example.com")
if err := client.Login(ctx, "admin", password); err != nil { … }
// or, when the token comes from a secret store:
client = gobpm.NewClient(url, gobpm.WithToken(token))
```

## Deploy and start a process

```go
projects, _ := client.ListProjects(ctx)                       // find the project ID
defID, _ := client.ImportDefinition(ctx, projectID, bpmnXML)  // BPMN 2.0 XML
instanceID, _ := client.StartProcess(ctx, projectID, "refund",
    gobpm.Variables{"amount": 42.50})
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
    gobpm.Variables{"paid": true})
err = client.BroadcastSignal(ctx, projectID, "quarter.closed", nil)
```

Careful with an empty correlation key: it matches every waiting subscription
for that message name. Pass one unless the name alone is genuinely
unambiguous.

## Human tasks in your own UI

```go
tasks, page, _ := client.ListTasks(ctx, gobpm.ListTasksOptions{PageSize: 50})
err := client.ClaimTask(ctx, task.ID, "alice")        // so nobody works it twice
err = client.CompleteTask(ctx, task.ID, "alice", gobpm.Variables{"approved": true})
```

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
worker := gobpm.NewWorker(client, "reverse-charge", "billing-service-1",
    gobpm.WorkerOptions{},          // sensible defaults; see WorkerOptions
    func(ctx context.Context, task *gobpm.ExternalTask) (gobpm.Variables, error) {
        amount := task.Variables["amount"]
        // … call your payment provider …
        return gobpm.Variables{"reversed": true}, nil
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
Idempotency-Key: gobpm-<32 characters>
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
built-in without a redeploy. Genuinely code-shaped connectors — an SDK, a
stream, anything stateful — keep the Go interface.

## Errors

Failures are JSON with an HTTP status: `{"error": "…"}`. The SDK surfaces
them as `*gobpm.APIError` with `IsNotFound` / `IsUnauthorized` helpers. Under
tenant scoping, another organization's resource answers **404, not 403** —
"not yours" and "does not exist" are deliberately indistinguishable.

## Run the whole journey

```bash
GOBPM_URL=http://localhost:8080 GOBPM_USERNAME=admin \
GOBPM_PASSWORD=… GOBPM_PROJECT="Default Project" \
  go run github.com/gsoultan/gobpm/sdk/examples/quickstart
```

It deploys a definition, starts an instance, serves its external task with a
worker, completes its human task, and prints the timeline.
