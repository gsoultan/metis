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
