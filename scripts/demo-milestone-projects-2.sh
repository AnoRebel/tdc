#!/usr/bin/env bash
#
# Projects Milestone 2: issues talk back.
#
# Two people discuss a bug in a private channel and one turns the message into
# an issue — in the browser. The issue shows the originating message to a
# member of the channel and only a non-disclosing reference to somebody outside
# it; the message shows the issue it became; and a comment on the issue, with a
# mention, travels the ordinary message path and notifies whoever it names.
#
# Usage: scripts/demo-milestone-projects-2.sh   (SKIP_WEB_BUILD=1 to reuse a build)
DEMO_NAME=m2
DEMO_PORT="${TDC_DEMO_PORT:-8402}"
source "$(dirname "${BASH_SOURCE[0]}")/demo-projects-lib.sh"

build
start_server

say "The people, the private channel they talk in, and the project their work goes to"
FOUNDER="$(sign_in founder)"
for person in amina bola outsider; do register "$person"; done

CHANNEL="$(api "$FOUNDER" POST /api/rooms '{"slug":"backend","visibility":"private"}' | json 'd["id"]')"
for person in amina bola; do
  INVITATION="$(api "$FOUNDER" POST "/api/rooms/${CHANNEL}/invitations" '{}' | json 'd["token"]')"
  api "$(sign_in "$person")" POST /api/invitations/redeem "{\"token\":\"${INVITATION}\"}" >/dev/null
done
api "$FOUNDER" POST /api/projects '{"key":"WEB","name":"Community website","visibility":"public"}' >/dev/null
for person in amina bola; do
  api "$FOUNDER" PUT "/api/projects/WEB/members/${person}" '{"role":"member"}' >/dev/null
done
note "amina and bola are in #backend (private) and in WEB; outsider is in neither"

say "In the browser: the discussion, the issue made from it, and a comment on the issue"
browser projects-milestone-2

say "From outside the browser: the link is a reference, and it respects the channel"
BOLA="$(sign_in bola)"
OUTSIDER="$(sign_in outsider)"

ISSUE_FOR_BOLA="$(api "$BOLA" GET /api/issues/WEB-1)"
check "the issue refers to the message in #backend" \
  test "$(echo "$ISSUE_FOR_BOLA" | json 'd["origin"]["disclosed"] and d["origin"]["roomName"]')" = "backend"
# What the author chose to write as the title and description is theirs; the
# link itself is two identifiers and nothing of the message.
CREATED="$(api "$BOLA" GET /api/issues/WEB-1/history | json 'json.dumps(d[0]["content"])')"
check "the creation event records the message as a reference: a room and an event, no copy" \
  test "$(echo "$CREATED" | json '(d.get("originChannel"), bool(d.get("originEvent")), "body" in d or "message" in d)')" \
    = "('${CHANNEL}', True, False)"
check "an outsider is told only that there was a conversation" \
  test "$(api "$OUTSIDER" GET /api/issues/WEB-1 | json 'json.dumps(d["origin"], separators=(",",":"))')" = '{"disclosed":false}'
check "an outsider cannot list the channel's issues either" \
  test "$(status "$OUTSIDER" GET "/api/rooms/${CHANNEL}/issues")" = "404"
check "the channel's member sees the message's issue" \
  test "$(api "$BOLA" GET "/api/rooms/${CHANNEL}/issues" | json 'd[0]["key"]')" = "WEB-1"

DISCUSSION="$(echo "$ISSUE_FOR_BOLA" | json 'd["id"]')"
TIMELINE="$(api "$BOLA" GET "/api/rooms/${DISCUSSION}/timeline")"
check "the comment is a message.sent in the issue's discussion room, and its edit a message.edited" \
  test "$(echo "$TIMELINE" | json '",".join(e["type"] for e in d if e["type"].startswith("message."))')" \
    = "message.sent,message.edited"
COMMENT="$(echo "$TIMELINE" | json '[e for e in d if e["type"]=="message.sent"][0]["id"]')"
check "bola, named in the comment, was notified of it as a mention" \
  test "$(api "$BOLA" GET '/api/me/notifications?grouped=false' \
    | json "[n['reason'] for n in d if n['eventId']=='${COMMENT}'][0] if [n for n in d if n['eventId']=='${COMMENT}'] else 'none'")" = "mention"
check "the outsider, who never took part, was told nothing" \
  test "$(api "$OUTSIDER" GET '/api/me/notifications?grouped=false' | json 'len(d)')" = "0"

say "Projects Milestone 2 demonstrated"
note "a channel message became an issue in one action, from the message's own menu"
note "the issue showed the message to a channel member and nothing but its existence to an outsider"
note "the message showed its issue, and a comment with a mention notified whoever it named"
