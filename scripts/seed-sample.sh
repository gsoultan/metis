#!/usr/bin/env bash
#
# Put something worth looking at into a development installation.
#
#   ./scripts/seed-sample.sh            against http://localhost:8080
#   API_PORT=9000 ./scripts/seed-sample.sh
#
# Run the setup wizard if it has not been run, import the two worked examples
# from docs/data-flow.md, and start a few instances so the screens have
# something in them: processes to open, a decision table to read, instances
# running, and tasks waiting in the inbox.
#
# An empty installation is a poor first impression — every list says "nothing
# here yet", which tells you the thing works but not what it does. This is the
# same data the documentation walks through, so the two agree.
#
# Safe to run twice: importing a definition or decision that already exists
# files a new version rather than failing, and starting more instances is only
# more instances.

set -Eeuo pipefail

readonly ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly API_PORT="${API_PORT:-8080}"
readonly API="http://localhost:${API_PORT}/api/v1"
readonly EXAMPLES="$ROOT/docs/examples"

# Development credentials. Stated out loud because there is no default account:
# whatever is typed at setup is the only way in, and a sample installation
# nobody can log into is not a sample.
readonly ADMIN_USER="${SAMPLE_ADMIN_USER:-admin}"
readonly ADMIN_PASS="${SAMPLE_ADMIN_PASS:-admin}"

C_RESET=$'\033[0m'; C_DIM=$'\033[2m'; C_BOLD=$'\033[1m'
C_GREEN=$'\033[32m'; C_YELLOW=$'\033[33m'; C_RED=$'\033[31m'
[[ -t 1 ]] || { C_RESET=""; C_DIM=""; C_BOLD=""; C_GREEN=""; C_YELLOW=""; C_RED=""; }

info() { printf '%s==>%s %s\n' "$C_BOLD" "$C_RESET" "$*"; }
ok()   { printf '  %sok%s   %s\n' "$C_GREEN" "$C_RESET" "$*"; }
warn() { printf '  %swarn%s %s\n' "$C_YELLOW" "$C_RESET" "$*"; }
die()  { printf '%serror%s %s\n' "$C_RED" "$C_RESET" "$*" >&2; exit 1; }

command -v python3 >/dev/null || die "python3 is required (used to build and read JSON)"
command -v curl >/dev/null    || die "curl is required"

api() { # api <method> <path> [json-body]
  local method="$1" path="$2" body="${3:-}"
  if [[ -n "$body" ]]; then
    curl -sS -m 60 -X "$method" "$API$path" \
      -H 'Content-Type: application/json' \
      ${TOKEN:+-H "Authorization: Bearer $TOKEN"} \
      --data-binary "$body"
  else
    curl -sS -m 60 -X "$method" "$API$path" \
      ${TOKEN:+-H "Authorization: Bearer $TOKEN"}
  fi
}

json() { # json <expression over the piped object, as `d`>
  python3 -c "
import json,sys
try: d = json.load(sys.stdin)
except Exception: print(''); raise SystemExit
print($1 if ($1) is not None else '')
"
}

TOKEN=""

wait_for_server() {
  local waited=0
  until curl -sS -m 2 -o /dev/null "http://localhost:${API_PORT}/health" 2>/dev/null; do
    (( waited += 1 ))
    if (( waited > 60 )); then
      die "the server on :${API_PORT} did not answer within 60s"
    fi
    sleep 1
  done
}

# --- setup -----------------------------------------------------------------

# ALREADY_CONFIGURED records whether this installation predates the seed, so a
# failure to sign in can be explained rather than just reported.
ALREADY_CONFIGURED=0

ensure_configured() {
  local configured
  configured="$(api GET /setup/status | json "d.get('status',{}).get('is_initialized')")"

  if [[ "$configured" == "True" ]]; then
    ok "already set up"
    ALREADY_CONFIGURED=1
    return 0
  fi

  info "Running the setup wizard"
  local key secret payload
  key="$(python3 -c 'import secrets,base64; print(base64.b64encode(secrets.token_bytes(32)).decode())')"
  secret="$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
  payload="$(python3 -c '
import json, sys
print(json.dumps({
    "admin_username": sys.argv[1], "admin_password": sys.argv[2],
    "admin_full_name": "Development Admin", "admin_public_name": "Admin",
    "admin_email": "admin@example.invalid",
    "organization_name": "Example Co", "project_name": "Sample Project",
    "database_driver": "sqlite", "db_name": "metis.db",
    "encryption_key": sys.argv[3], "jwt_secret": sys.argv[4],
}))' "$ADMIN_USER" "$ADMIN_PASS" "$key" "$secret")"

  local err
  err="$(api POST /setup "$payload" | json "d.get('error')")"
  [[ -z "$err" ]] || die "setup failed: $err"
  ok "set up as ${C_BOLD}${ADMIN_USER}${C_RESET} / ${C_BOLD}${ADMIN_PASS}${C_RESET}"
}

sign_in() {
  local response
  # Single-quoted: bash expands {a, b} inside double quotes, which turns a
  # Python dict literal into two mangled arguments.
  response="$(api POST /login "$(python3 -c '
import json, sys
print(json.dumps({"username": sys.argv[1], "password": sys.argv[2]}))
' "$ADMIN_USER" "$ADMIN_PASS")")"
  TOKEN="$(printf '%s' "$response" | json "d.get('token')")"
  if [[ -z "$TOKEN" ]]; then
    # An installation that already exists has whatever password was typed at
    # its setup, which is usually not this one. That is not a failure of the
    # servers — they are running — so say what happened and stop, rather than
    # reporting an error for something nobody asked to change.
    if (( ALREADY_CONFIGURED )); then
      warn "this installation is already set up, and \"$ADMIN_USER\" did not sign in with the sample password"
      warn "to seed it anyway:  SAMPLE_ADMIN_PASS='your-password' ./scripts/seed-sample.sh"
      warn "to start over:      ./scripts/dev.sh --reset --sample"
      exit 0
    fi
    die "could not sign in as $ADMIN_USER after setting it up"
  fi
}

# --- the examples ----------------------------------------------------------

import_examples() {
  PROJECT="$(api GET /projects | json "(d.get('projects') or [{}])[0].get('id')")"
  [[ -n "$PROJECT" ]] || die "no project to import into"

  local example wrapped
  for example in expense-approval supplier-check; do
    for kind in decision definition; do
      wrapped="$(python3 -c '
import json, sys
body = json.load(open(sys.argv[1]))
body["project"] = {"id": sys.argv[2]}     # a reference is an object, not an id
json.dump({sys.argv[3]: body}, sys.stdout)
' "$EXAMPLES/$example.$kind.json" "$PROJECT" "$kind")"

      local err
      err="$(api POST "/${kind}s" "$wrapped" | json "d.get('error') or d.get('err')")"
      [[ -z "$err" ]] || die "importing $example.$kind: $err"
    done
    ok "imported $example"
  done
}

# --- something to look at --------------------------------------------------

start_instances() {
  local amount err started=0
  # One of each path through the expense process, so the inbox has work in it
  # and the instance list shows a finished one as well as waiting ones.
  for amount in 2400 500 40 1750; do
    err="$(api POST /process.ProcessService/StartProcess "$(python3 -c '
import json, sys
print(json.dumps({
    "projectId": sys.argv[1],
    "definitionKey": "expense-approval",
    "variables": {
        "amount": float(sys.argv[2]),
        "currency": "GBP",
        "description": sys.argv[3],
        "submittedBy": "alice",
    },
}))' "$PROJECT" "$amount" "Expense of GBP $amount")" | json "d.get('error')")"
    [[ -z "$err" ]] && (( started += 1 )) || warn "starting an instance: $err"
  done
  ok "started $started expense approvals"
}

# --- main ------------------------------------------------------------------

main() {
  info "Seeding a sample into the installation on :${API_PORT}"
  wait_for_server
  ensure_configured
  sign_in
  import_examples
  start_instances

  printf '\n'
  ok "Sample ready. Sign in as ${C_BOLD}${ADMIN_USER}${C_RESET} / ${C_BOLD}${ADMIN_PASS}${C_RESET}"
  cat <<EOF
${C_DIM}
  Processes    two: an expense approval, and a new supplier check
  Decisions    the tables those two consult
  Instances    four expense approvals, one of them finished
  My work      approvals waiting for a manager and for a director

  The amount decides who approves: under 100 needs nobody, under 1000 a
  manager, anything more a director. docs/data-flow.md follows one through.

  The supplier check calls https://api.example.com, which does not exist, so
  it raises an incident — deliberately, to show what a failure looks like.
${C_RESET}
EOF
}

main "$@"
