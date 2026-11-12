#!/usr/bin/env bash
#
# Project federation Milestone 2: remote members, before anything is shared.
#
# On one server, built from this checkout, with federation on: a project's
# administrator opens it to other servers and adds somebody from another server
# with a role, which is then changed. The project lists them, its history and
# the audit log record the opening, a member who does not administer it is
# refused naming the permission — and nothing about the project has left the
# server: it has no peers, no room federation, and the server never contacted
# the other server.
#
# Usage: scripts/demo-milestone-project-federation-2.sh   (SKIP_WEB_BUILD=1 to reuse a build)
DEMO_NAME=pf2
DEMO_PORT="${TDC_DEMO_PORT:-8412}"
source "$(dirname "${BASH_SOURCE[0]}")/demo-projects-lib.sh"

sql() {
  docker exec tdc-database-1 psql -U tdc -d "$DATABASE" -tAc "$1"
}

build
start_server

FOUNDER="$(sign_in founder)"
register bola
BOLA="$(sign_in bola)"
api "$FOUNDER" PUT /api/admin/settings/federation.enabled '{"value":"true"}' >/dev/null
api "$FOUNDER" POST /api/projects '{"key":"FED","name":"Federation","visibility":"private"}' >/dev/null
api "$FOUNDER" PUT /api/projects/FED/members/bola '{"role":"member"}' >/dev/null

say "Somebody elsewhere cannot join a project that is not opened"
check "adding dalila@b.example is refused while the project is closed" \
  test "$(status "$FOUNDER" PUT /api/projects/FED/members/dalila@b.example '{"role":"member"}')" = 409
check "a member who does not administer the project cannot open it" \
  test "$(status "$BOLA" PUT /api/projects/FED/federation '{"federated":true}')" = 403

say "The administrator opens it and adds dalila@b.example"
api "$FOUNDER" PUT /api/projects/FED/federation '{"federated":true}' >/dev/null
api "$FOUNDER" PUT /api/projects/FED/members/dalila@b.example '{"role":"admin"}' >/dev/null
api "$FOUNDER" PUT /api/projects/FED/members/dalila@b.example '{"role":"member"}' >/dev/null
MEMBERS="$(api "$BOLA" GET /api/projects/FED/members)"
note "members: $(echo "$MEMBERS" | json '", ".join(m["address"]+" ("+m["role"]+(", elsewhere" if m.get("remote") else "")+")" for m in d)')"
check "the project lists dalila@b.example as a member from elsewhere" \
  test "$(echo "$MEMBERS" | json '[m["role"] for m in d if m["address"]=="dalila@b.example" and m.get("remote")]')" = "['member']"
check "the switch is shown on the project as open" \
  test "$(api "$BOLA" GET /api/projects/FED | json 'd["federated"]')" = "True"

HISTORY="$(api "$BOLA" GET /api/projects/FED/history)"
check "the project's history records the opening and dalila's role, then its change" \
  test "$(echo "$HISTORY" | json '",".join(e["type"] for e in reversed(d) if e["type"] in ("project.federation_changed","project.member_added") and ("dalila" in str(e["content"]) or e["type"]=="project.federation_changed"))')" \
    = "project.federation_changed,project.member_added,project.member_added"
check "the audit log records the opening" \
  grep -q '"project.federation.changed"' <<<"$(api "$FOUNDER" GET /api/admin/audit)"

say "Nothing has left this server"
PROJECT="$(api "$FOUNDER" GET /api/projects/FED | json 'd["id"]')"
check "the project's room has no peers" \
  test "$(sql "SELECT count(*) FROM room_peers WHERE room_id = '${PROJECT}'")" = 0
check "the project's room is not open to room federation" \
  test "$(sql "SELECT federated FROM rooms WHERE id = '${PROJECT}'")" = f
check "no room on this server has a peer at all" \
  test "$(sql "SELECT count(*) FROM room_peers")" = 0
check "the server never contacted b.example" \
  bash -c "! grep -q 'b.example' '$WORK/server.log'"

say "Project federation Milestone 2 demonstrated"
note "a private project lists dalila@b.example with a role, changed once, held on this server alone"
note "opening it is shown on the project, recorded in its history and audited"
note "nothing about the project has been sent anywhere"
