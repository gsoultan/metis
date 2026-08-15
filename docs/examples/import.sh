#!/usr/bin/env bash
#
# Import the worked examples from docs/data-flow.md into a running server.
#
#   ./docs/examples/import.sh                     # localhost:8080, admin
#   ./docs/examples/import.sh -u alice -p secret  # different account
#   BASE_URL=http://host:9000 ./docs/examples/import.sh
#
# Both files per example are imported: the decision table first, because the
# process references it by key and will raise an incident at the business rule
# task if it is missing.
#
# The wrapping this does is the reason the script exists. The API takes
# {"decision": {...}} and {"definition": {...}}, and the request types hold
# those by value, so a bare object decodes to an empty one and is accepted —
# you get 200 and an id back, having stored nothing you sent. Posting these
# files directly is the mistake this saves you from.

set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
USERNAME="admin"
PASSWORD="admin"
PROJECT=""

while getopts "u:p:P:h" opt; do
  case "$opt" in
    u) USERNAME="$OPTARG" ;;
    p) PASSWORD="$OPTARG" ;;
    P) PROJECT="$OPTARG" ;;
    h) sed -n '2,18p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) exit 2 ;;
  esac
done

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
API="$BASE_URL/api/v1"

die() { printf '\033[31m%s\033[0m\n' "$*" >&2; exit 1; }
say() { printf '  %s\n' "$*"; }

command -v python3 >/dev/null || die "python3 is required (used to build the JSON payloads)"

# --- authenticate ------------------------------------------------------------
# The password is whatever was set in the setup wizard. There is no built-in
# default account, so admin/admin only works if that is what you typed.
login_response="$(curl -sS -m 30 -X POST "$API/login" \
  -H 'Content-Type: application/json' \
  -d "$(python3 -c 'import json,sys; print(json.dumps({"username":sys.argv[1],"password":sys.argv[2]}))' "$USERNAME" "$PASSWORD")")"

TOKEN="$(printf '%s' "$login_response" | python3 -c 'import json,sys
try: print(json.load(sys.stdin).get("token",""))
except Exception: print("")')"

[ -n "$TOKEN" ] || die "could not log in as $USERNAME: $login_response"
say "signed in as $USERNAME"

# --- pick a project ----------------------------------------------------------
if [ -z "$PROJECT" ]; then
  PROJECT="$(curl -sS -m 30 "$API/projects" -H "Authorization: Bearer $TOKEN" | python3 -c 'import json,sys
ps = (json.load(sys.stdin).get("projects") or [])
print(ps[0]["id"] if ps else "")')"
  [ -n "$PROJECT" ] || die "no projects exist yet — create one first, or pass -P <project-id>"
fi
say "project $PROJECT"

# --- import ------------------------------------------------------------------
post() { # post <path> <json-file>
  curl -sS -m 60 -X POST "$API/$1" \
    -H "Authorization: Bearer $TOKEN" \
    -H 'Content-Type: application/json' \
    --data-binary "@$2"
}

wrap() { # wrap <envelope-key> <file> -> payload on stdout
  python3 -c 'import json,sys
key, path, project = sys.argv[1], sys.argv[2], sys.argv[3]
body = json.load(open(path))
body["project"] = {"id": project}          # a relational reference is an object, not an id
json.dump({key: body}, sys.stdout)' "$1" "$2" "$PROJECT"
}

report() { # report <what> <response>
  local err
  err="$(printf '%s' "$2" | python3 -c 'import json,sys
try: d = json.load(sys.stdin)
except Exception: print("unreadable response"); raise SystemExit
print(d.get("error") or d.get("err") or "")' 2>/dev/null || true)"
  if [ -n "$err" ]; then
    printf '  \033[31m%-34s %s\033[0m\n' "$1" "$err"
    return 1
  fi
  printf '  \033[32m%-34s ok\033[0m\n' "$1"
}

failed=0
for example in expense-approval supplier-check; do
  echo
  echo "$example"

  tmp="$(mktemp)"; trap 'rm -f "$tmp"' EXIT
  wrap decision "$HERE/$example.decision.json" > "$tmp"
  report "decision table" "$(post decisions "$tmp")" || failed=1

  wrap definition "$HERE/$example.definition.json" > "$tmp"
  report "process definition" "$(post definitions "$tmp")" || failed=1
  rm -f "$tmp"
done

echo
[ "$failed" -eq 0 ] || die "some imports failed — nothing was rolled back, so fix and re-run"

cat <<EOF
Imported. Start the first one with:

  curl -sS -X POST $API/process.ProcessService/StartProcess \\
    -H "Authorization: Bearer \$TOKEN" -H 'Content-Type: application/json' \\
    -d '{"projectId":"$PROJECT","definitionKey":"expense-approval",
         "variables":{"amount":2400,"currency":"GBP","description":"Conference tickets","submittedBy":"alice"}}'

An amount of 2400 needs a director, 500 a manager, and 40 is approved without
anyone being asked. docs/data-flow.md traces all three.

The supplier check calls https://api.example.com, which does not exist, so its
service task will fail and raise an incident — that is the intended lesson.
Point http_url at something real to watch the mapping work.
EOF
