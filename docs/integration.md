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
| Tell a running process something happened | messages and signals — or a RabbitMQ queue Metis consumes |
| Show Metis's human tasks in your own UI | the task API |
| Have *your* service do a process step | external-task workers — polling Metis, or fed from a RabbitMQ queue |

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

### Roles: in one organization, or in every one

A role — Administrator, Designer, Operator, Query author — is held in one of
two places:

- **On the account's membership of one organization**, where it acts in that
  organization alone. That organization's administrators grant and revoke it.
- **On the account**, where it acts in every organization the account belongs
  to — which is how every role was held before roles could be granted in one
  organization. Only a platform administrator grants or takes one away.

**What a request acts with.** The account's roles in every organization, and the
ones it holds in the organization the request is for: the one
`X-Organization-ID` chose, or its first membership without it — the
organization the request is then scoped to. A role held in another
organization counts for nothing, and so does a `roles` claim in a token: roles
are read from the account when the request is authenticated, never from the
token. What no organization owns — adding an organization, the platform
accounts, connector templates and manifests — counts only the roles held in
every organization.

**Granting one in an organization.** An administrator of the organization the
request is for replaces the roles an account holds there:

```bash
curl -X PUT $GOBPM/api/v1/users/$USER_ID/organization-roles \
  -H "Authorization: Bearer $TOKEN" -H "X-Organization-ID: $ORG_ID" \
  -H 'Content-Type: application/json' \
  -d '{"roles": ["DESIGNER", "OPERATOR"]}'
```

The organization is always the request's, never the body's. The account has to
be a member of it (404 otherwise), and the roles have to be the four above (400
otherwise); they are stored as the server spells them, each once. Nothing else
about the account changes, so an account another organization shares needs no
say from there. `POST /api/v1/users` takes the same list as
`organization_roles`, for an account created in the organization the request is
for. The Roles tab on Platform access and the account dialog do the same.

**Granting one in every organization.** `roles` on `PUT /api/v1/users/{id}` or
`POST /api/v1/users` — and deleting an account that holds one — takes a
**platform administrator**: an administrator of every organization whose
account id the operator lists in `METIS_PLATFORM_ADMINS` where there is more
than one organization, and any administrator of every organization where there
is one. Anybody else is refused with a 403 that names the setting.

**Reading them.** An organization's list of accounts, `GET /api/v1/users/{id}`
and `GET /api/v1/users/me` give `roles`, held in every organization, and
`organization_roles`, held in the organization the request is for; what an
account holds in another organization is never written out. `/users/me` also
says `may_change_global_roles`, which the interface uses to decide what to
offer; the server checks again on every change.

**The last administrators are kept.** An organization's administrators are its
members holding Administrator there, in it alone or in every organization.
Taking the role from the last one, or deleting them, is refused with a 403 that
names the organization. The last account holding Administrator in every
organization is kept the same way: only such an account adds organizations and
manages the platform accounts, and no role granted in one organization can make
somebody one again.

**When a change takes effect.** At the next request. The server keeps the
account a token names for `METIS_AUTH_CACHE_TTL` (five seconds unless set) and
forgets it the moment its roles change, so the replica that made the change
uses the new roles at once, and any other within that time. A token issued
before the change carries nothing that outlives it.

**Signed in through an identity provider.** The account linked to the identity
holds roles in its organizations the same way. They live on its memberships, so
an organization the provider stops naming is left with the roles held there,
and joining it again starts with none.

### Signing in with OIDC

With `OIDC_ISSUER` and `OIDC_CLIENT_ID` set, the API accepts ID tokens from
that identity provider as the bearer token, beside the tokens `/api/v1/login`
issues to local accounts. Metis does not run the sign-in itself. The client
gets an ID token from the provider, for the client ID Metis is configured with,
and sends it as `Authorization: Bearer <id_token>`; Metis checks its signature,
issuer, audience and expiry.

**Which rules check a token** is decided by what the token says it is, before
anything in it is trusted, and those rules alone check it:

| The header's `alg` | The payload | The token is | Checked by |
| :-- | :-- | :-- | :-- |
| an HMAC — `HS256`, as `/api/v1/login` signs | names no `iss` | a local account's | `JWT_SECRET`, its expiry, and the account it names — exactly as without OIDC |
| a public-key signature — `RS256`, `PS256`, `ES256`, `EdDSA` and their kin | — | an ID token | the provider's published keys and algorithms, `OIDC_ISSUER`, `OIDC_CLIENT_ID` and its expiry; then the account linked to its issuer and subject, placed by the organization claim below |
| anything else — an HMAC that names an issuer, `none`, no `alg` | | neither | nothing: refused with 401 |

A token refused by its own rules is not tried against the other rules. So a
token that names the provider as its issuer cannot be signed with `JWT_SECRET`
instead — the provider's rules have no shared secret in them, and the local
rules, which never read an issuer, never see it — and an RS256 token is never
checked against `JWT_SECRET`. Trying one set of rules and falling back to the
other would do exactly that: whatever the stricter rules refused, the looser
ones would decide, and every refusal would carry the second check's reason
rather than the one that applied. What a token says about itself only chooses
the rules; it cannot get it accepted, because those rules read the same header
and claims again and verify them. A token that lies about its kind reaches
rules that refuse it.

**Keep a local administrator.** Because a local account's token works while OIDC
is on, an administrator with a password can still sign in when the provider is
unreachable. Keep one, with a strong password held offline. Turning OIDC on does
not switch local accounts off; delete the ones nobody should use.

**Which organizations somebody is in** comes from the token, in the claim
`METIS_OIDC_ORGANIZATION_CLAIM` names. The claim is one string or a list of
strings, and each value is an organization's **id** — the value
`X-Organization-ID` takes, shown on the Organizations page in Expert mode and
returned as `id` by `GET /api/v1/organizations`. An organization's name is not
matched: two organizations may share one, and a rename would silently move
people. A value that is not the id of an organization here is ignored. The
claim's name is matched exactly, at the top level of the token, so a namespaced
claim such as `https://metis.example.com/organizations` works as written.

```json
{
  "iss": "https://id.example.com/realms/acme",
  "aud": "metis",
  "sub": "f3c1d2e4-…",
  "preferred_username": "ada",
  "email": "ada@acme.example",
  "metis_organizations": ["0199a4c2-5e1b-7c3d-8f00-1a2b3c4d5e6f"]
}
```

Fill that claim at the provider from something your administrators control —
group membership, an attribute only they can set — and never from anything a
person can edit about themselves: whatever it says decides which organizations
they reach.

**The account.** A person's first sign-in creates an account linked to the
token's issuer and subject (`sub`). It is never matched to an existing account
by email or username — an email claim is no proof of the same person across
providers — so a local account with the same address stays a separate account.
The new account is named after `preferred_username`, else the email, else the
subject, with a short suffix when somebody already has that username, and takes
its name and email from the token. It holds **no role**: the task inbox —
listing, claiming and completing tasks — needs none. An administrator of one of
its organizations grants Designer, Operator or Administrator there afterwards,
and a platform administrator can grant one in every organization (see *Roles:
in one organization, or in every one*); a `roles` claim in the token grants
nothing. Changing the password in Metis is
refused with "change it at your identity provider", and so is setting one with
`--reset-password`, which names the provider to reset it at: a password here
would be a way in that the provider does not control.

**Memberships follow the claim.** A request is admitted only to the
organizations its own token's claim names; `X-Organization-ID` chooses among
them, and without it a request works in the first one the claim lists. Each
sign-in also makes the account's memberships match the claim: an organization
the provider stops naming is left, one it starts naming is joined. Every
membership such an account has came from its claim — Metis has no action that
adds an existing account to an organization — so nothing an administrator did
is undone, and local accounts are never touched. A token already issued still
names what it named until it expires, and organizations are never created from
a claim.

**Removing somebody** is done at the provider: take the organization out of
their claim, or take them out of the provider. Deleting their account in Metis
does not stop them — while the provider still places them in an organization
here, their next sign-in creates a new account under a new username, and the
deleted one's history stays with it.

**When somebody is refused.** A person whose claim places them in no
organization here is authenticated but not admitted, so the answer is **403**
with the reason, and no account is created for them:

| They are told | Cause | Fix |
| :-- | :-- | :-- |
| "…has not been told which ID-token claim lists their organizations…; an operator has to set METIS_OIDC_ORGANIZATION_CLAIM" | The setting is unset. The server also warns once at boot. | Set it to the claim's name and restart. |
| "the ID token from your identity provider has no \"metis_organizations\" claim…" | The provider does not send the claim, or sends it under another name. | Add the claim at the provider (a mapper, a rule, an action), or correct the setting. |
| "none of the organizations named in the \"metis_organizations\" claim of your ID token exists here…" | No value is the id of an organization here: a name instead of an id, a typo, another installation's ids, an organization since deleted. | Send the organization's id. |

The operator sees one warning per person and claim for as long as the refusal
is remembered, with the reason, the issuer and the claim — never the subject or
the email:

```json
{"level":"warn","issuer":"https://id.example.com/realms/acme","claim":"metis_organizations","reason":"forbidden: none of the organizations named in the \"metis_organizations\" claim of your ID token exists here; …","message":"Refused a sign-in through the identity provider: it does not place the person in any organization here"}
```

A token that does not verify — wrong issuer, audience or signature, or
expired — is still a 401.

Two things to know before changing anything. `METIS_AUTH_CACHE_TTL` (5s by
default) is also how long a sign-in's placement is reused, so an organization
deleted in Metis stops being reachable within it; a changed claim takes effect
with the first token that carries it. And the link is the issuer and the
subject together: pointing `OIDC_ISSUER` at a different issuer URL, even for
the same provider, gives everybody a new account at their next sign-in.

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
specific there is `AssignTask` (`POST /api/v1/tasks/{id}/assign`), allowed for
an administrator or for whoever currently holds the task — and, for a task
nobody was named for, for an operator. The person it goes to must be an account
in the task's organization, must not be barred from the step by separation of
duties, and — when the step is offered to people or teams — must be one of
them, unless an administrator says why not. Whoever does not hold the task
sends a `reason` with an assign, a delegate, a release, a hand back or an edit;
it is kept in the audit trail beside who did it.

**Delegating** (`POST /api/v1/tasks/{id}/delegate`) is different from
assigning: the holder stays the task's `owner`, the delegate works on it and
hands it back with `POST /api/v1/tasks/{id}/resolve`, and only the owner
completes it. `GET /api/v1/tasks/delegated` lists what the caller delegated
that has not come back. These routes are REST only.

**Who may take a task.** A task with an assignee is its assignee's to
complete. One offered to candidate users or groups may be claimed and
completed by those people and the members of those groups. One that names
nobody — no assignee, no candidates — is an administrator's or an operator's:
anybody else claiming, completing or handing it on gets a 403 that says the
task has no assignee and no candidates and who can take it, and
`ListTasksByCandidates` lists it only for them. That holds for a manual task
as for a user task: a manual step names its assignee and candidates the same
way, in the designer and in a BPMN file (`camunda:assignee`,
`camunda:candidateUsers`, `camunda:candidateGroups`). `METIS_ALLOW_UNASSIGNED_TASK_CLAIMS=true`
brings back the old rule, where anybody signed in could take such a task, for
a migration window.

**What a completion may set.** Completing writes the variables back into the
process and the instance moves on — the variables the task's form declares,
and no others. A form declares the `id` of each of its fields, hidden ones
included; a task that names a stored form by its form key declares that form's
fields as well. A completion carrying any other variable is refused with a 400
that names it — *this task's form has no field named amount; a task can set
only the variables its form declares* — and nothing changes: the task stays
open and no variable is set. A task with no form sets no variables, and
completes with none. So the approver of a refund can answer the approval and
cannot rewrite the amount on the way past. To set something new from a step,
give its form the field. `METIS_ALLOW_UNDECLARED_TASK_VARIABLES=true` brings
back the old rule for a migration window.

The instance's story is readable as plain language:

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
- **Work can be taken back while a worker holds it.** When its instance is
  cancelled, or its step is withdrawn, the task is gone, and a report on it is
  answered `{"error": "not found: no such external task"}`. When the instance
  ended some other way with the work still parked — a terminate end event on
  another branch — the report is answered `{"error": "invalid argument: This
  work belongs to an instance that has ended (completed); it is no longer
  wanted."}`, nothing the worker sent is written, and the work is taken off
  the list. Both arrive with HTTP 200 and the refusal in the reply's `error`
  field, on `/complete` and on `/failure`: check `error`, not the status, then
  stop and do not retry. Work of an instance that ended that second way can
  still be fetched until somebody reports on it. A worker whose lock has run
  out is refused for the lock first, whatever has become of the instance, so
  that work stays on the list and is offered again (read from the code, not
  run).

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

### Extending a lock

Work that can outlast its lock keeps it by extending it, before it runs out:

```bash
curl -X POST $GOBPM/api/v1/external-tasks/$TASK_ID/extend-lock \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"worker_id":"billing-1","lock_duration_ms":60000}'
```

The lock then runs out `lock_duration_ms` from now — not from when it would
have — and the reply says when: `{"lock_expiration":"2026-09-28T10:15:00.123456Z"}`.
So a lock can stay short, and a task whose worker died is offered again soon,
while a worker that is still at it extends for as long as it is working;
extending when half the lock has gone leaves room for a slow request.

- **Only the worker holding the lock, and only before it runs out.** Another
  `worker_id`, or a lock that has run out — even with nobody else holding the
  task yet — is refused with `400` and "… fetch it again". Stop the work: the
  task may already be with another worker, whose completion is the one that
  will be accepted. A lock knows its worker only by `worker_id`, so give each
  worker process its own.
- `lock_duration_ms` is from 1 to 86400000, a day. There is no default: `0`,
  or none, is refused rather than given fetch-and-lock's minute.
- Another organization's task, or one already completed, is `404`.
- Over Connect and gRPC it is `ExtendExternalTaskLock` on `ExternalTaskService`,
  with the same fields. As with that service's other methods, a refusal comes
  back in the reply's `error` field rather than as an error of the call, and
  with no `lock_expiration` beside it: check `error`.
- The Go SDK's worker does not extend locks yet; its handler's budget is still
  the lock it fetched with.

A worker taking tasks off a RabbitMQ bridge's queue extends the same way, with
the `worker_id` its message carries; see [What the bridge publishes, and what a
worker does with it](#what-the-bridge-publishes-and-what-a-worker-does-with-it).

## RabbitMQ: tasks out to a queue, messages in from one

A server can run two things against a RabbitMQ broker, and both are **off
unless whoever runs Metis turns them on**:

- a **bridge** publishes the external tasks of a topic to an exchange, for
  workers that consume from a queue rather than poll Metis;
- a **consumer** reads a queue and correlates each message on it as a BPMN
  message, for systems that already publish their events to RabbitMQ.

They are set in the server's environment, not through the API. A bridge sends
a project's work out of the installation, and that is for whoever runs the
servers to decide, not for any organization's administrator. This section is
not part of the SDK's quickstart; the path it describes is exercised against a
live broker by `internal/app/rabbitmq_broker_test.go`.

### Setting one up

1. On the project's **Connectors** page, an administrator adds a *RabbitMQ
   Publisher* connection whose URL names the broker, such as
   `amqps://metis:…@broker.example.com:5671/orders`. That is where the broker's
   password lives: encrypted at rest, never sent back to a browser, and changed
   there. The bridge and the consumer read it from the connection rather than
   from a second copy in the environment.
2. With **Expert Mode** on, the Projects page shows each project's id under its
   name, and the Connectors page each connection's. An entry needs both.
3. Name what to run, and restart:

```bash
METIS_RABBITMQ_BRIDGES='[
  {"project": "0192f0e4-5c1a-7d2e-8f3a-1b2c3d4e5f60", "connection": "0192f0e5-7a2b-7c3d-9e4f-5a6b7c8d9e0f",
   "topic": "reverse-charge", "exchange": "billing", "routing_key": "charges.reverse",
   "lock_seconds": 900}
]'
METIS_RABBITMQ_CONSUMERS='[
  {"project": "0192f0e4-5c1a-7d2e-8f3a-1b2c3d4e5f60", "connection": "0192f0e5-7a2b-7c3d-9e4f-5a6b7c8d9e0f",
   "queue": "payments", "message": "payment.received"}
]'
```

| Setting | |
| :-- | :-- |
| `project` | The project the bridge or consumer acts for. |
| `connection` | A RabbitMQ connection of that project. Another project's connection, or a connection to anything but RabbitMQ, is refused. |
| `topic` (bridge) | The external-task topic to publish, as set on the service task. |
| `exchange`, `routing_key` (bridge) | Where each task is published. Either may be empty, not both: with no exchange, the default exchange delivers to the queue the routing key names. Metis does not declare the exchange; it must exist. |
| `lock_seconds` (bridge) | How long each task the bridge publishes stays locked to it: whole seconds from 30 to 86400, and 300 (five minutes) when it is not set. It is the downstream worker's whole budget, queue time included — see [the worker's budget](#the-workers-budget-is-the-bridges-lock). A value out of range, or not a whole number, refuses the entry. Optional. |
| `queue` (consumer) | The queue to consume. Metis declares it — durable, no arguments — and a dead-letter queue beside it named `<queue>.dlq`. A queue that already exists with other arguments is refused by the broker. |
| `message` (consumer) | The BPMN message name each message is correlated as. |

One setting covers every bridge and consumer, and the *RabbitMQ Publisher*
connector as well: `METIS_RABBITMQ_CONFIRM_TIMEOUT`, how long a publish waits
for the broker to confirm it — a Go duration, `10s` when it is not set. A
publish not confirmed in time is treated as refused.

One bridge per project and topic, and one consumer per project and queue: a
repeat is refused. At start, each entry says so in the log — `Started a
RabbitMQ bridge` or `Started a RabbitMQ consumer`, with its project,
organization, topic or queue, the broker's host and virtual host (never its
password), and a bridge's lock — and then `A RabbitMQ bridge connected to its
broker` or `A RabbitMQ consumer is consuming from its queue`, which it says
again after every reconnection.

When something is wrong:

- **An entry that cannot be read** — not JSON, a setting misspelt or missing,
  an id that is not one, a `lock_seconds` out of range — is named by its
  position, as in `METIS_RABBITMQ_BRIDGES, bridge 2: "topic" is required`, and
  skipped. The others run, and the server starts either way.
- **A project or connection that does not exist, or a connection that cannot be
  used**, is logged with the reason and tried again after 5 seconds, then 10,
  20 and so on, up to every 5 minutes. Fix it and the bridge starts, with no
  restart. The connection is read once, when the bridge starts: a URL changed
  later takes effect at the next restart.
- **A broker that is down**, at start or later, is tried again after 5 seconds,
  then 10, 20 and so on, up to every 5 minutes, each wait varied by up to a
  quarter so that replicas do not come back in step. The log line gives the
  wait as `retryIn`. Once it connects the log says so, and once the connection
  has lasted — to a bridge's next round, or a task forwarded; through a
  consumer's first 5 seconds of consuming — the next outage starts again from
  5 seconds. A broker that takes each connection and drops it before then is
  waited for as if it were down. Nothing is lost meanwhile: tasks wait in
  Metis, and messages wait on the broker.
- **A connection the broker drops** — the broker restarting, the network
  cut — is noticed at once. A bridge connects again at its next round, a
  consumer after about 5 seconds.
- **An exchange that does not exist**, or that the broker's user may not
  publish to, makes the broker close the bridge's channel on the first publish.
  The task is handed back without spending a retry, the rest of that round's
  tasks go back unpublished, and the bridge opens a new channel on the same
  connection at its next round. Until the exchange is there each round hands
  the tasks back again; once it is, the next round forwards them. No restart.
- **A broker that takes a publish and does not confirm it** within
  `METIS_RABBITMQ_CONFIRM_TIMEOUT` — one out of memory or disk, say — is
  treated as having refused it: the task is handed back, the rest of the round
  goes back unpublished, and the next round publishes on a new channel, since
  the broker's late answer about the lost message could be read as its answer
  about the next. The broker may have delivered it all the same, so the worker
  can see it twice.
- **A consumer whose queue is deleted**, which makes the broker cancel it, or
  whose channel the broker closes, says why and consumes again on a new
  channel of the same connection about 5 seconds later, declaring the queue and
  its dead-letter queue again. A dead-letter publish the broker does not
  confirm in time puts the message back on the queue and replaces the channel.

Each problem is logged at `error` the first time, naming the bridge or
consumer and giving the broker's reason. The same problem again is logged at
`debug` until the bridge or consumer is working again — connected anew, a task
forwarded, consuming again — and the next problem after that is at `error`
afresh. What an operator sees:

| Line | Level | When |
| :-- | :-- | :-- |
| `A RabbitMQ bridge connected to its broker` | info | Every connection, the first and each after a loss. |
| `A RabbitMQ bridge could not connect to its broker` | error, then debug | The broker cannot be reached, or dropped the connection the bridge made before its next round; with the reason and `retryIn`. |
| `The broker closed a RabbitMQ bridge's channel or connection; the bridge opens a new one at its next round` | error, then debug | With the broker's reason, such as `the broker closed the channel: Exception (404) Reason: "NOT_FOUND - no exchange 'billing' in vhost '/'"`. |
| `A task was not accepted by the broker and was handed back` | error, then debug | With `taskID` and why: refused, returned as unroutable, or not confirmed in time. |
| `Tasks the bridge fetched and did not publish were handed back` | warn | The rest of a round after a closed channel or a missed confirm, or what was fetched when the bridge stopped. |
| `A RabbitMQ consumer is consuming from its queue` | info | Every time it starts consuming. |
| `A RabbitMQ consumer could not consume from its queue; retrying` | error, then debug | It could not connect, or declare the queue, or start consuming; with the reason and `retryIn`. |
| `A RabbitMQ consumer stopped receiving messages from its queue; it will consume again` | error, then debug | With the broker's reason, or that the broker cancelled the consumer, and `retryIn`. |

### What the bridge publishes, and what a worker does with it

Every 5 seconds each bridge takes up to ten tasks of its topic and publishes
each one as JSON — the task as the external-task API returns it, with its `id`,
`variables`, `worker_id` and `lock_expiration` — with a `task_id` header.
Publishes are confirmed and `mandatory`: a task the broker does not accept,
cannot route to a queue, or does not confirm within
`METIS_RABBITMQ_CONFIRM_TIMEOUT`, is handed back at once without spending one
of its retries, and is offered again at the next poll. So is a task the bridge
had fetched and not yet published when the server stops.

Every message Metis publishes — a bridge's task, a dead letter, a *RabbitMQ
Publisher* step's message — is persistent, so one the broker has confirmed
survives the broker restarting, **if the queue it sits in is durable**. The
queues Metis declares are. Bind a bridge's exchange to a durable queue: a
message in a queue that is not durable goes with the queue, whatever its
delivery mode.

The worker completes the task, or reports its failure, through the
external-task API like any other worker, with the `worker_id` the message
carries, which is `messaging-bridge`:

```bash
curl -X POST $GOBPM/api/v1/external-tasks/$TASK_ID/complete \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"worker_id":"messaging-bridge","variables":{"reversed":true}}'
```

Work that may outlast the bridge's lock extends it the same way, before the
message's `lock_expiration`, with the message's `id` — the `task_id` header
holds it too — and the same `worker_id`
([Extending a lock](#extending-a-lock)):

```bash
curl -X POST $GOBPM/api/v1/external-tasks/$TASK_ID/extend-lock \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"worker_id":"messaging-bridge","lock_duration_ms":600000}'
```

- **A topic is shared by an organization's projects.** A bridge publishes every
  task of its topic in the project's organization: the same tasks a worker of
  that organization fetching the topic through the API would get. Run one
  bridge per topic per organization, and no API worker on a bridged topic.
- A bridge reads the main runtime. Tasks of processes running in an
  environment of their own are not bridged.
- The message carries the task's variables. Use `amqps://` for a broker outside
  your network.

#### The worker's budget is the bridge's lock

The bridge locks each task when it fetches it, for its `lock_seconds` — five
minutes unless the entry says otherwise — and the lock covers the task's whole
trip: the time its message waits on the queue, and the time the worker takes,
unless the worker extends it. The message's `lock_expiration` says when it runs
out.

Extend before then, not after. Every bridge holds its locks under the one
worker id, `messaging-bridge`, so a lock cannot tell one delivery of a task
from the next. Once `lock_expiration` has passed, the bridge may have fetched
the task again and published it to another worker, and an extension that
arrives after that extends the new delivery's lock: its answer does not say the
task is still yours. A worker past its message's `lock_expiration` — or past
the time its last extension answered — should treat the task as lost.

A task still open when its lock runs out is published again at the bridge's
next round, and only one completion is accepted. So a worker that takes longer
without extending, or a queue that backs up for longer, means the same task
delivered twice and done twice — harmless only if the handler is idempotent,
which any external-task worker's must be. It was a fixed 30 seconds, which a
queue that backed up at all ran through, so the backlog multiplied itself.

Set `lock_seconds` above the longest the queue is expected to back up plus the
longest the work takes. A longer lock has one cost: a task whose message is
lost — the queue purged, or, since the bridge publishes its messages as
transient, a broker that restarted with them still queued — is published
again only once its lock runs out, and so is the task of a worker that died.

### What the consumer expects

Each message is a JSON object. Its `correlation_key` field — or, without one, a
`correlation_key` header — picks the waiting instance, and the whole object is
merged into that instance's variables, as with `SendMessage`. With no key at
all it reaches every instance waiting on that message name, and starts any
process whose message start event names it.

- A message is acknowledged once it has been correlated, or parked on
  `<queue>.dlq` with the reason: a body that is not a JSON object, or a correlation that
  failed three times. The parked copy keeps the original body. A message no
  instance is waiting for is acknowledged and dropped, as it is when sent
  through the API.
- Messages are taken one at a time and acknowledged only once handled. One
  interrupted by a shutdown goes back on the queue.

### More than one replica

Give every replica the same list. Replicas consuming one queue are competing
consumers, so each message reaches one of them, and a bridge's fetch locks the
tasks it takes, so two replicas never publish one task inside its lock. See
[`recovery.md`](recovery.md) §2.1.

## Watching instances

```bash
curl -H "Authorization: Bearer $TOKEN" $GOBPM/api/v1/instances/$ID        # status + variables
curl -H "Authorization: Bearer $TOKEN" $GOBPM/api/v1/instances/$ID/audit  # the timeline
curl -H "Authorization: Bearer $TOKEN" $GOBPM/api/v1/instances/$ID/path   # execution path + frequencies
curl -H "Authorization: Bearer $TOKEN" $GOBPM/api/v1/instances/$ID/deviations  # what was done outside its process
```

`/deviations` answers `{"deviations": [...]}`, oldest first, with a row for each
thing done to the instance that its process did not decide — a hand-over by
somebody who did not hold the task, a migration's skip, cancel or hold, a
waive, cancel or hold of the one instance in place, a step
started inside an ad-hoc sub-process — saying who did it and why, and what
changed on the task or instance where something did, to anyone signed in to the instance's organization; another organization's
instance is a 404.

Three things about a row's shape that a client can rely on:

- `actor` is a username, exactly as the account has it — a space at either end
  included, since that is a different name — and no account id is returned. The server acting with
  nobody signed in is written as `System`, which an account may also be called,
  so `actor_is_server` says which it was: `true` exactly when the row names no
  account, `false` for every row a signed-in account made, whatever its name.
  It is always present.
- `before`, `after` and `details` are always objects, `{}` when the row has
  nothing to put in one, so `row.before.tasks` can be read without asking
  first whether there is a `before`.
- The row of an act is written once and never changed. A `hold` row says the
  hold was placed; whether it is still open is the status of the incident it
  names in `after.incident.id`, read from `GET /api/v1/incidents/$ID`. What
  writes no row at all, and why, is in
  [Changing a process that is already running](process-change-in-flight.md#audit).
- **Read `status` before counting a row as something done.** A waive that
  waits for a second administrator has a row with `status: "pending_approval"`
  and a `request_id`. That one row is changed once, when its request is
  decided: to `applied`, with `approved_by` and `decided_at`, or to `rejected`,
  `expired` or `stale`, which say the waive was asked for and not made. Every
  other row is `applied`.

`GET /api/v1/events` is a server-sent-events stream for live updates, which
is how the built-in UI avoids polling.

## Waiving, cancelling or holding one instance

An administrator can deal with one running instance outside what its process
says: waive the step it waits at, cancel it, or hold it for somebody to
decide. What each does, and what each refuses, is in
[Changing a process that is already running](process-change-in-flight.md#in-place-waive-cancel-and-hold);
what an operator does with it is in
[the runbooks](runbooks.md#waiving-cancelling-or-holding-one-instance). This
is the request and the reply.

```bash
# Preview: what would it do, and can it be done? Nothing is changed.
curl -X POST $GOBPM/api/v1/instances/$ID/deviations \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"kind":"waive","node_id":"opsApprove","reason":"the operations manager is on leave","outputs":{"approved":true}}'

# Apply: the same request, with the plan's visit_key and "dry_run": false.
curl -X POST $GOBPM/api/v1/instances/$ID/deviations \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"kind":"waive","node_id":"opsApprove","reason":"the operations manager is on leave","outputs":{"approved":true},"visit_key":"dv1-…","dry_run":false}'
```

REST only. Connect and gRPC have no such call.

**Who may call it.** An account holding Administrator in the organization the
request is for: on its membership there, or on the account, for one that
belongs to that organization. A preview takes the same, because it reads the
instance. The roles legend (`GET /api/v1/roles`) lists the action as
`DeviateInstance`, under Administrator, among the instances' actions.

The organization the request is for is the one `X-Organization-ID` names, or
the account's first membership without it, so an account in several
organizations names the instance's:

| The header names | Answer |
| :-- | :-- |
| the instance's organization, where the account is an administrator | the plan, or the apply |
| the instance's organization, where the account is not an administrator | 403 |
| another organization the account administers | 404: the instance is not in it |
| an organization the account does not belong to | 401 |

**The request.**

| Field | |
| :-- | :-- |
| `kind` | `waive`, `cancel` or `hold`. |
| `node_id` | The step's id, at most 255 characters. Needed for a waive and a hold. A cancel names the step the instance is to be ended at, and leaves it out only for an instance that waits at no step. |
| `reason` | Why. Needed for all three, at most 2,000 characters, the spaces around it dropped. Kept with the record. |
| `outputs` | A waive only: what the waived step counts as, as an object of values by the id of a field of the step's form. At most 50 values, at most 64 KiB as JSON, no `null`, a name of at most 255 characters. |
| `visit_key` | The `visit_key` of the plan that was previewed. Needed to apply. At most 255 characters; the server's own are 36. |
| `dry_run` | Only the JSON boolean `false` applies. Left out, `true` or `null` is a preview. |

**How the body is read.** Strictly, because how it is read decides whether an
instance is changed:

- One JSON object and nothing after it, at most 256 KiB.
- Only those six fields, each named exactly as above and given once.
  `"DRY_RUN": false`, `"dryRun": false` and `dry_run` given twice are refused,
  not read as a preview. `instance_id` in the body is refused: the instance
  comes from the address.
- Inside `outputs`, a name given twice in one object is refused too, at any
  depth: `"outputs": {"amount": 250, "amount": 10}` is not read as 10. An
  output decides which branch a gateway takes.
- Each field holds the kind of value it takes. `"dry_run": "false"` and
  `"dry_run": 0` are refused.
- Anything else is a 400 with one sentence: *this request could not be read:
  send one JSON object with kind, reason, node_id (a cancel may leave it out),
  and for a waive outputs; to apply a plan add its visit_key and "dry_run":
  false; name each field exactly and once*. An empty body is that 400, never a
  preview. `{}` can be read, and is refused for its kind: *kind must be waive,
  cancel or hold*.
- The switch is the top-level field only. `?dry_run=false` in the address is
  ignored. A `dry_run` inside `outputs` is an output of that name (read from
  the code, not run).
- `Content-Type` is not examined: the body is read as JSON whatever the
  header says. A byte-order mark before the object is refused. (Both read
  from the code, not run.)

**The reply to a preview** is the plan:

```json
{
  "plan": {
    "instance_id": "0199…",
    "kind": "waive",
    "scope": "task",
    "node_id": "opsApprove",
    "node_name": "Operations approve",
    "visit_key": "dv1-…",
    "open_work": [
      { "task_id": "0199…", "name": "Operations approve", "node_id": "opsApprove",
        "node_name": "Operations approve", "status": "claimed", "assignee": "ollie" }
    ],
    "open_work_in_all": 1,
    "outputs": { "approved": true },
    "decision_points": [
      { "node_id": "decide", "node_name": "Approved?", "kind": "gateway",
        "reads": ["approved"], "reads_in_all": 1,
        "supplied": ["approved"], "missing": [], "missing_in_all": 0,
        "has_default_flow": false, "analysed": true }
    ],
    "decision_points_in_all": 1,
    "missing": [],
    "missing_in_all": 0,
    "called_instances": [], "called_instances_in_all": 0,
    "requires_second_approver": true,
    "refusals": [],
    "warnings": ["“Operations approve” is with ollie, who will be told it was withdrawn."],
    "applicable": true
  },
  "applied": false,
  "replayed": false
}
```

- **`applicable`** is the answer to "would an apply be made": nothing refuses
  the plan. Read it, and never infer it from an empty list.
- **`refusals`** is every reason the act cannot be made, each a sentence;
  **`warnings`** is what to know before applying. A plan that refuses is still
  a 200 to a preview.
- **`visit_key`** is what an apply sends back.
- **`open_work`** is the open tasks where the act is made: the step's for a
  waive and a hold, every open task of the instance for a cancel. A waive and
  a cancel take them; a hold takes none, and lists them to show what is
  waiting there. A holder is named
  by username; no account id is returned.
- **`outputs`** echoes the outputs as the server read them, in the preview and
  in the apply. It is where a client confirms what was read.
- **`decision_points`** is the places in the process that decide from a field
  the waived step's form declares, each with what it reads of those fields
  (`reads`), which of them the waive gives (`supplied`) and which it does not
  (`missing`). `kind` is `gateway`, `conditional_event`,
  `completion_condition`, `decision_table`, `collection` or `called_process`.
  `analysed: false` means what the place reads could not be told in full: its
  lists hold what could be told, and may be short.
- **`missing`** is the complete list of what a waive has still to supply, each
  name in full, whenever the waive can be made: at most 50 names, each to 255
  characters. A waive that would need more, or a longer name, is refused for
  that, and the list is then cut.
- **`called_instances`** is, for a cancel, the ids of the processes this
  instance started that have not ended: the 200 with the lowest ids, and
  `called_instances_in_all` is how many there are.
- **`requires_second_approver`** is `true` on every plan of a waive and
  `false` on a cancel's and a hold's. `true` means an apply will not make the
  act: it will ask for it, and answer 202.
- `scope` is `task` for a waive and `instance` for a cancel and a hold.
- Every list is `[]` and `outputs` is `{}` when empty; nothing is `null`. Left
  out when empty: the plan's `node_id` and `node_name` for a cancel that names
  no step, and a task's `assignee` and `iteration_id`.

**The lists are the first of what there is.** A plan has a size whatever the
process. It lists at most 100 decision points (those missing a value first,
then those that could not be read), 200 open tasks, 200 called instances, 50
missing names, and ten names in each of a point's lists, each shown to 64
characters. The counts beside them are of everything:
`decision_points_in_all`, `open_work_in_all`, `called_instances_in_all`,
`missing_in_all`, and a point's `reads_in_all` and `missing_in_all`. They are
exact, except that a point's own two counts may count a name twice where
several steps of a definition share an id; never too few. `refusals` and
`warnings` are bounded the same way: they speak of the first ten decision
points of a kind and of each task the plan lists, and count the rest in a
sentence.

**Read `applied`, not the status alone.** A waive needs a second
administrator. Its apply is answered **202**, with `applied: false` and
`pending_approval`, and nothing about the instance has changed. A client that
treats every 2xx as "done" will take a waive for made when it was only asked
for; one that treats only 200 as success will take the 202 for a failure. Read
`applied`. Until this release the field `requires_second_approver` was always
`false` and an apply of a waive answered 200 with `applied: true`.

**The reply to an apply of a waive** is the plan, `"applied": false`, the row
that waits under `deviation`, and the request under `pending_approval`:

```json
{
  "plan": { "…": "as above" },
  "applied": false,
  "replayed": false,
  "deviation": {
    "id": "0199…", "kind": "waive", "scope": "task", "origin": "in_place",
    "status": "pending_approval", "node_id": "opsApprove", "node_name": "Operations approve",
    "actor": "boss", "actor_is_server": false,
    "reason": "the operations manager is on leave",
    "before": {}, "after": { "variables": { "approved": true } },
    "details": { "open_work": 1, "decision_points": 1 },
    "run_id": "0199…", "request_id": "0199…", "created_at": "2026-10-04T09:12:00Z"
  },
  "pending_approval": {
    "request_id": "0199…",
    "status": "pending_approval",
    "requested_by": "boss",
    "expires_at": "2026-10-07T09:12:00Z",
    "because": ["“Operations approve” would be waived: nobody performs it, and the process moves on"]
  }
}
```

No task is named on the row and none is withdrawn: who holds the work is
recorded when the waive is made. `pending_approval` is absent from every other
reply of this route. What happens next is in
[Requests for a second administrator](#requests-for-a-second-administrator).

**The reply to an apply of a cancel or a hold** is the plan it was applied
with, `"applied": true`, and the record under `deviation`, exactly as
`GET …/deviations` returns that row. A waive that has been approved is
answered in the same shape when its apply is sent again, as a replay. This is
that reply; the record is the row that waited, now naming who approved:

```json
{
  "plan": { "…": "names the act and lists nothing, as every replay's does" },
  "applied": true,
  "replayed": true,
  "deviation": {
    "id": "0199…", "kind": "waive", "scope": "task", "origin": "in_place",
    "status": "applied", "node_id": "opsApprove", "node_name": "Operations approve",
    "task_id": "0199…", "actor": "boss", "actor_is_server": false,
    "reason": "the operations manager is on leave", "approved_by": "deputy",
    "before": { "tasks": { "0199…": { "status": "claimed", "assignee": "ollie" } } },
    "after": { "tasks": { "0199…": { "status": "canceled" } },
               "variables": { "approved": true } },
    "details": { "withdrawn": 1, "tasks_listed": 1, "decision_points": 1 },
    "run_id": "0199…", "request_id": "0199…", "audit_entry_id": "0199…",
    "created_at": "2026-10-04T09:12:00Z", "decided_at": "2026-10-04T11:40:00Z"
  }
}
```

`actor` is who asked, and `approved_by` who approved.

**A retried apply acts once.** The same request sent again — the same kind,
step, reason, values and `visit_key` — is answered `"replayed": true` with the
record the first one wrote, and nothing is done again. This does not expire.
For a waive that still waits, the answer is the 202 again with
`"replayed": true` and the same `request_id`, to the account that asked; once
it is approved, the same request is answered 200 with `"applied": true,
"replayed": true`. Anybody else asking for the same waive, and the requester
asking for something else on the visit, gets a 400 naming the request that
waits: *A request to waive “Operations approve” is already waiting for approval
(request 0199…, asked by boss); approve or reject that one.* A request that
waited past its deadline holds nothing: the same apply closes it and makes a
fresh one.
The plan of a replayed reply names the act and lists nothing: its lists are
empty and its counts are zero, and neither says there was nothing. A different
request naming a `visit_key` that has had its act is a 400 that says who
acted: *this step was already waived by boss*.

So the route is safe to retry with no `Idempotency-Key`. If a client sends one
anyway (`TestAnIdempotencyKeyOnADeviationFollowsTheHeadersOwnRules` runs the 409
and the replay of a 400 on this route; a replayed 403 or 500, and the last
point, are read from the header's code, not run here):

- A preview and its apply are different bodies. Sent under one key, the second
  is refused by the header's own check with a plain-text 409, before the
  route sees it. Use a new key for each request.
- The first answer under a key is kept for 15 minutes and returned again,
  with `Idempotency-Replayed: true`, whatever its status: a 400, a 403 or a
  500 comes back after its cause is fixed. Send a new key after fixing. A
  waive's 202 is kept the same way, and comes back as a 202 for those 15
  minutes **even after the request has been approved**. Do not poll an apply
  under one key to learn whether a waive was made: read the request.
- The header and the `replayed` field are different signals. The header's
  replay returns the first body unchanged, so its `replayed` may read `false`.
- A retry under the same key while the first is still waiting for the
  instance's lock waits up to ten seconds and is then answered 408, or 503 if
  the first was abandoned. Both are plain text.

**Statuses.** Branch on the status, never on the text of a reply.

| Status | When | Body |
| :-- | :-- | :-- |
| 200 | A preview, whether or not its plan refuses. An apply of a cancel or a hold. A replay of an act that was made, an approved waive among them. | as above |
| 202 | An apply of a waive that makes a request, and the same apply sent again by whoever asked while it waits. Nothing has changed. | as above, with `pending_approval` |
| 400 | The body could not be read, or is over 256 KiB. The id in the address is not an id. The command is malformed: no kind, no step for a waive or a hold, a `node_id` or a `visit_key` longer than 255 characters, outputs on a cancel or a hold, a `null` or unnamed output, more than 50, an apply with no `visit_key`. An apply whose plan refuses: the refusals, joined by a space. An apply that comes too late: *this instance has moved since you previewed it; preview again*, *this instance is cancelled, so it can no longer be waived; preview again*, *this step was already waived by boss*. A waive of a visit a request already waits for, asked by anybody but its requester or for something else. A suspended instance. (A waive whose value fits no branch of a gateway is no longer found here: it is the approval's 400.) | `{"error": "invalid argument: …"}` |
| 401 | No token. | the plain text `Unauthorized` |
| 403 | Signed in, and not an administrator of the organization the request is for. Or an administrator whose token names no account, applying a waive: *asking for and giving a second administrator's approval needs an account, and this request carries none*. | `{"error": "forbidden: this needs the ADMIN role, which your account does not hold in this organization; an administrator here can grant it"}` |
| 404 | The instance is not in the organization the request is for, or there is no such instance: the same words for both. | `{"error": "not found: no such process instance"}` |
| 500 | The server failed after the request was accepted. What the act had done is rolled back, unless the failure came at the commit itself. Send the same request again: it acts, asks or replays. | `{"error": "recording the request to waive “Review the claim”: …"}` |

Four things about them:

- **A 400 carries a sentence and no machine-readable code.** The same status
  covers a request to correct and an instance to preview again.
- **The route itself never answers a conflict status.** What was already
  done, and work that has moved, are 400s. A request sent with an
  `Idempotency-Key` can still meet the header's own 409 and 408 on this
  route, in plain text, before the route sees it.
- **A 500's text is not a status.** It keeps the cause as words, and may
  contain `not found:`, the instance's id, step ids and a decision's key.
- **The body is read before the caller is checked**, as on every route. A
  signed-in account that is not an administrator and sends a body that cannot
  be read gets the 400 for the body. Nothing of any instance has been read by
  then. With a readable body it gets the 403, whatever instance it names.

Answers from in front of every route are plain text, not JSON: 413 for a body
declared larger than 2 MiB (between 256 KiB and 2 MiB is the route's 400), 429
from the rate limit, 503 when the server is at its limit of requests in
flight (read from the code, not run on this route).

**Numbers.** `outputs` is decoded as a task completion's variables are: every
number is a float. Two spellings of one number (`250`, `250.0`, `2.5e2`) are
the same request, and replay. `"250"` is not `250`. Two whole numbers past
2^53 that round to the same float are the same request too
(`TestTwoWholeNumbersPastTheFloatsPrecisionAreOneRequest`): send a large
identifier as a string.

**No deadline on the server.** An apply waits for the instance's lock, and for
the rows of the tasks it withdraws, for as long as the client stays. While it
waits it holds one of the server's 128 slots for requests in flight. Set a
timeout in the client, and send the same request again afterwards: it either
acts or replays.

**What a worker meets.** A cancel withdraws the work an instance has parked
for outside workers. A worker that then reports on it is told, as of any
withdrawn work, HTTP 200 with `{"error": "not found: no such external task"}`.
See [External-task workers](#external-task-workers-your-service-does-the-step).

## Requests for a second administrator

Two applies do not make their change. They record a **request**, answer 202,
and the change is made when a different administrator approves:

- a waive of a step of one instance
  (`POST /api/v1/instances/{id}/deviations`, [above](#waiving-cancelling-or-holding-one-instance));
- a migration whose plan says `requires_second_approver: true`
  (`POST /api/v1/definitions/versions/migrate`,
  [below](#a-migration-that-waits)).

What asks, what an approval does and what the control does not cover are in
[Changing a process that is already running](process-change-in-flight.md#a-second-administrator);
what an approver does is in
[the runbooks](runbooks.md#approving-a-request-for-a-second-administrator).
This is the four routes that read and decide a request.

```bash
curl -H "Authorization: Bearer $TOKEN" "$GOBPM/api/v1/deviation-requests"            # what waits
curl -H "Authorization: Bearer $TOKEN" "$GOBPM/api/v1/deviation-requests/$REQUEST"   # one, whole
curl -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"reason":"checked with the process owner"}' "$GOBPM/api/v1/deviation-requests/$REQUEST/approve"
curl -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"reason":"the approver is back tomorrow"}' "$GOBPM/api/v1/deviation-requests/$REQUEST/reject"
```

REST only. Connect and gRPC have no such calls, and no migrate call either.

**Who may call them.** An administrator of the organization the request is
for, as on the route above: the role held there or on the account, and
`X-Organization-ID` for an account in several. The roles legend lists them as
`ListDeviationRequests`, `GetDeviationRequest`, `ApproveDeviationRequest` and
`RejectDeviationRequest`. The service asks the same again and also needs the
caller to have an account. A request of another organization, and one whose
project was deleted, is answered as one that does not exist: 404, *no such
request*.

**The requester cannot approve their own request.** They are told apart from
the approver by account id, not by name. The one exception is an organization
the operator has named as having one administrator
([the runbooks](runbooks.md#an-organization-with-one-administrator)). The
requester can always reject their own request, which is a withdrawal.

### The queue

`GET /api/v1/deviation-requests` answers
`{"requests": [...], "total": n}`, newest first.

| Query | |
| :-- | :-- |
| `status` | One of `pending_approval`, `approved`, `applied`, `interrupted`, `stale`, `rejected`, `expired`. **Left out or empty, it is `pending_approval`.** No call lists every status. |
| `project_id` | Narrows to one project. A project the organization does not have lists nothing. |
| `page`, `page_size` | Whole numbers, at most 1,000,000 each. The page size is 50 unless given and at most 200. |

- **`status` is as the request reads now**, not as it is stored. A request
  past its deadline is listed under `expired`, and not under
  `pending_approval`, before anything has recorded the expiry. `total` counts
  the same way.
- **A listed request is not whole.** It has `id`, `kind`, `status`,
  `project_id`, `requested_by`, `reason`, `expires_at`, `self_approved`,
  `outcome`, `created_at`, the ids its kind has and the decision when there is
  one. It never has `command`, `plan`, `because`, `instances`,
  `instances_in_all` or `unavailable`. Read the request by id for those. So a
  client that wants to show a step's name beside each row makes one read per
  row.
- `requests` is `[]` when there are none.

### One request

`GET /api/v1/deviation-requests/{id}` answers `{"request": {...}}`:

```json
{
  "request": {
    "id": "0199…",
    "kind": "instance_waive",
    "status": "pending_approval",
    "project_id": "0199…",
    "instance_id": "0199…",
    "requested_by": "boss",
    "reason": "the operations manager is on leave",
    "because": ["“Operations approve” would be waived: nobody performs it, and the process moves on"],
    "instances": [],
    "instances_in_all": 0,
    "command": { "instance_id": "0199…", "kind": "waive", "node_id": "opsApprove",
                 "reason": "the operations manager is on leave",
                 "visit_key": "dv1-…", "outputs": { "approved": true } },
    "plan": { "…": "the plan as the requester previewed it, and because" },
    "expires_at": "2026-10-07T09:12:00Z",
    "self_approved": false,
    "outcome": {},
    "created_at": "2026-10-04T09:12:00Z"
  }
}
```

| Field | |
| :-- | :-- |
| `kind` | `instance_waive` or `migration`. |
| `status` | As the request reads now. See [a request's life](process-change-in-flight.md#the-request). |
| `instance_id` | A waive only. |
| `source_definition_id`, `target_definition_id` | A migration only. |
| `requested_by` | A username. No view carries an account id. |
| `reason` | A waive's reason as typed. For a migration: the reasons its decisions gave, in the order of their steps' ids; then `acknowledged the loss of …`; then `redirected “a” to “b”, …`. Each part lists ten and counts the rest. A migration that types, acknowledges and redirects nothing says what its plan said. |
| `because` | Why it needs a second administrator, one sentence each. For a migration, the plan's `second_approver_reasons`. Always a list. |
| `command` | What an approval carries out. For a waive: `instance_id`, `kind`, `node_id`, `reason`, `visit_key`, and `outputs` when there were any. For a migration: `source_definition_id`, `target_definition_id`, `node_mapping`, `node_actions`, `instances`, `acknowledge`. |
| `plan` | The plan **as the requester was shown it** when they asked, under the names the preview used, with `because` added. Every list it cuts short has its count beside it: for a waive, at most 100 decision points, ten names at a point, 200 open tasks and 50 missing names, as in any [plan](#waiving-cancelling-or-holding-one-instance). An approval does not act on it: it plans again. |
| `instances`, `instances_in_all` | A migration: the instances that had not ended when it was asked for, which are the only ones a run under this request may act on. The list holds the first 200; the count is of all. A waive: `[]` and `0`. |
| `expires_at` | Its deadline, fixed when it was made. |
| `decided_by`, `decided_at`, `decision_reason` | Left out until somebody decides. An expired request has none of them: the clock decided it. A stale request has `decided_at` alone; who found it stale is `outcome.attempted_by`. |
| `self_approved` | Always present. `true` only for a request its own requester approved. |
| `outcome` | An object always, `{}` while it waits. See below. |
| `unavailable` | Only when a stored document can no longer be read. It names which of `command`, `plan` and `instances`; those fields, with `because` and `instances_in_all`, are then left out, not written empty. Such a request can still be read, rejected and withdrawn. Approving it is a 500. |

Nothing is `null`. The waive's `visit_key` is in `command` and `plan`. Nothing
else the server tells requests apart by is returned.

**`outcome`**, by what became of the request:

| Status | Keys |
| :-- | :-- |
| `applied`, a waive | `deviation_id`: the ledger row. |
| `applied`, a migration | `changed` and `passed_over`: how many instances the run acted on and how many it left alone. `note` when it changed nothing: *every instance the run reached was passed over* or *no instance was active on the version when it ran*. |
| `interrupted` | `changed`, `passed_over` and `error`: one sentence of the server's own. The failure's own words are not kept here; they are in the server's log. After a panic, `count_unknown: true` in place of the two counts: do not print zeros for it. For a run that never reported, `error` alone: *the run did not report back*. `reported_after_sweep: true` on a report a run wrote after it had been marked interrupted (an `applied` request can carry it too). |
| `stale` | `why`, `refusals` (what the plan made at the approval refused) and `attempted_by`. |
| `rejected`, `expired` | `{}`. |
| any, when `self_approved` | also `self_approved: true`, `other_administrators: 0` and `organization_id` on a migration's request. |

`passed_over` in an outcome is a count. Which instances they were is in the
reply to the approval and nowhere else.

### Approving

`POST /api/v1/deviation-requests/{id}/approve`. The body is `{"reason": "…"}`,
`{}`, or nothing at all: an approval's note is optional. A rejection takes the
same body, and its reason is required.

**How the body is read.** One JSON object and nothing after it, at most 16
KiB, holding the one field `reason`, named exactly and once, as text of at
most 2,000 characters. Any other field — a command, outputs, a visit key, an
approver's name — is a 400: *this request could not be read: send one JSON
object with a reason, or nothing; name the field exactly and once*. What is
approved is exactly what was asked for.

**Approving a waive** makes it. The reply is the request, `"applied": true`,
the ledger's record under `deviation` and the plan made under the instance's
lock under `plan`:

```json
{
  "request": { "…": "as above, status applied, decided_by, decided_at, outcome.deviation_id" },
  "applied": true,
  "deviation": { "…": "the record, status applied, approved_by" },
  "plan": { "…": "the plan as the instance stood when it was approved" }
}
```

**Approving a migration** runs it, in this call, for as long as the run takes.
The reply has no `deviation`. Its `plan` is the migration's plan as the
migrate route writes it, and it carries `passed_over` and
`passed_over_in_all` ([below](#the-instances-a-run-passed-over)):

```json
{
  "request": { "…": "status applied, outcome.changed, outcome.passed_over" },
  "applied": true,
  "plan": { "…": "the migration's plan" },
  "passed_over": [],
  "passed_over_in_all": 0
}
```

**Read `applied` beside the request's status.** `applied` keeps the migrate
route's meaning: `false` when the run passed instances over and acted on none.
The request reads `applied` all the same, because it is spent.

| Status | When |
| :-- | :-- |
| 200 | Approved and carried out. |
| 400 | The body could not be read, or is over 16 KiB. The id is not an id. The note is over 2,000 characters. |
| 400 | Already decided, nothing written: *deputy approved this on 4 October 2026 11:40 UTC, and it was applied.*, *… rejected this on ….*, *This request expired on ….*, *This request went stale on …: what it asked for no longer held.*, *…; it is being applied.*, *…, and the run stopped part-way: … Ask again for what remains.* |
| 400 | **Recorded first, then refused.** The request is closed and the 400 follows: *This request expired on … before anybody approved it, so nothing was applied. Ask again if it is still needed.* (it now reads `expired`); for a waive *This request no longer holds — … — so nothing was applied. Preview again and ask afresh.*, for a migration *This request no longer holds, so nothing was applied: …. Ask again.* (it now reads `stale`). For a waive the closing is the request, its ledger row and one trail entry; for a migration the request alone. |
| 400 | A waive, nothing written, the request still waits: a gateway with no way out for the values, its sentence followed by *The request is still waiting: reject it, and the waive can be asked for again.*; a suspended instance. |
| 400 | A migration that was approved, whose run its own plan then refused: *invalid argument: the approved migration did not finish: …*. Nothing was moved. The request reads `interrupted`. |
| 400 | A self-approval with no reason, where one is allowed: *Say why you are approving your own request: with nobody else to approve it, the reason is the record.* |
| 401 | No token, or an `X-Organization-ID` naming an organization the caller does not belong to. |
| 403 | Not an administrator of the organization. The requester: *You asked for this. A different administrator has to approve it.*, or the longer sentence for an organization with nobody else. A token that names no account. |
| 403 | A migration that was approved and then refused at the gate before anything was moved: *forbidden: the approved migration did not finish: …*. The request reads `interrupted`. |
| 404 | *no such request*. |
| 500 | A migration's run stopped part-way, whatever its cause: `{"error": "the approved migration did not finish: …. Request … now reads interrupted; what its run had done stands, and what remains has to be asked for again"}` and nothing else. Instances were moved. Read the request for `outcome`. |
| 500 | Any other failure of the server's. For a waive nothing was written and the request still waits. |

A refusal never carries a reply: a status that is not 200 has `error` and
nothing else.

### Rejecting, and withdrawing

`POST /api/v1/deviation-requests/{id}/reject`, body `{"reason": "…"}`. Any
administrator of the organization may. Sent by whoever asked, it is a
withdrawal, and there is no other route for one. The reply is
`{"request": {...}}` with `status: "rejected"`, `decided_by`, `decided_at` and
`decision_reason`; a withdrawal reads the same, with `decided_by` the
requester and `self_approved: false`.

| Status | When |
| :-- | :-- |
| 200 | Rejected, or withdrawn. Nothing about any instance changed. |
| 400 | The body could not be read, as for an approval. No reason: *Say why: a rejection keeps its reason with the record.* A reason over 2,000 characters. |
| 400 | Already decided, in an approval's sentences. A migration that is running: *…; it is being applied.* |
| 400 | **Recorded first, then refused**: *This request already expired on ….* It now reads `expired`. |
| 401, 403, 404, 500 | As for an approval. |

### What every one of the four does

- **The body is read before the caller is checked**, as on every route. In
  order: the rate limit, the size of the body and the token (401); the
  `Idempotency-Key`; the body; whether the caller belongs to the organization
  (401); the role (403); and only then the id in the address and the request.
  So an account that is not an administrator and sends a decision that cannot
  be read gets the 400 for the body, which says nothing of any request. With
  a readable body it gets the 403, whether or not there is such a request.
- **`Idempotency-Key`** follows the header's own rules on the two decisions.
  A retry under the same key is the first answer again, with
  `Idempotency-Replayed: true`, for 15 minutes; the same key with another
  body is the header's plain-text 409. One of the refusals that records
  something first is kept like any answer: sent again under its key it is the
  same 400, and nothing is closed twice
  (`TestARefusalThatRecordedSomethingIsAnsweredOnceUnderAKey`). With no key, a
  second approval is the 400 that says who approved, and the first refusal
  that recorded an expiry is followed by the plain *This request expired on
  ….*
- **A 400 carries a sentence and no machine-readable code**, as on the route
  above. Branch on the status, and on the request's `status` read afterwards.
- **No deadline on the server.** An approval of a migration lasts as long as
  its run.

### A migration that waits

`POST /api/v1/definitions/versions/migrate` plans a migration, and applies it
when the body says `"dry_run": false`. Its fields are `source_definition_id`,
`target_definition_id`, `node_mapping`, `node_actions`, `instances`,
`acknowledge` and `dry_run`; what each does is in
[Changing a process that is already running](process-change-in-flight.md#5-what-the-engine-does-now).
Two fields of its plan say whether an apply will be made or asked for:

- **`requires_second_approver`**, always present.
- **`second_approver_reasons`**, left out when nobody is asked: at most ten
  sentences and, when there are more, one last sentence counting the rest.
  They are sentences to show. Do not parse them.

| Status | When | Reply |
| :-- | :-- | :-- |
| 200 | A dry run, whatever its plan says. | `plan`, `"applied": false`, `"passed_over": []`, `"passed_over_in_all": 0` |
| 200 | An apply that needs nobody else. | `plan`, `applied`, `passed_over`, `passed_over_in_all`. No `pending_approval`. |
| 200 | An apply over a version no instance is on, even one that names a skip. | `"applied": true`, nobody passed over, `plan.instances: 0`. Nothing was written and nobody was asked. |
| **202** | An apply whose plan needs a second administrator: the first ask, and the same apply sent again by whoever asked while it waits. | `plan`, `"applied": false`, `"passed_over": []`, `"passed_over_in_all": 0`, and `pending_approval` |
| 400 | The same apply by a different administrator while it waits: *The same migration is already waiting for approval (request …, asked by boss); approve or reject that one.* While its approved run is going: *The same migration was approved by deputy and is being applied now (request …); nothing new was asked for.* | `error` |
| 400 | An apply whose plan refuses: the refusals, joined by `; `. A plan that refuses is not sent for approval, and asks nobody. | `error` |
| 401, 403, 404 | No token; not an administrator of the organization; a version of another organization. The same answer whether or not a request waits. | |

`pending_approval` is the same object as on a waive:
`request_id`, `status`, `requested_by`, `expires_at` and `because`.

**An approval cannot be sent in the body.** A request id or an approver put
there is ignored, and the apply is sent for approval like any other. A
migration that needs a second administrator is applied only by the approve
route (`TestAnApprovalSentInTheBodyIsIgnoredAndMovesNothing`).

**Asking again.** The same migration is the same request: the two versions,
the mapping, each decision and its reason, what was acknowledged and the
instances named. Change any of them and it is another migration, with a
request of its own. A request that waited past its deadline, or was approved
and whose run never reported, holds nothing: the same apply closes it and
makes a fresh request.

### The instances a run passed over

The migrate route's reply, and the reply to an approval of a migration, say
which instances the run left alone:

```json
{
  "passed_over": [
    { "instance_id": "0199…",
      "cause": "left_the_step",
      "steps": [ { "node_id": "opsApprove", "name": "Operations approve" } ],
      "steps_in_all": 1,
      "reason": "It was no longer waiting at \"Operations approve\" when the migration reached it, …" }
  ],
  "passed_over_in_all": 1
}
```

- **`passed_over`** lists at most 200, in the order the run came to them.
  **`passed_over_in_all`** is how many there were. Both are always present on
  the migrate route, `[]` and `0` when the run left nobody. Until this release
  the list held every one and there was no count.
- **`cause`** is one of the eight codes below, never empty. **`reason`** is
  the same thing as an English sentence, and stays: say `reason` for a
  `cause` you do not know.
- **`steps`** is the steps the cause is about, as `{node_id, name}`: at most
  ten, in the order of their ids, `[]` for a cause about no step.
  **`steps_in_all`** is how many there were. `name` is the step's name in the
  version the instance runs, or its id where it has none. Each of the two is
  cut at 255 characters, so a `node_id` here is for showing, not a key to
  send back.

| `cause` | Given when | `steps` |
| :-- | :-- | :-- |
| `not_planned_for` | The instance was not on the version being migrated from when the migration was planned: it started there, or was moved there, afterwards. | none |
| `left_the_step` | A skip, a cancel or a hold found it, under its lock, no longer waiting at the step it decides. That includes an instance that finished meanwhile, in a migration that decides work. | the step |
| `already_moved` | Another run of a migration had moved it off the version. | none |
| `no_longer_running` | It finished, or was ended, after it was listed, in a migration that had nothing to decide for it. | none |
| `waiting_to_be_decided` | It held a token on steps this migration decides, and no decision had settled it. | those steps |
| `nowhere_to_land` | It held work on steps the new version has no step for and the mapping does not cover. | those steps |
| `left_where_nothing_decides` | It had an open task or a waiting event, and no token, on steps this migration decides. | those steps |
| `counters_would_merge` | It held progress counters on two steps the mapping puts onto one. | none |

An instance gets one cause: the first that applies, in that order. `applied`
can be `true` beside a list that is not empty, and even when no instance
changed version: a skip that was made counts as acting on an instance.

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
Content-Type: application/json
X-Metis-Timestamp: 1767225600
X-Delivery-Id: evt_0001
X-Metis-Signature: v2=bb84c51b81746277520c49e834388c1fdaa0680755ef30bc087b6a7eba752675

{"order":{"id":"ORD-1"}}
```

The endpoint is public, because a partner's configuration screen has nowhere to
put a token this engine would recognise. What authenticates a delivery is its
signature, computed with the secret you were given when the webhook was
created. **The secret is shown once**; it is encrypted at rest and no read path
returns it. Use it exactly as shown — its characters are the key, it is not
base64 to be decoded.

### Signing a delivery

Three headers, all required:

| Header | Value |
| :-- | :-- |
| `X-Metis-Timestamp` | When this attempt is sent, in Unix **seconds** |
| `X-Delivery-Id` | Your own ID for the event: unique per event, **the same on every retry**, at most 191 characters and **without a dot** |
| `X-Metis-Signature` | `v2=` and the hex HMAC-SHA256, keyed with the secret, of `<timestamp>.<delivery id>.<raw body>` |

```
X-Metis-Signature = "v2=" + hex(hmac_sha256(secret, timestamp + "." + delivery_id + "." + raw_body))
```

- **Sign the exact bytes you send.** Serialise the JSON once, sign that, send
  that. Re-encoding after signing changes the bytes, and the signature no
  longer matches.
- **Sign every attempt when it is sent, retries included.** A delivery signed
  more than **5 minutes** from Metis's clock, either way, is refused, so a stored
  copy cannot be replayed later and a retry needs a fresh timestamp.
- **Keep `X-Delivery-Id` the same across retries of one event.** A delivery
  whose ID Metis has already acted on is answered `202` with
  `"duplicate": true` and not acted on again; IDs are remembered for 48 hours.
  Because the ID is signed, a captured delivery sent again under a new ID no
  longer matches its signature.
- **No dot in the ID.** The signed string is split by its dots, and a body has
  dots of its own: allowing one in the ID would let the same signature read as
  a longer ID and a shorter body. A UUID, a ULID or an `evt_…` ID is fine. An ID
  with a dot, or longer than 191 characters, is refused with a `400` that says
  so.

Check your code before sending anything: with the secret
`your-webhook-secret`, timestamp `1767225600`, delivery ID `evt_0001` and body
`{"order":{"id":"ORD-1"}}`, the signature is
`v2=bb84c51b81746277520c49e834388c1fdaa0680755ef30bc087b6a7eba752675`.

Go:

```go
func signV2(secret, timestamp, deliveryID string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "." + deliveryID + "."))
	mac.Write(body)
	return "v2=" + hex.EncodeToString(mac.Sum(nil))
}

// For each attempt, retries included:
timestamp := strconv.FormatInt(time.Now().Unix(), 10)
req.Header.Set("Content-Type", "application/json")
req.Header.Set("X-Metis-Timestamp", timestamp)
req.Header.Set("X-Delivery-Id", deliveryID)
req.Header.Set("X-Metis-Signature", signV2(secret, timestamp, deliveryID, body))
```

Node.js (18 or later):

```js
import { createHmac } from "node:crypto";

function signV2(secret, timestamp, deliveryId, body) {
  return "v2=" + createHmac("sha256", secret)
    .update(`${timestamp}.${deliveryId}.`)
    .update(body)
    .digest("hex");
}

// For each attempt, retries included:
const body = JSON.stringify(event); // sign and send these exact bytes
const timestamp = Math.floor(Date.now() / 1000).toString();
await fetch(url, {
  method: "POST",
  headers: {
    "Content-Type": "application/json",
    "X-Metis-Timestamp": timestamp,
    "X-Delivery-Id": event.id,
    "X-Metis-Signature": signV2(secret, timestamp, event.id, body),
  },
  body,
});
```

Python:

```python
import hashlib, hmac, json, time, urllib.request

def sign_v2(secret: str, timestamp: str, delivery_id: str, body: bytes) -> str:
    message = f"{timestamp}.{delivery_id}.".encode() + body
    return "v2=" + hmac.new(secret.encode(), message, hashlib.sha256).hexdigest()

# For each attempt, retries included:
body = json.dumps(event).encode()  # sign and send these exact bytes
timestamp = str(int(time.time()))
request = urllib.request.Request(url, data=body, method="POST", headers={
    "Content-Type": "application/json",
    "X-Metis-Timestamp": timestamp,
    "X-Delivery-Id": event["id"],
    "X-Metis-Signature": sign_v2(secret, timestamp, event["id"], body),
})
urllib.request.urlopen(request, timeout=10)
```

Each webhook names the BPMN message a delivery becomes, and optionally a FEEL
expression over the payload — `order.id` — picking the value that says which
waiting instance it concerns. Leave that empty and every delivery starts a
process instead of moving one.

Responses:

| Status | Meaning |
| :-- | :-- |
| `202` | Accepted — or, with `"duplicate": true`, already acted on and not acted on again |
| `400` | Signed correctly but not usable: the timestamp is missing or more than 5 minutes out, there is no delivery ID, the body is not a JSON object, the correlation value is missing, or the webhook no longer accepts legacy signatures. The body says which, and what to change |
| `401` | Anything about who sent it: an unknown address, a switched-off webhook, a signature that does not match. Deliberately indistinguishable, so the reply cannot be used to find addresses — a `400` explanation is only given to a delivery whose signature matched |

A `401` for a sender you believe is right is nearly always the wrong secret, a
body re-encoded after signing, a timestamp or delivery ID header that differs
from what was signed, or a timestamp in milliseconds.

### Legacy signatures, and moving off them

Before v2 a signature was the HMAC of the body alone, sent as
`X-Signature-256` (or `X-Hub-Signature-256`, `X-Signature`,
`Stripe-Signature`, `X-Webhook-Signature`), optionally prefixed `sha256=`, with
the delivery ID in `X-Delivery-Id`, `X-GitHub-Delivery`, `X-Request-Id` or
`Idempotency-Key`. It says nothing about when a delivery was sent or under which
ID, so anyone who captured one could post it again under a new ID, as often as
they liked, and each copy was acted on.

- **Webhooks created from this release on accept v2 only.**
- **Webhooks that existed before it accept legacy signatures for 90 days from
  the upgrade**, so no sender is cut off on the day. The webhooks screen shows
  the date under each of them, and the reply to an accepted legacy delivery
  carries it as `legacy_signatures_until`.
- Inside that window a legacy signature works as it always did — replays
  included. That is the risk the window accepts; move senders early.
- After it, a legacy-signed delivery is refused with `400` and a message saying
  how to sign with v2. v2 is accepted throughout, and a delivery carrying
  `X-Metis-Signature` is judged by v2 alone, so a sender can switch whenever it
  is ready.

To move a sender:

1. On the Connectors page, under *Incoming webhooks*, find the webhooks that say
   *Still accepts legacy signatures until …*. The server logs each legacy
   delivery it accepts, with the webhook's name: *Accepted a webhook delivery
   signed the legacy way*.
2. Send the sender's developers this section, or the screen's *How to move the
   sender to v2*. The secret does not change.
3. They send `X-Metis-Timestamp`, a stable `X-Delivery-Id`, and
   `X-Metis-Signature` as above, signing each attempt as it is sent.
4. Their replies stop carrying `legacy_signatures_until`, and the log line stops
   naming that webhook. [`upgrading.md`](upgrading.md) shows how to close a webhook's
   window early once its sender has moved.

A sender that can only sign the body — a service whose webhook settings you
cannot change, such as GitHub's `X-Hub-Signature-256` — cannot produce v2.
Before its window closes, put a small relay in front of Metis that checks the
sender's own signature and re-signs each delivery with v2 as it forwards it.

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
nothing to say, not an instruction to unassign. If neither the table nor the
diagram names anybody, the task is an administrator's or an operator's to take,
as any task that names nobody is.

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
takes a **platform administrator** — an administrator of every organization
whose account id whoever operates the installation has listed in
`METIS_PLATFORM_ADMINS`. Anybody else is refused with a 403 that names the
setting and gives them their account id to pass on. An installation with one
organization needs nothing configured: its administrators of every
organization may, as they always could. The Administrator role granted in one
organization does not admit to this on any installation (see *Roles: in one
organization, or in every one*).

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
