# Deploying Metis on Kubernetes

`metis.yaml` is a complete deployment, not a skeleton. Every field in it is
there because leaving it out breaks something specific, and each one says which
in a comment.

```bash
kubectl create namespace metis

kubectl -n metis create secret generic metis-secrets \
  --from-literal=ENCRYPTION_KEY="$(openssl rand -hex 16)" \
  --from-literal=JWT_SECRET="$(openssl rand -base64 48)" \
  --from-literal=DATABASE_URL="host=postgres user=metis password=... dbname=metis sslmode=require"

kubectl -n metis apply -f metis.yaml
```

Then open the service and the first request walks through the setup wizard.
There is no default account.

## Four things that are easy to get wrong

**Back up `ENCRYPTION_KEY` separately from the database.** Process and task
variables are encrypted with it. A database backup restored without it gives you
unreadable rows and no way back. This is the single most expensive mistake
available here — see [`docs/recovery.md`](../../docs/recovery.md).

**Set `METIS_TRUSTED_PROXIES` to match your ingress.** `X-Forwarded-For` is only
believed from peers named there, because it is a header the client sets;
believing it unconditionally is how one address took 30 requests through a limit
of 3. The manifest allows RFC1918, which is where an in-cluster ingress connects
from. If yours has a public address, add it — otherwise every client behind it
shares a single rate-limit bucket.

**Liveness is `/healthz`, readiness is `/readyz`.** Only `/readyz` checks the
database. Pointing liveness at it means a brief database blip restarts every
pod, turning an outage into a crash loop.

**`replicas: 1` is deliberate.** Job claiming, migrations, correlation,
idempotency and live UI updates are all safe across replicas. HTTP rate limiting
and connector rate limits/circuit breakers are not — they hold per-process
state, so with N replicas each of those limits applies N times over.
[`docs/recovery.md` §2.1](../../docs/recovery.md) has the table. Raising this is
a decision to make deliberately, not a default to inherit.

## Alerting

`alerts.yaml` is a `PrometheusRule` for kube-prometheus-stack. Six rules, each
with a description saying what it means and where to look — an alert whose
runbook is "ask whoever wrote it" gets silenced the first time it fires at 3am.

```bash
kubectl -n metis apply -f alerts.yaml
```

Two of them are worth knowing about before they fire. **MetisNoTraffic** looks
paranoid and is not: the alarming failures here are quiet ones — a job worker
that stopped claiming, or a tenant scope answering every query with nothing,
both look exactly like an idle system. And **MetisReadLatencyOverTarget** fires
at 150ms, where a load test at 500k instances measured under 5ms; if it fires,
something changed in kind, not a system merely under load.

## Measuring before you commit

`tests/loadtest` seeds a configurable volume into PostgreSQL and measures the
same endpoints these alerts watch. Run it against hardware like yours, with
your expected data volume, before deciding the targets are achievable:

```bash
METIS_TEST_POSTGRES_DSN='host=... user=... dbname=metis sslmode=disable' \
METIS_LOADTEST=1 METIS_LOADTEST_INSTANCES=500000 METIS_LOADTEST_TENANTS=50 \
go test ./tests/loadtest/ -v -timeout 40m
```

Measured on a laptop container at 500k instances and 500k tasks across 50
tenants: p95 between 1.9ms and 5.2ms on every endpoint, against a 150ms target.

## The image

Nothing has been tagged yet, so no image is published. Build and push your own:

```bash
make docker
docker tag metis:latest <your-registry>/metis:<version>
docker push <your-registry>/metis:<version>
```

Pushing a `v*` tag runs `.github/workflows/release.yml`, which publishes
`ghcr.io/<owner>/metis`. Pin a digest in a real deployment: a moving tag makes a
rollback ambiguous, which is the one moment it needs not to be.

## What this does not include

An Ingress, a PostgreSQL, a NetworkPolicy or a PodDisruptionBudget — those
depend on your cluster, and a guess at them is worse than their absence. The
database is expected to already exist and to be named in `DATABASE_URL`.
