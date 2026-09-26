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
listing, claiming and completing tasks — needs none. An administrator grants
Designer, Operator or Administrator on the account in Metis afterwards; a
`roles` claim in the token grants nothing. Changing the password in Metis is
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
specific there is `AssignTask`, allowed for an administrator or for whoever
currently holds the task — and, for a task nobody was named for, for an
operator.

**Who may take a task.** A task with an assignee is its assignee's to
complete. One offered to candidate users or groups may be claimed and
completed by those people and the members of those groups. One that names
nobody — no assignee, no candidates — is an administrator's or an operator's:
anybody else claiming, completing or handing it on gets a 403 that says the
task has no assignee and no candidates and who can take it, and
`ListTasksByCandidates` lists it only for them. A manual task is the
exception, open to anybody in its organization. `METIS_ALLOW_UNASSIGNED_TASK_CLAIMS=true`
brings back the old rule, where anybody signed in could take such a task, for
a migration window.

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
   "topic": "reverse-charge", "exchange": "billing", "routing_key": "charges.reverse"}
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
| `queue` (consumer) | The queue to consume. Metis declares it — durable, no arguments — and a dead-letter queue beside it named `<queue>.dlq`. A queue that already exists with other arguments is refused by the broker. |
| `message` (consumer) | The BPMN message name each message is correlated as. |

One bridge per project and topic, and one consumer per project and queue: a
repeat is refused. At start, each entry says so in the log — `Started a
RabbitMQ bridge` or `Started a RabbitMQ consumer`, with its project,
organization, topic or queue, and the broker's host and virtual host, never its
password — and then `A RabbitMQ bridge connected to its broker` or `A RabbitMQ
consumer is consuming from its queue`, which it says again after every
reconnection.

When something is wrong:

- **An entry that cannot be read** — not JSON, a setting misspelt or missing,
  an id that is not one — is named by its position, as in
  `METIS_RABBITMQ_BRIDGES, bridge 2: "topic" is required`, and skipped. The
  others run, and the server starts either way.
- **A project or connection that does not exist, or a connection that cannot be
  used**, is logged with the reason and tried again after 5 seconds, then 10,
  20 and so on, up to every 5 minutes. Fix it and the bridge starts, with no
  restart. The connection is read once, when the bridge starts: a URL changed
  later takes effect at the next restart.
- **A broker that is down**, at start or later, is tried again every 5 seconds,
  and each attempt is logged. Nothing is lost meanwhile: tasks wait in Metis,
  and messages wait on the broker.
- **An exchange that does not exist**, or that the broker's user may not
  publish to, makes the broker close the bridge's channel on the first publish.
  The bridge does not open another until its connection drops, so every task is
  handed back at every poll, and fixing the exchange is not enough: restart
  after fixing it.

### What the bridge publishes, and what a worker does with it

Every 5 seconds each bridge takes up to ten tasks of its topic and publishes
each one as JSON — the task as the external-task API returns it, with its `id`,
`variables`, `worker_id` and `lock_expiration` — with a `task_id` header.
Publishes are confirmed and `mandatory`: a task the broker does not accept, or
cannot route to a queue, is handed back at once without spending one of its
retries, and is offered again at the next poll.

The worker completes the task, or reports its failure, through the
external-task API like any other worker, with the `worker_id` the message
carries, which is `messaging-bridge`:

```bash
curl -X POST $GOBPM/api/v1/external-tasks/$TASK_ID/complete \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"worker_id":"messaging-bridge","variables":{"reversed":true}}'
```

- **The worker has 30 seconds.** The bridge locks each task for 30 seconds, and
  that is the worker's whole budget, time spent waiting on the queue included.
  A task still open when its lock runs out is published again at the next
  poll, and only one completion is accepted. So a slow worker, or a long
  queue, means the same task delivered twice and done twice — harmless only if
  the handler is idempotent, which any external-task worker's must be. Keep
  the queue short.
- **A topic is shared by an organization's projects.** A bridge publishes every
  task of its topic in the project's organization: the same tasks a worker of
  that organization fetching the topic through the API would get. Run one
  bridge per topic per organization, and no API worker on a bridged topic.
- A bridge reads the main runtime. Tasks of processes running in an
  environment of their own are not bridged.
- The message carries the task's variables. Use `amqps://` for a broker outside
  your network.

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
