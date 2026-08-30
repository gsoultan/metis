# Upgrading

## GoBPM is now Metis

The project, its module path and its repository are renamed. **An existing
installation keeps working without being reconfigured** — every old name is
still read — but each fallback is a migration aid with an expiry, not a second
supported spelling. This page is the list of things to change and when they stop
working.

### What must change now

Nothing, to keep running. One thing, to keep building:

```go
// go.mod, and every import
github.com/gsoultan/gobpm      →  github.com/gsoultan/metis
github.com/gsoultan/gobpm/sdk  →  github.com/gsoultan/metis/sdk
```

The Go client's package name changed with it — `gobpm.NewClient` is now
`metis.NewClient`. GitHub redirects the old repository URL, so `git remote` and
`go get` keep resolving, but the import path in your source has to be edited.

### What still works, and for how long

| Was | Is | Until |
| :-- | :-- | :-- |
| `GOBPM_*` environment variables | `METIS_*` | Read, with a warning naming the variable. Removed in a future release. |
| `gobpm.db` (SQLite default) | `metis.db` | Opened if present. Kept indefinitely; renaming the file is optional. |
| `gobpm-*-storage` (browser) | `metis-*-storage` | Migrated on first load, automatically and once. |

**Environment variables.** Nothing is required of you. The server reads the old
name, uses it, and logs once per variable saying what to rename it to:

```
This setting is read under its old name. GoBPM is now Metis; the GOBPM_
spelling still works and will be removed in a future release.
  using=GOBPM_HTTP_ADDRESS  rename_to=METIS_HTTP_ADDRESS
```

`ENCRYPTION_KEY`, `JWT_SECRET` and `DATABASE_URL` were never prefixed and are
unchanged.

The reason for the fallback rather than a clean break: several of these change
behaviour when they go missing, and they do it quietly. An installation with
`GOBPM_FEATURE_JAVASCRIPT_CONDITIONS=true` would have come back up with the flag
at its default, and every gateway routing on a `js:` condition would have
stopped — with nothing in the log connecting that to an upgrade.

**The SQLite file.** A fresh install creates `metis.db`. An existing `gobpm.db`
is opened as it is, and the startup log says so. Rename it when convenient, or
point `DATABASE_URL` at it explicitly. If both exist, `metis.db` wins.

**Browser storage.** Nobody is signed out. The four `gobpm-`prefixed keys are
copied to their `metis-` names on first load and the old ones removed. Selected
project, theme and sidebar state come across with the session.

### What changed with no fallback

- The container entrypoint and binary are `metis`, not `gobpm`. A deployment
  that names the binary in a command override needs editing.
- `docker-compose.yml` uses `metis` for its evaluation database's user, password
  and database name, and a `metis-postgres` volume. **An existing evaluation
  stack starts empty** — it is throwaway by design, but if you were relying on
  it, rename the volume or point the compose file back at the old credentials.

- **Metrics and traces are renamed, and nothing forwards the old names.** The
  Prometheus metrics are `metis_http_requests_total`,
  `metis_http_request_duration_seconds` and `metis_http_requests_in_flight`; the
  OpenTelemetry service is `metis`, its span is `metis.http`, and its attributes
  are `metis.instance.id`, `metis.node.id`, `metis.definition.id`,
  `metis.connector.key` and `metis.attempt`.

  **A dashboard or alert rule written against the `gobpm_` names goes blank
  rather than failing.** A blank panel is easy to notice; a paging alert whose
  query matches nothing simply stops firing, which is not. Grep your alerting
  rules for `gobpm_` before upgrading, not after.

- Webhook deliveries send `User-Agent: Metis-Webhook/1.0`. Only relevant if a
  receiver matches on it.

### What deliberately did not change

The `Idempotency-Key` Metis sends on outbound service calls still begins with
`gobpm-`, and that is not an oversight.

The key is derived fresh on every retry rather than read back from storage, so
it is the only thing that identifies a retry to the receiving system as the same
request it already saw. Renaming it would mean a job that happened to be
between retries during the upgrade arrives looking new — and a service task
exists to have an effect out in the world, so "new" means charging the card a
second time. `TestTheServiceCallKeyIsFrozenAcrossTheRename` fails if anyone
finishes the job.

### Checking

The server tells you what it is reading. Start it and look for any line
mentioning an old name; when there are none, the fallbacks are doing nothing and
you can stop thinking about this page.
