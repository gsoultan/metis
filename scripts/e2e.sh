#!/usr/bin/env bash
#
# Run the Playwright end-to-end suite (e2e/) against a real Metis.
#
#   DATABASE_URL=postgres://metis:metis@127.0.0.1:5432/metis_e2e?sslmode=disable \
#     ./scripts/e2e.sh                     the whole suite
#   ./scripts/e2e.sh tests/designer.spec.ts --headed
#                                          any arguments go to `playwright test`
#
#   ./scripts/e2e.sh --serve               start and seed the server, then leave it
#                                          running for writing tests against:
#   E2E_BASE_URL=http://localhost:8373 ./scripts/e2e.sh
#                                          use a server that is already running
#                                          and already seeded; nothing is started
#
# Builds the binary (and the UI, if ui/dist is missing), starts it on the
# database DATABASE_URL names, sets it up and fills it with the sample from
# scripts/seed-sample.sh, then runs the suite. The server is stopped on the
# way out, and its log is kept next to the Playwright report.
#
# The database must be empty, and must be one nobody minds losing: the suite
# completes tasks, deploys versions and adds people. So the script refuses a
# database whose name does not say "e2e", unless E2E_ANY_DATABASE=1.

set -Eeuo pipefail

readonly ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
# Not the development ports (8273), so the suite can run while scripts/dev.sh does.
readonly E2E_PORT="${E2E_PORT:-8373}"
readonly E2E_GRPC_PORT="${E2E_GRPC_PORT:-8374}"
# Long enough for the server's own minimum, so no hash needs replacing in the
# database the way scripts/seed-sample.sh does for "admin".
readonly ADMIN_PASSWORD="${E2E_ADMIN_PASSWORD:-e2e-admin-password}"
readonly RESULTS="$ROOT/e2e/test-results"

info() { printf '==> %s\n' "$*"; }
die()  { printf 'error: %s\n' "$*" >&2; exit 1; }

run_suite() {
  cd "$ROOT/e2e"
  [[ -d node_modules ]] || bun install --frozen-lockfile
  E2E_BASE_URL="$1" E2E_ADMIN_PASSWORD="$ADMIN_PASSWORD" bunx playwright test "${@:2}"
}

if [[ -n "${E2E_BASE_URL:-}" ]]; then
  info "Using the server at $E2E_BASE_URL"
  run_suite "$E2E_BASE_URL" "$@"
  exit
fi

[[ -n "${DATABASE_URL:-}" ]] || die "set DATABASE_URL to an empty PostgreSQL database for the suite to use"

# The seed posts the database's parts to the setup wizard, so split the URL once.
# shellcheck disable=SC2046
eval "$(python3 - "$DATABASE_URL" <<'PY'
import shlex, sys
from urllib.parse import urlparse, unquote
u = urlparse(sys.argv[1])
for key, value in {
    "DB_HOST": u.hostname or "127.0.0.1", "DB_PORT": str(u.port or 5432),
    "DB_USER": unquote(u.username or ""), "DB_PASSWORD": unquote(u.password or ""),
    "DB_NAME": u.path.lstrip("/"),
}.items():
    print(f"export {key}={shlex.quote(value)}")
PY
)"
if [[ "$DB_NAME" != *e2e* && "${E2E_ANY_DATABASE:-0}" != 1 ]]; then
  die "the suite changes the data in \"$DB_NAME\"; use a database whose name contains e2e, or set E2E_ANY_DATABASE=1"
fi

WORK="$(mktemp -d)"
SERVER_PID=""
stop_server() {
  local status=$?
  if [[ -n "$SERVER_PID" ]]; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  mkdir -p "$RESULTS"
  cp "$WORK/server.log" "$RESULTS/server.log" 2>/dev/null || true
  if (( status != 0 )) && [[ -f "$WORK/server.log" ]]; then
    printf '\n--- the last of the server log (all of it is in e2e/test-results/server.log) ---\n'
    tail -n 40 "$WORK/server.log"
  fi
  rm -rf "$WORK"
}
trap stop_server EXIT

if [[ ! -f "$ROOT/ui/dist/index.html" ]]; then
  info "Building the UI (ui/embed.go embeds ui/dist)"
  (cd "$ROOT/ui" && bun install --frozen-lockfile && bun run build)
fi
info "Building the server"
(cd "$ROOT" && go build -o "$WORK/metis" ./cmd/metis)

random() { python3 -c 'import secrets; print(secrets.token_hex(32))'; }

# Chosen here and given to both: the server answers the setup wizard only for
# whoever holds it, and the seed is what runs the wizard.
export METIS_SETUP_TOKEN="$(random)"

info "Starting it on :$E2E_PORT"
# Run from the work directory: the setup wizard writes config.yaml where the
# server runs, and the checkout is not the place for one.
(
  cd "$WORK"
  export ENCRYPTION_KEY="$(random)" JWT_SECRET="$(random)"
  export METIS_HTTP_ADDRESS=":$E2E_PORT" METIS_GRPC_ADDRESS=":$E2E_GRPC_PORT"
  # One browser is one address, and a page load is a few dozen calls: at the
  # default of 240 a minute the suite is refused part way through. The limit
  # has its own tests; this suite is about the pages.
  export METIS_HTTP_RATE_LIMIT=100000
  # As in development (scripts/dev.sh), so a step can call a mock on localhost.
  export METIS_HTTP_ALLOW_PRIVATE_NETWORKS=true
  exec ./metis
) >"$WORK/server.log" 2>&1 &
SERVER_PID=$!

info "Setting it up and seeding the sample"
API_PORT="$E2E_PORT" SAMPLE_ADMIN_PASS="$ADMIN_PASSWORD" "$ROOT/scripts/seed-sample.sh"

if [[ "${1:-}" == "--serve" ]]; then
  info "Serving on http://localhost:$E2E_PORT (admin / $ADMIN_PASSWORD). Ctrl-C stops it."
  wait "$SERVER_PID"
  exit
fi

info "Running the suite"
run_suite "http://localhost:$E2E_PORT" "$@"
