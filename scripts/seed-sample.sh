#!/usr/bin/env bash
#
# Put something worth looking at into a development installation.
#
#   ./scripts/seed-sample.sh            against http://localhost:8273
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
# Matches scripts/dev.sh, which is the server this talks to. Not 8080: that is
# what every other project on the machine is already using.
readonly API_PORT="${API_PORT:-8273}"
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
  # /readyz, not /health. The embedded UI is a catch-all, so an unknown path
# answers 200 with the index page — a wait on one of those succeeds the moment
# the listener binds, before the database is reachable.
until curl -sS -m 2 -o /dev/null "http://localhost:${API_PORT}/readyz" 2>/dev/null; do
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

  # The wizard only answers whoever has the server's setup token. scripts/dev.sh
  # chooses it and starts this script with it; run on its own, pass the token
  # the server printed.
  [[ -n "${METIS_SETUP_TOKEN:-}" ]] || die "not set up yet: set METIS_SETUP_TOKEN to the setup token in the server's log"
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
    # Passed as arguments rather than interpolated: the script body is inside
    # single quotes, so a ${...} written here reaches python as literal text and
    # fails to parse. It did.
    "database_driver": "postgres",
    "db_host": sys.argv[5], "db_port": int(sys.argv[6]),
    "db_username": sys.argv[7], "db_password": sys.argv[8], "db_name": sys.argv[9],
    "encryption_key": sys.argv[3], "jwt_secret": sys.argv[4],
    "setup_token": sys.argv[10],
}))' "$ADMIN_USER" "$ADMIN_PASS" "$key" "$secret" \
       "${DB_HOST:-127.0.0.1}" "${DB_PORT:-5473}" \
       "${DB_USER:-metis}" "${DB_PASSWORD:-metis}" "${DB_NAME:-metis_dev}" \
       "$METIS_SETUP_TOKEN")"

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
  ADMIN_ID="$(printf '%s' "$response" | json "(d.get('user') or {}).get('id')")"
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


# --- the people the sample routes work to ----------------------------------

# The sample's user tasks are offered to candidate *groups* — line-managers,
# finance and compliance — and nothing created them. So every approval the seed
# started sat unclaimed, offered to a group with no members, and the inbox this
# script's own summary promised was empty on a fresh install. A demo whose
# headline screen is blank is worse than no demo.
#
# Each group also gets a person in it, so the sample shows what the product is
# for: work arriving in somebody's queue and being claimed by them. The admin
# joins all three, so signing in as the account this script prints shows
# everything without switching users.
readonly SAMPLE_PASS="sample-password"

ORG=""

ensure_people() {
  ORG="$(api GET /organizations | json "(d.get('organizations') or [{}])[0].get('id')")"
  [[ -n "$ORG" ]] || die "no organization to add groups to"

  # name:description:who-belongs-to-it
  local spec name description member
  for spec in \
    "line-managers:Approves expenses up to GBP 1,000:manager" \
    "finance:Approves anything larger:director" \
    "compliance:Reviews new suppliers:reviewer"
  do
    name="${spec%%:*}"
    description="$(cut -d: -f2 <<<"$spec")"
    member="${spec##*:}"

    create_group "$name" "$description"
    create_person "$member" "$name"
    join_group "$ADMIN_ID" "$name"
  done
  ok "created three groups and the people in them"
}

# Creating something that already exists is not an error here: this script is
# safe to run twice, and the second run should reach the same end state.
create_group() { # create_group <name> <description>
  api POST "/organizations/$ORG/groups" "$(python3 -c '
import json, sys
print(json.dumps({"group": {
    "name": sys.argv[1], "description": sys.argv[2], "roles": ["USER"],
}}))' "$1" "$2")" >/dev/null
}

group_id() { # group_id <name>
  api GET "/organizations/$ORG/groups" | python3 -c "
import json,sys
try: d = json.load(sys.stdin)
except Exception: print(''); raise SystemExit
name = sys.argv[1]
print(next((g['id'] for g in (d.get('groups') or []) if g.get('name') == name), ''))
" "$1"
}

create_person() { # create_person <username> <group-name>
  # organizations, not organization_id: the server reads a membership list, and
  # a user created without one is refused on every request they make.
  api POST /users "$(python3 -c '
import json, sys
print(json.dumps({
    "user": {
        "organizations": [{"id": sys.argv[1]}],
        "username": sys.argv[2],
        "full_name": sys.argv[2].title(),
        "display_name": sys.argv[2].title(),
        "email": sys.argv[2] + "@example.invalid",
        "roles": ["USER"],
    },
    "password": sys.argv[3],
}))' "$ORG" "$1" "$SAMPLE_PASS")" >/dev/null

  local uid
  uid="$(api GET "/organizations/$ORG/users" | python3 -c "
import json,sys
try: d = json.load(sys.stdin)
except Exception: print(''); raise SystemExit
name = sys.argv[1]
print(next((u['id'] for u in (d.get('users') or []) if u.get('username') == name), ''))
" "$1")"
  [[ -n "$uid" ]] && join_group "$uid" "$2"
}

join_group() { # join_group <user-id> <group-name>
  local gid
  gid="$(group_id "$2")"
  [[ -n "$gid" && -n "$1" ]] || return 0
  api POST "/groups/$gid/members/$1" >/dev/null
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

  # And one supplier check, which is the failure half of the sample.
  #
  # Its first step calls a web address that does not exist, so the instance
  # stops there and raises an incident — which is the point: the incident inbox
  # is where an operator spends their time, and a sample with nothing in it
  # cannot show that. It also means the compliance review after that step is
  # not reached until somebody resolves the incident, which is the walk-through.
  err="$(api POST /process.ProcessService/StartProcess "$(python3 -c '
import json, sys
print(json.dumps({
    "projectId": sys.argv[1],
    "definitionKey": "supplier-check",
    "variables": {
        "registration_id": "12345678",
        "supplierName": "Northwind Supplies Ltd",
    },
}))' "$PROJECT")" | json "d.get('error')")"
  if [[ -z "$err" ]]; then
    ok "started a supplier check, which fails on purpose"
  else
    warn "starting the supplier check: $err"
  fi
}

# --- main ------------------------------------------------------------------

main() {
  info "Seeding a sample into the installation on :${API_PORT}"
  wait_for_server
  ensure_configured
  sign_in
  ensure_people
  import_examples
  start_instances

  printf '\n'
  ok "Sample ready. Sign in as ${C_BOLD}${ADMIN_USER}${C_RESET} / ${C_BOLD}${ADMIN_PASS}${C_RESET}"
  cat <<EOF
${C_DIM}
  Processes    two: an expense approval, and a new supplier check
  Decisions    the tables those two consult
  Instances    four expense approvals, one of them finished
  My inbox     approvals waiting, under "Available to Claim"

  Sign in as any of these — all with the password ${SAMPLE_PASS}:

    manager    sees the approvals under GBP 1,000
    director   sees the ones above it
    reviewer   sees the supplier compliance review, once the incident
               below is resolved and the process reaches that step

  ${ADMIN_USER} is in all three groups, so it sees everything.

  The amount decides who approves: under 100 needs nobody, under 1000 a
  manager, anything more a director. docs/data-flow.md follows one through.

  One supplier check is running, and it fails on purpose: its first step calls
  https://api.example.com, which does not exist. It retries with backoff first,
  so give it a couple of minutes — the incident appears once the retries are
  spent, not straight away. Then: Instances → the failed one → "Show what
  failed", which explains the cause and offers a retry. That is the operator's
  half of the product, and it needs something broken to show.
${C_RESET}
EOF
}

main "$@"
