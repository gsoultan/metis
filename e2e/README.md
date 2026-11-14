# End-to-end tests

Playwright drives the embedded UI in Chromium against a real `metis` server and a
seeded PostgreSQL database. CI runs it in the `e2e` job of
`security_reliability_ci.yml`.

## Running

```bash
# Build the UI once (see CLAUDE.md), then:
DATABASE_URL="postgres://metis:metis@localhost:5432/metis_e2e?sslmode=disable" make e2e
```

`scripts/e2e.sh` does these steps:

1. Builds the server.
2. Starts it on `:8373` with throwaway secrets and a high rate limit.
3. Runs the setup wizard and loads the sample through `scripts/seed-sample.sh`.
4. Runs the suite.

The server log is copied to `test-results/server.log`.

The run writes to the database. For that reason, the script refuses any database
whose name does not contain `e2e`, unless `E2E_ANY_DATABASE=1` is set.

- `make e2e-serve` starts and seeds the server, then leaves it running. Run specs
  against it from here:
  `E2E_ADMIN_PASSWORD=e2e-admin-password bunx playwright test tests/tasks.spec.ts`
- `E2E_BASE_URL=https://… ./scripts/e2e.sh` runs the suite against a server that
  is already running and seeded.
- `bun run report` opens the last HTML report.

## Layout

| Path | What it holds |
| :-- | :-- |
| `tests/auth.setup.ts` | Signs the admin in once and saves the session that the other specs reuse |
| `tests/*.spec.ts` | One spec per area of the product |
| `support/test.ts` | The `test` fixture. It fails a test on any uncaught page error, and provides `api`, `uniqueName` and `knownBug` |
| `support/api.ts` | Deploys and starts processes through the API, so a UI test starts from a known state |
| `support/designer.ts` | Locators for the process designer |

## Conventions

- Find elements by role and accessible name, as a user would, not by CSS class.
- Give each test the data it needs, and name that data with `uniqueName()`. Do
  not rely on the order the tests run in.
- **Known bugs.** A bug that is open is written as a test of the *intended*
  behaviour, with `knownBug('<finding>')` at the top. Playwright expects that test
  to fail, and shows it with a ✘ while the run still passes. When the fix lands,
  the test passes and the run fails. That is the signal to delete the marker.
- Signing out revokes every session of the account. So tests that sign out use
  the sample's `reviewer` and never the admin, whose session the other specs
  share.
