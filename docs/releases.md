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
| **Cut** | development | Records the *live* versions of the processes you name as a release. It adds the decisions they consult and the processes they call. Nothing that runs changes. |
| **Import** | production | Reads the release from the environment it was cut in, checks it, and stores its models as new versions, **not live**. |
| **Approve** | production, if protected | Someone other than who cut it and who imported it agrees it may go live. |
| **Go live** | production | Checks the release, then makes every model live in one transaction: decisions first, then processes. |
| **Roll back** | production | Makes the release that was live before the live one live again. |

Running instances are never moved. They finish on the version they started on.

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

When production is an installation of its own, a release travels as a file.

1. On the installation the release was cut in, set `METIS_RELEASE_SIGNING_KEY`
   to an Ed25519 seed: 32 random bytes, base64
   (`openssl rand -base64 32`). Export the release: its file carries the
   release, its hash, the public key and a signature over the hash.
2. On production, set `METIS_RELEASE_TRUSTED_KEYS` to that installation's
   public key, the `key` field of any file it exports. Several keys are
   separated by commas.
3. Import the file into the production project. It is checked as an import
   between environments is, and then the signature: a file that was changed,
   or signed by a key production does not trust, is refused.

Go live, approval and rollback are then the same as between environments.
The checks at go live run production's own saved simulation cases.

Both settings are read from the environment only, never from the Settings
page: an administrator who could change them could make production trust a
release nobody tested.

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
| `POST` | `/releases/{name}/approve` | |
| `POST` | `/releases/{name}/go-live` | |
| `POST` | `/releases/rollback` | |
