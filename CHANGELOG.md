# Changelog

Notable changes to Metis BPM. Entries describe what changed for somebody
running this, and name the behaviour that changed rather than the files.

This project has not cut a numbered release yet; everything below is on `main`
and ships in images tagged with `git describe`.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Security

- **`js:` gateway conditions are refused by default.** The JavaScript runtime
  behind them cannot be memory-bounded — a single native call ran for 37s
  against a 200ms budget, allocating throughout — so a default installation was
  one deployed definition away from memory exhaustion. FEEL has evaluated
  conditions natively since the expression layer landed.

  **Upgrading:** a definition still using `js:` will refuse to route, loudly,
  with an error naming the condition. `GET /api/v1/definitions/javascript-conditions`
  lists every affected definition. Set `GOBPM_FEATURE_JAVASCRIPT_CONDITIONS=true`
  to keep the old behaviour while migrating.

- **Idempotency keys are scoped to the caller.** The cache was keyed on method,
  path and the header value alone. Because clients choose their own keys and
  choose obvious ones, two tenants using the same value shared a cache entry:
  the second was served the first's response body and its own write never ran.
  Keys are now namespaced by tenant and user.

- **A malformed identifier is a 400, not a 500.** Forty-two endpoints returned
  the raw parse error for a bad UUID, which the transport mapped to a server
  error — spending the 0.1% 5xx error budget on client typos and paging whoever
  was on call for requests answered correctly.

### Fixed

- **A client retry landing on another replica no longer re-executes the write.**
  Idempotency records were held in the serving process, so a second replica
  found an empty cache and ran the work again — a duplicate business action,
  which is what the header exists to prevent. Records now live in
  `idempotency_records` (migration 8), claimed with a single conditional insert
  so exactly one replica executes and the rest replay its answer. Proven across
  SQLite, PostgreSQL and MySQL.

  This closes the only multi-replica gap that could corrupt data. Four remain
  and are degradations rather than corruption — see `docs/recovery.md` §2.1.

- **The PostgreSQL advisory lock never released.** Advisory locks are
  session-scoped and the locker held a connection *pool*, so the release ran on
  a session that held nothing, reported success, and left the lock held on an
  idle pooled connection. It now pins a session per lock.

- **A job could be claimed and then stranded.** The claim wrote a five-minute
  lease before taking the distributed lock, so a refused lock left a job marked
  running that nobody was running. The lock is now taken first, and released if
  the row race is then lost.

- **The bundle budget was calibrated on the wrong platform.** The same tree
  measures ~2.4 kB larger on Linux than macOS, against a budget with 1.5 kB of
  headroom — so the check passed on a laptop and blocked the release image.

### Added

- **Connector manifests can authenticate with OAuth client credentials.** The
  schema accepted `oauth2_client_credentials` and the runtime then refused it,
  so a manifest could be written, validated and installed and would fail on its
  first call. Tokens are fetched once and reused until shortly before expiry;
  concurrent callers share one fetch rather than each starting their own.

- **A production image.** Three stages (bun builds the UI, Go builds a static
  binary, distroless runs it as non-root on a read-only root filesystem), plus
  a compose file for evaluation. `make docker`, `make docker-run`.

- **`/healthz` reports the running build**, so "which version is this" no longer
  depends on finding a startup log line.

- **Backup and restore scripts** (`scripts/backup.sh`, `scripts/restore.sh`)
  implementing the procedures in `docs/recovery.md`. The backup refuses to run
  without `ENCRYPTION_KEY`; the restore refuses to start until the operator
  confirms the engine is stopped.

- **Connector contract tests** (`tests/connector/contract_test.go`) — the test
  pyramid's last missing tier. They pin what each connector puts on the wire and
  how it reads what comes back, including that a manifest's error rules decide
  before its success condition, and that the outbound egress policy refuses a
  private address by default.

- **SLO tests** (`tests/slo`). The roadmap's targets now fail a build when
  missed. Measured against PostgreSQL 17: reads p95 11.1ms (target 150ms),
  workflow actions p95 13.8ms (target 500ms), 0.000% 5xx (budget 0.1%),
  170,569 process starts/min (target 10,000/min).

- **Strict-tenant-scope integration coverage** (`tests/strictscope`), entering
  through the real HTTP chain and job worker, so the flag can be turned on in
  staging with evidence behind it. `make strict-scope`.

### Changed

- **The supported topology is a single engine replica**, now stated in
  `docs/recovery.md` §2.1 along with what breaks with two and why. Job claiming,
  migrations, correlation and outbound-call idempotency are already
  replica-safe; rate limiting, the idempotency cache, SSE delivery, connector
  breakers and the AMQP bridge are not.

- The designer's condition builder emits FEEL rather than JavaScript, and shows
  an expression too complex for its three fields instead of silently replacing
  it.
