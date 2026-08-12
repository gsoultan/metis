# How data moves through a process

Two worked examples, traced value by value.

If you take one idea from this page, take this one:

> **A running process carries a single bag of variables.**
> Every step reads from that bag and writes back to it. Nothing is "passed"
> between steps — steps just take what they need and leave what they produce.

That is the whole model. Everything below is a consequence of it.

---

## The four ways the bag changes

| What | Reads | Writes |
| :-- | :-- | :-- |
| **Start** | — | whatever you pass when starting the process |
| **Ask a person** (User Task) | shown on the form | whatever the person submits |
| **Apply a business rule** (Business Rule Task) | the variables the decision's inputs name | the decision's outputs |
| **Call another system** (Service Task) | the variables named by `input_*` | the response fields named by `output_*` |
| **Choose one path** (Gateway) | variables named in the conditions | nothing |

A gateway is the only one that reads without writing. That is worth noticing:
**a gateway never changes your data, it only chooses where to go next.**

---

# Example 1 — Expense approval

A person submits an expense. The amount decides who has to approve it. The
approver's answer decides whether it gets paid.

```
Start ──▶ Decide approval level ──▶ Choose one path ──┬─▶ Ask the manager ──▶ Finish
          (business rule)            (gateway)         └─▶ Ask the director ─▶ Finish
```

## The decision table

`expense-approval-level`, hit policy **Use the first line that matches**:

| Condition: `amount` | Result: `approvalLevel` | Result: `approver` |
| :-- | :-- | :-- |
| `< 100` | `"auto"` | `"system"` |
| `< 1000` | `"manager"` | `"line-manager"` |
| *any value* | `"director"` | `"finance-director"` |

Read a row left to right: *if the conditions on the left all hold, the results
on the right apply.* An empty condition cell means "this column does not matter
for this line" — which is why the last row matches anything.

**The `amount` in the Condition header is a variable name.** That is the join
between the table and the process: the decision looks up `amount` in the bag.

## Trace

### 1. Someone starts the process

```json
POST /api/v1/process.ProcessService/StartProcess
{
  "projectId": "…",
  "definitionKey": "expense-approval",
  "variables": {
    "amount": 2400,
    "currency": "GBP",
    "description": "Conference tickets, 3 people",
    "submittedBy": "alice"
  }
}
```

**The bag now:**

```json
{ "amount": 2400, "currency": "GBP",
  "description": "Conference tickets, 3 people", "submittedBy": "alice" }
```

### 2. "Decide approval level" runs

It looks up `amount` → **2400**.

- `< 100`? No.
- `< 1000`? No.
- *any value*? Yes → `approvalLevel: "director"`, `approver: "finance-director"`

Those two results are written into the bag.

**The bag now:**

```json
{ "amount": 2400, "currency": "GBP",
  "description": "Conference tickets, 3 people", "submittedBy": "alice",
  "approvalLevel": "director",          // ← added by the decision
  "approver": "finance-director" }      // ← added by the decision
```

Nothing was replaced. The decision only added.

### 3. "Choose one path" reads the bag

Two paths leave the gateway, each with a condition:

| Path | Condition |
| :-- | :-- |
| → Ask the manager | `approvalLevel = manager` |
| → Ask the director | `approvalLevel = director` |

> **Watch the syntax.** It is a single `=`, and the value is written plain —
> no quotes. `approvalLevel == "director"` looks more like code and **silently
> never matches**: the evaluator splits the condition on `=`, gets three pieces
> instead of two, and gives up. The path is simply never taken, and the process
> stops at the gateway with "no outgoing sequence flow could be selected".
>
> This caught the author of this page while writing it, which is the best
> argument for reading the incident message rather than guessing.

`approvalLevel` is `"director"`, so the second path is taken. **The bag is
unchanged** — a gateway only chooses.

> If no condition matches and no default path is set, the process raises an
> incident rather than guessing. That is deliberate: silently taking the first
> path is how an expense gets approved that should have been rejected.

### 4. "Ask the director" creates a task

A task appears in the finance director's inbox, showing `amount`, `currency`
and `description` — those come from the bag.

They approve with a note:

```json
POST /api/v1/process.TaskService/CompleteTask
{
  "id": "…",
  "userId": "finance-director",
  "variables": { "approved": true, "approverNote": "Budgeted under L&D" }
}
```

**The bag now:**

```json
{ "amount": 2400, "currency": "GBP",
  "description": "Conference tickets, 3 people", "submittedBy": "alice",
  "approvalLevel": "director", "approver": "finance-director",
  "approved": true,                        // ← added by the person
  "approverNote": "Budgeted under L&D" }   // ← added by the person
```

### 5. Finish

The bag is the record of what happened. Every value in it can be traced to the
step that put it there — which is what the instance's variable history shows.

## The same process, a different amount

Start with `amount: 40` and only step 2 differs:

- `< 100`? **Yes** → `approvalLevel: "auto"`, `approver: "system"`

No path matches `"auto"`, so the process raises an incident telling you the
table produced a value the diagram has no route for. **This is a modelling
mistake the engine surfaces rather than hides** — you would add a third path
for `approvalLevel == "auto"` going straight to Finish.

---

# Example 2 — New supplier check

Shows the part people find hardest: getting data **into** and **out of**
another system, where the names on each side are different.

```
Start ──▶ Look up the company ──▶ Decide the risk band ──▶ Ask compliance ──▶ Finish
          (service task)          (business rule)          (user task)
```

## The service task's mapping

Your variables are named for your business. The API's fields are named for
theirs. Two settings on the step translate between them:

| Setting | Meaning |
| :-- | :-- |
| `input_companyNumber` = `"registration_id"` | take **my** `companyNumber`, send it as **their** `registration_id` |
| `output_credit_score` = `"creditScore"` | take **their** `credit_score` from the response, store it as **my** `creditScore` |

Read `input_X = "Y"` as *"my X goes out as their Y"*, and `output_A = "B"` as
*"their A comes back as my B"*. The prefix says which direction it travels.

## Trace

### 1. Start

```json
{ "companyNumber": "09876543", "supplierName": "Northwind Ltd", "requestedBy": "bob" }
```

### 2. "Look up the company" calls the API

The step is configured with:

```
http_url            https://api.example.com/companies/lookup
http_method         POST
input_companyNumber registration_id
output_credit_score creditScore
output_status       companyStatus
```

**What is sent** — note the name changed on the way out:

```json
{ "registration_id": "09876543" }
```

**What comes back:**

```json
{ "registration_id": "09876543", "credit_score": 42,
  "status": "active", "incorporated": "2011-04-02" }
```

**What is kept.** Only the two fields with an `output_` mapping. `incorporated`
is discarded — the process never asked for it.

**The bag now:**

```json
{ "companyNumber": "09876543", "supplierName": "Northwind Ltd", "requestedBy": "bob",
  "creditScore": 42,             // ← their credit_score
  "companyStatus": "active" }    // ← their status
```

> With no `output_*` settings at all, the whole response is merged into the bag
> under its own field names — you would get `credit_score`, not `creditScore`.
> Mapping is what keeps your process speaking your language.

### 3. "Decide the risk band"

`supplier-risk`, hit policy **Use the first line that matches**:

| Condition: `creditScore` | Condition: `companyStatus` | Result: `riskBand` |
| :-- | :-- | :-- |
| `< 30` | *any value* | `"high"` |
| `< 60` | `"active"` | `"medium"` |
| *any value* | *any value* | `"low"` |

Two conditions on a row mean **both** must hold.

`creditScore` is 42, `companyStatus` is `"active"`:

- Row 1: `42 < 30`? No.
- Row 2: `42 < 60`? Yes. `"active" == "active"`? Yes. → `riskBand: "medium"`

**The bag now** gains `"riskBand": "medium"`.

### 4. "Ask compliance"

The reviewer sees the supplier name, the credit score and the risk band —
values that came from three different places and now sit side by side, because
they are all just entries in the same bag.

They complete the task with `{ "decision": "approved", "reviewedBy": "carol" }`.

---

## The three questions this answers

**"Where did this value come from?"**
Something wrote it: the start call, a person completing a task, a decision's
output, or a service task's `output_` mapping. Those are the only four sources.

**"Why did it take that path?"**
A gateway read a variable and compared it. The variable was already in the bag
before the gateway ran — gateways do not compute, they only choose.

**"Why is my variable empty?"**
Almost always one of:

- the name does not match — `creditScore` and `credit_score` are different keys
- a service task returned the field but had no `output_` mapping for it
- the step that would have set it has not run yet
- a decision matched a row whose result cell was blank

---

## Try it

Both processes are in `docs/examples/`. Import a definition in the designer,
then start it with the variables shown above and watch the instance's variable
history — it shows the bag after each step, which is this page in live form.
