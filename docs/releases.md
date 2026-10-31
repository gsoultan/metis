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

## The steps

Each step runs in one runtime: the one whose port the request arrives on.

| Step | Where | What it does |
|---|---|---|
| **Cut** | development | Records the *live* versions of the processes you name as a release. It adds the decisions they consult and the processes they call. Nothing that runs changes. |
| **Import** | production | Reads the release from the environment it was cut in, checks it, and stores its models as new versions, **not live**. |
| **Go live** | production | Makes every model of the release live in one transaction: decisions first, then processes. |
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

## API

All under `/api/v1/projects/{id}`. Every route is a model change: a designer
may use it where nothing is protected, and a releaser or an administrator in
a protected runtime.

| Method | Path | Body |
|---|---|---|
| `GET` | `/releases` | |
| `POST` | `/releases` | `{"name": "2026.11.1", "processes": ["claims"], "decisions": []}` |
| `GET` | `/releases/{name}` | |
| `POST` | `/releases/import` | `{"from_environment": "<environment id, or empty for the main runtime>", "name": "2026.11.1"}` |
| `POST` | `/releases/{name}/go-live` | |
| `POST` | `/releases/rollback` | |
