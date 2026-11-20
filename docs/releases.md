# Releases

A release moves a project's models from where they were designed and tested
to where they run, unchanged. It is the way to put a change into production
when production is [protected](upgrading.md#protected-environments-migration-52).

```
development ──cut──▶ release ──import──▶ production (staged) ──go live──▶ live
                                 └──▶ staging (optional)
```

Staging is optional. Import into staging first when you want a rehearsal;
import straight into production when you do not.

## In the interface

**Operate → Releases** lists the project's releases in the environment the
page is served from, with **Cut a release**, **Import a release**,
**Approve**, **Go live** and **Roll back**. A banner across every page says
which environment it is: red in a protected one.

## The steps

Each step runs in one runtime: the one whose port the request arrives on.

| Step | Where | What it does |
|---|---|---|
| **Cut** | development | Records the *live* versions of the processes you name as a release. It adds the decisions they consult and the processes they call, then runs their checks there (see below). Nothing that runs changes. |
| **Import** | production | Reads the release from the environment it was cut in, checks it, and stores its models as new versions, **not live**. |
| **Approve** | production, if protected | Someone other than who cut it and who imported it agrees it may go live. |
| **Go live** | production | Checks the release, then makes every model live in one transaction: decisions first, then processes. |
| **Roll back** | production | Makes the release that was live before the live one live again. |

Running instances are never moved. They finish on the version they started on.

## What cut checks

Before a release is recorded, every decision it carries runs its own tests and
every process its saved simulation cases, in the runtime it is cut from. Any
failure refuses the cut and lists what failed. What passed is recorded in the
release as `checked`, inside its hash, so an installation that trusts the
release's signature also knows it was checked where it was made. That is what a
production with no staging before it relies on. The release API shows it as
`checked`: `at`, `decision_tests` and `simulation_cases`.

## What import checks

- **The content is what was cut.** A release carries a SHA-256 of its content.
  A release changed since it was cut is refused.
- **Every connection exists here.** Steps in a release name their connection
  by name, such as `payments-api`, because ids differ in every environment.
  An administrator creates a connection with that name in each environment,
  with that environment's credentials. A name with no connection is refused,
  and every missing name is listed.
- **Every model is valid.**

Importing the same release twice returns the first import. A different
release with the same name is refused.

## What go live checks

Go live rehearses the release first: every model is put into force in a
transaction that is then rolled back, and the checks run against that, so a
process is checked with the decisions it will consult, new ones included.

- **Every connection still exists here.**
- **Every decision passes its own tests**, the examples stored with the table.
- **Every process ends its saved simulation cases as they expect.** The cases
  are the ones saved in the designer for that process. A simulation calls no
  connector and sends nothing.

Any failure refuses the go-live and lists what failed. Nothing of the release
goes live.

## A second person in production

In a [protected](upgrading.md#protected-environments-migration-52)
environment, a release goes live only once it has been approved by someone
who neither cut it nor imported it. The approver needs the same role as any
model change there: Releaser or Administrator. A rollback needs no new
approval: the release it returns to was approved when it went live.

## A key new to production

The first version of a process or decision key is live the moment it is
stored. So a key that production does not have yet is only checked at
import, and is stored when the release goes live.

## What a release cannot carry

- **A pinned version.** A step that consults a decision or calls a process at
  a fixed version number is refused at cut. Version numbers differ between
  environments, so the number would name another model there. Let the step
  use the live version.
- **Simulation cases.** They are kept with the project, not with a runtime,
  so every environment of an installation already reads the same ones.

## Rolling back

Rolling back puts the previous release's versions back in force. A model the
live release brought that the previous one did not carry stays as it is,
because there is no earlier version of it to return to. Rolling back again
walks further back.

## Between installations

When production is an installation of its own, with its own address and
database, the installations are **connected**, and a release travels between
them signed.

### Connecting two servers

Each server says what it is for with `METIS_SERVER_PURPOSE`: `development`
(the default), `staging` or `production`. Releases go forwards only:
production takes them from staging or development, staging from development.
`METIS_SERVER_NAME` is what the others call it; unset, it is called by its
purpose.

1. On the server releases come from (development, or staging for a
   production after it), a platform administrator makes a **one-time code**
   (`POST /api/v1/server-connections/codes`). It is good once, for 15 minutes.
2. On the server releases go to, a platform administrator enters that
   server's address and the code (`POST /api/v1/server-connections`). This
   server calls the other one with the code; the other answers with its name,
   purpose and public key. Both now list the connection with the same **four
   words**: the two people check they match. If they do not, something
   between the servers changed a key, and the connection is removed.
3. On the server releases go to, a **second** platform administrator accepts
   the connection (`POST /api/v1/server-connections/{id}/accept`). The one
   who connected it cannot. Only then are the other server's releases and
   live notes trusted.

Servers talk over https. Plain http is allowed only where
`METIS_HTTP_ALLOW_PRIVATE_NETWORKS` is set, which is a developer's machine as
a rule. A private address such as `https://10.20.0.15` is allowed: an
administrator chose it, not a process definition. The server releases go to
is the one that calls, so a production in a private network needs no address
anyone outside can reach.

### Keys

Each server signs the releases it exports, and its live notes, with an
Ed25519 key it makes the first time one is needed and keeps sealed with
`ENCRYPTION_KEY`. It trusts the keys of the servers it accepted. An
acceptance is sealed too: a row written into the database without
`ENCRYPTION_KEY` is accepted in name only, and its key is not trusted.

An operator can still set them in the environment:

- `METIS_RELEASE_SIGNING_KEY`, an Ed25519 seed (32 random bytes, base64:
  `openssl rand -base64 32`), replaces the key the server makes.
- `METIS_RELEASE_TRUSTED_KEYS`, public keys separated by commas, is then the
  whole of what the server trusts, and no server can be connected or accepted
  from the pages.

### A release file

With no network path between them at all, export the release
(`GET /releases/{name}/export`) and import the file on the other server
(`POST /releases/import-file`). The file carries the release, its hash, the
public key and a signature over the hash. It is checked as an import between
environments is, and then the signature: a file that was changed, or signed
by a key the server does not trust, is refused.

Go live, approval and rollback are then the same as between environments.
The checks at go live run that server's own saved simulation cases.

A program that embeds the engine can keep the keys itself instead
(`engine.WithReleaseKeys`); it is then answerable for who may change which
keys are trusted, because an administrator who could change them could make
production trust a release nobody tested.

## When production has a staging before it

A production that takes releases from a staging can require that a release
went live in staging before it goes live there. Staging gives a **live note**
for a release that has gone live there: the release's name and hash, when it
went live, signed with staging's release key. Production keeps it with the
staged release:

1. On staging, after the release went live, read its note
   (`GET /releases/{name}/live-note`).
2. On production, attach it to the staged release
   (`POST /releases/{name}/live-note`). It is refused unless it is for exactly
   this release (the same hash), and signed by a key production trusts.
3. Go live. Production checks the note again, so a note from a key no longer
   trusted does not count.

A note is required once a production has accepted a staging before it. A
production connected only to development needs none, and relies on the
checks the release passed where it was cut. A program that embeds the engine
can decide otherwise with `engine.WithReleasePolicy`.

## API

All under `/api/v1/projects/{id}`. Every route is a model change: a designer
may use it where nothing is protected, and a releaser or an administrator in
a protected runtime.

| Method | Path | Body |
|---|---|---|
| `GET` | `/api/v1/environments/current?project_id=` | Which runtime this port serves, and the project's environments by name. Any member of the project may read it. |
| `GET` | `/releases` | |
| `POST` | `/releases` | `{"name": "2026.11.1", "processes": ["claims"], "decisions": []}` |
| `GET` | `/releases/{name}` | |
| `POST` | `/releases/import` | `{"from_environment": "<environment id, or empty for the main runtime>", "name": "2026.11.1"}` |
| `GET` | `/releases/{name}/export` | Answers `{"file": {...}}`. |
| `POST` | `/releases/import-file` | `{"file": {...}}`, at most 16 MiB |
| `GET` | `/releases/{name}/live-note` | Answers `{"note": {...}}`, signed, for a release that has gone live here. |
| `POST` | `/releases/{name}/live-note` | `{"note": {...}}`: another environment's note, attached to a staged release. |
| `POST` | `/releases/{name}/approve` | |
| `POST` | `/releases/{name}/go-live` | |
| `POST` | `/releases/rollback` | |

Connecting servers, under `/api/v1/server-connections`, a platform
administrator's:

| Method | Path | Body |
|---|---|---|
| `GET` | `/server-connections` | This server (`name`, `purpose`, `key`, `url`, `trust_locked`) and every connection. |
| `POST` | `/server-connections/codes` | Answers `{"code": {"code": "…", "expires_at": "…"}}`, shown once. |
| `POST` | `/server-connections` | `{"url": "https://dev.metis.example.com", "code": "…"}` |
| `POST` | `/server-connections/{id}/accept` | By an administrator other than the one who connected it. |
| `DELETE` | `/server-connections/{id}` | |
| `POST` | `/server-connections/handshake` | Public: another server, with a code made here. |
