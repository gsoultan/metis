# Releasing

A tag is what publishes. `.github/workflows/release.yml` runs on a `v*` tag and
nothing else: it pushes the image to `ghcr.io/gsoultan/metis` with a software
bill of materials, build provenance and a keyless signature, and attaches Linux
archives to the GitHub release. A tag with a suffix — `v0.4.0-rc.1` — publishes
the same way, as a GitHub pre-release (`prerelease: auto` in `.goreleaser.yaml`),
and leaves the image's `latest` where it was, so a release candidate is never
anybody's default pull.

So everything that decides whether a release is good happens before the tag, and
most of it can only happen with an installation's own data and its own identity
provider, which CI has neither of.

## The order

1. **Everything for the release is on `main`**, and CI is green on the merge
   commit — not only on the pull requests that made it.
2. **The changelog reads as one release.** `[Unreleased]` has one section per
   kind (Security, Added, Changed, Fixed), and an **Upgrading** list first,
   each line pointing into [`upgrading.md`](upgrading.md).
3. **Rehearse the upgrade on a copy of production data:**
   `scripts/upgrade-rehearsal.sh <backup-directory>`
   ([Rehearse it first](upgrading.md#rehearse-it-first)).
4. **Tag a release candidate** from `main`: `v0.4.0-rc.1`. Check what was
   published is what was built:

   ```bash
   gh attestation verify oci://ghcr.io/gsoultan/metis:v0.4.0-rc.1 --owner gsoultan
   cosign verify ghcr.io/gsoultan/metis:v0.4.0-rc.1 \
     --certificate-identity https://github.com/gsoultan/metis/.github/workflows/release.yml@refs/tags/v0.4.0-rc.1 \
     --certificate-oidc-issuer https://token.actions.githubusercontent.com
   ```

5. **Stage it.** Deploy the candidate to staging and work through the list
   below. A finding is a fix on `main` and `rc.2`, not a patch on staging.
6. **Canary it in production** ([Rolling out through a
   canary](runbooks.md#rolling-out-through-a-canary)). Take a backup first: the
   canary runs the release's migrations as it starts, and a rollback does not
   undo a migration ([Rolling back a release](runbooks.md#rolling-back-a-release)).
7. **Release.** One commit on `main` renames `[Unreleased]` to
   `[0.4.0] - <date>`; tag that commit `v0.4.0`, annotated, with a message that
   says what an operator needs to know first — the way `v0.3.0`'s does.

## 0.4.0 on staging

Staging needs what production has: PostgreSQL restored from a recent backup, the
identity provider, a RabbitMQ broker if production uses one, and at least one
organization with the number of projects the largest production one has.

Each item says what to do and what should be true. Keep the answers — they are
the release's evidence, and the next release's baseline.

### Before the upgrade

- [ ] **Local accounts come back with OIDC on.** If OIDC was turned on to keep
  local accounts out, it no longer does. Run the query in [With OIDC on, local
  accounts sign in again](upgrading.md#with-oidc-on-local-accounts-sign-in-again),
  delete the accounts nobody should use, keep one administrator offline.
- [ ] **Tasks that name nobody.** Run the query in [Tasks nobody was named
  for](upgrading.md#tasks-nobody-was-named-for-are-the-administrators-and-operators)
  and decide, per process, whether it gets an assignee or candidates, or
  whether `METIS_ALLOW_UNASSIGNED_TASK_CLAIMS=true` covers a migration window.
- [ ] **Decisions that may answer differently.** Run the checks in [Decision
  cells see the rest of the case](upgrading.md#decision-cells-see-the-rest-of-the-case)
  and note every table they find.
- [ ] **A backup**, taken and restored once (`scripts/backup.sh`,
  `scripts/restore.sh`), so the rollback below has something to roll back to.

### The upgrade

- [ ] **Every migration applies, and how long each took.** The log has one
  `Migration applied` line per version with its duration; `schema_migrations`
  keeps `duration_ms`. Anything over a few seconds on staging is a maintenance
  window in production, at production's size.
- [ ] **Migration 28 under load.** Hold a read of `audit_logs` open — in psql,
  `BEGIN; SELECT count(*) FROM audit_logs;` and leave the transaction open — then
  start the upgrade. It should stop within two seconds with the message in
  [Migration 28 can stop the
  upgrade](upgrading.md#migration-28-can-stop-the-upgrade-when-the-audit-table-is-busy).
  End the transaction and start it again: it applies.
- [ ] **Nothing was lost.** Counts of running instances, open tasks, due jobs and
  incidents are the same before and after.
- [ ] **The boot is quiet.** `metis_schema_drift_items` is 0: a model change
  whose migration is missing logs `Schema drift` at boot. No warning repeats.
  `METIS_ALLOW_UNASSIGNED_TASK_CLAIMS`, if set, warns once per boot, as it
  should.

### After the upgrade

- [ ] **Signing in, both ways.** A local administrator signs in with OIDC on. An
  OIDC account signs in and lands in the organizations
  `METIS_OIDC_ORGANIZATION_CLAIM` names; a first sign-in has no role. A
  person the claim places nowhere gets the 403 that names the claim.
  `metis --reset-password <username>` refuses an account linked to the identity
  provider.
- [ ] **A task that names nobody.** A member cannot claim it (403 with the
  sentence from the upgrade notes); an operator can, and can assign it. The
  designer warns about the step.
- [ ] **Notifications.** The bell's number matches what the list says is unread,
  for somebody with more than a thousand notifications; older pages load. Asking
  for somebody else's (`GET /api/v1/notifications?user_id=<someone else>`) is a
  403.
- [ ] **A decision cell that names another column** evaluates as the upgrade
  notes say, and the tables found before the upgrade answer as expected.
- [ ] **An instance's history** reads start to end in the order it ran; the OCEL
  export names each case's process key and version.
- [ ] **RabbitMQ, if used.** With `METIS_RABBITMQ_BRIDGES` and
  `METIS_RABBITMQ_CONSUMERS` set, each bridge and consumer logs that it
  connected. Restart the broker: each logs the reconnect and carries on, and no
  task stays locked past its `lock_seconds`.
- [ ] **The largest organization.** Page through its instances and tasks, and
  compare `metis_tenant_scope_reads_total` with the request rate: one read of
  the organization's project ids per request, not one per statement.
- [ ] **Connect clients.** A member calling an administrator's method over
  Connect gets `permission_denied`, and a call with no token `unauthenticated`,
  where the first was `unknown` and an HTTP 500. A client that retried on those
  should stop retrying.
- [ ] **Screen reader and dark mode.** Every dialog's close button is announced
  with a name; the webhooks card and message badges are readable in dark mode.
- [ ] **Alerts.** `make alerts-test` passes, and the 5xx rate does not rise after
  the upgrade. Refusals over Connect no longer count as server errors, so where
  Connect clients are refused it falls.

### Strict tenant scope

0.4.0 still ships `METIS_FEATURE_STRICT_TENANT_SCOPE` off. Staging is where it
goes on: follow [The rollout](strict-tenant-scope.md#the-rollout) through a full
release cycle, and record every warning and its fix. Production follows when
staging has been quiet for that cycle — not as part of this release.

### Rolling back

- [ ] **The previous image boots against the migrated schema** and serves:
  instances advance, tasks complete. The schema is forward-only
  ([Rolling back a release](runbooks.md#rolling-back-a-release)), so this is
  what a rollback in production would be.
- [ ] **Then roll forward again**, and confirm nothing re-runs.
