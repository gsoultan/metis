# Releasing

A tag is what publishes. `.github/workflows/release.yml` runs on a `v*` tag and
nothing else: it pushes the image to `ghcr.io/gsoultan/metis` with a software
bill of materials, build provenance and a keyless signature, and attaches Linux
archives to the GitHub release. A tag with a suffix — `v0.5.0-rc.1` — publishes
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
4. **Tag a release candidate** from `main`: `v0.5.0-rc.1`. Check what was
   published is what was built:

   ```bash
   gh attestation verify oci://ghcr.io/gsoultan/metis:v0.5.0-rc.1 --owner gsoultan
   cosign verify ghcr.io/gsoultan/metis:v0.5.0-rc.1 \
     --certificate-identity https://github.com/gsoultan/metis/.github/workflows/release.yml@refs/tags/v0.5.0-rc.1 \
     --certificate-oidc-issuer https://token.actions.githubusercontent.com
   ```

5. **Stage it.** Deploy the candidate to staging and work through the list
   below. A finding is a fix on `main` and `rc.2`, not a patch on staging.
6. **Canary it in production** ([Rolling out through a
   canary](runbooks.md#rolling-out-through-a-canary)). Take a backup first: the
   canary runs the release's migrations as it starts, and a rollback does not
   undo a migration ([Rolling back a release](runbooks.md#rolling-back-a-release)).
7. **Release.** One commit on `main` renames `[Unreleased]` to
   `[0.5.0] - <date>`; tag that commit `v0.5.0`, annotated, with a message that
   says what an operator needs to know first — the way `v0.3.0`'s does.

## 0.5.0 on staging

Staging needs what production has: PostgreSQL restored from a recent backup, the
identity provider, a RabbitMQ broker if production uses one, and at least one
organization with the number of projects, groups and running instances the
largest production one has.

Each item says what to do and what should be true. Keep the answers: they are
the release's evidence, and the next release's baseline. The 0.4.0 list is in
this file's history.

### Before the upgrade

- [ ] **Groups that list roles.** Run the query in [A group's roles grant what
  they say](upgrading.md#a-groups-roles-grant-what-they-say) and, for each
  group, keep its roles or remove them. Every member of a group that keeps
  them will hold them.
- [ ] **Completions that set variables their form does not declare.** Follow
  [Completing a task sets only what its form
  declares](upgrading.md#completing-a-task-sets-only-what-its-form-declares)
  for each integration that completes tasks through the API, and decide
  whether `METIS_ALLOW_UNDECLARED_TASK_VARIABLES=true` covers a migration
  window.
- [ ] **Manual steps that name nobody.** Run the query in [Tasks nobody was
  named for](upgrading.md#tasks-nobody-was-named-for-are-the-administrators-and-operators),
  give each manual step somebody under *Who does this*, and decide whether
  `METIS_ALLOW_UNASSIGNED_TASK_CLAIMS=true` covers a migration window.
- [ ] **Integrations that hand tasks over, waive or migrate.** List each one
  that assigns, delegates, releases or edits tasks it does not hold, waives a
  step, or migrates with a skip. Each needs a `reason`, or a second
  administrator, after the upgrade ([Handing a task
  over](upgrading.md#handing-a-task-over-is-checked-and-recorded), [A second
  administrator](upgrading.md#a-second-administrator-approves-waivers-and-skips-migration-34)).
- [ ] **Organizations with one administrator.** Appoint a second, or name the
  organization in `METIS_SOLE_ADMINISTRATOR_ORGANIZATIONS`.
- [ ] **Retention.** Decide each of `METIS_RETENTION_FINISHED_INSTANCES_DAYS`
  (unset archives finished instances after 90 days; `0` keeps them live),
  `METIS_RETENTION_ARCHIVE_DAYS`, `METIS_RETENTION_VARIABLE_SNAPSHOTS_DAYS` and
  `METIS_RETENTION_COMPLETED_JOBS_DAYS` (unset keeps them). Staging is where a
  setting is tried first.
- [ ] **A backup**, taken and restored once (`scripts/backup.sh`,
  `scripts/restore.sh`), so the rollback below has something to roll back to.

### The upgrade

- [ ] **Every migration applies, and how long each took.** The log has one
  `Migration applied` line per version, 31 to 36, with its duration;
  `schema_migrations` keeps `duration_ms`. Anything over a few seconds on
  staging is a maintenance window in production, at production's size.
- [ ] **Migration 31 under load.** Hold a read of `tasks` open (in psql,
  `BEGIN; SELECT count(*) FROM tasks;` and leave the transaction open), then
  start the upgrade. It should stop within two seconds with the message in
  [Migration 31](upgrading.md#migration-31-a-task-records-which-run-of-its-step-it-is-for).
  End the transaction and start it again: it applies.
- [ ] **Nothing was lost.** Counts of running instances, open tasks, due jobs and
  incidents are the same before and after. Tasks that were `delegated` before
  the upgrade are claimed by their assignee after it (migration 32).
- [ ] **The boot is quiet.** `metis_schema_drift_items` is 0. Each of
  `METIS_ALLOW_UNDECLARED_TASK_VARIABLES`, `METIS_ALLOW_UNASSIGNED_TASK_CLAIMS`
  and `METIS_SOLE_ADMINISTRATOR_ORGANIZATIONS`, if set, warns once per boot, as
  it should, and a retention setting that is set is announced.

### After the upgrade

- [ ] **A group's roles.** A member of a group kept with a role can do what the
  role allows in the group's organization, and nothing more in another
  organization they belong to. Removing them from the group takes it away on
  their next request.
- [ ] **A second administrator.** A waive answers 202 and changes nothing; the
  same administrator's approval is a 403; a different administrator's approval
  makes it. A request left alone reads `expired` after
  `METIS_DEVIATION_APPROVAL_TTL`.
- [ ] **Handing a task over.** An administrator assigning a task they do not
  hold without a `reason` gets a 400; with one, the timeline names who did it
  and why. A delegated task can be completed only after it is handed back.
- [ ] **A completion with an undeclared variable** is a 400 that names it, and
  the task stays open.
- [ ] **A step over a list.** An instance that reaches a repeated step with an
  empty list moves past it; one with a misspelled list variable is refused,
  naming the step and the variable.
- [ ] **Retention, if set.** The sweep runs at boot and every ten minutes after;
  each table it removed rows from has a `Retention sweep` line with how many.
  An instance with an open incident or a recorded deviation is still there.
- [ ] **Offline work.** On a phone that queued approvals offline before the
  upgrade, the person who signs in sees them listed with **Discard**.
- [ ] **Alerts.** `make alerts-test` passes, and the 5xx rate does not rise after
  the upgrade.

### Strict tenant scope

0.5.0 still ships `METIS_FEATURE_STRICT_TENANT_SCOPE` off. If staging has not
yet been through [The rollout](strict-tenant-scope.md#the-rollout) for a full
release cycle, this is the cycle to do it. Production follows when staging has
been quiet for that cycle, not as part of this release.

### Rolling back

- [ ] **Reject every request that waits for a second administrator first.**
  The previous release cannot approve, reject or expire one ([Rolling back a
  release](runbooks.md#rolling-back-a-release)).
- [ ] **The previous image boots against the migrated schema** and serves:
  instances advance, tasks complete. The schema is forward-only, so this is
  what a rollback in production would be.
- [ ] **Then roll forward again**, and confirm nothing re-runs.
