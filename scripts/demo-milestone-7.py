"""
The Milestone 7 demonstration, run against a fresh server.

An operator configures limits and registration policy, a moderator removes a
message and suspends somebody, a filtered word is blocked, a disposable chat
expires, and the audit log accounts for all of it.

Every step goes through the HTTP API exactly as a client would. Nothing is
asserted by reading the database except the two things that are not visible
through the API at all — the clock, which a demonstration cannot wait out, and
the retention sweep's own timing.
"""

import json
import subprocess
import sys
import time
import urllib.error
import urllib.request

API = "http://localhost:8080"
PASSWORD = "kilimanjaro-2026-tdc"

passed, failed = [], []


def check(name, ok, detail=""):
    (passed if ok else failed).append(name)
    print(f"  {'PASS' if ok else 'FAIL'}  {name}{'  — ' + detail if detail else ''}")


def request(method, path, body=None, token=None, headers=None):
    data = json.dumps(body).encode() if body is not None else None
    head = {"content-type": "application/json"}
    if token:
        head["authorization"] = f"Bearer {token}"
    if headers:
        head.update(headers)

    req = urllib.request.Request(API + path, data=data, method=method, headers=head)
    try:
        with urllib.request.urlopen(req) as response:
            raw = response.read()
            return response.status, (json.loads(raw) if raw else None)
    except urllib.error.HTTPError as error:
        raw = error.read()
        try:
            return error.code, json.loads(raw)
        except Exception:
            return error.code, raw.decode()[:200]


def sql(statement):
    """Only for the clock: a demonstration cannot wait out a retention period."""
    subprocess.run(
        ["psql", "-h", "127.0.0.1", "-p", "55433", "-U", "tdc", "-d", "tdc",
         "-tAc", statement],
        env={"PGPASSWORD": "tdc", "PATH": "/usr/bin:/bin"},
        check=True, capture_output=True,
    )


def settle():
    """The settings cache holds for thirty seconds; a change is not instant."""
    time.sleep(32)


print("\n=== First run: the operator sets the server up ===")

status, _ = request("POST", "/api/setup", {
    "username": "mwendeshaji", "password": PASSWORD,
    "displayName": "Mwendeshaji", "serverName": "TDC",
})
check("the first administrator is created", status in (200, 201), f"HTTP {status}")

status, session = request("POST", "/api/auth/signin",
                          {"username": "mwendeshaji", "password": PASSWORD})
operator = session["token"]
check("the operator can sign in", status == 200)


print("\n=== 1. The operator configures limits and registration policy ===")

status, _ = request("PUT", "/api/admin/settings/limits.max_users",
                    {"value": "3"}, operator)
check("a capacity limit is set", status == 204, f"HTTP {status}")

status, usage = request("GET", "/api/admin/usage", None, operator)
check("usage reports the limit", usage["users"]["limit"] == 3,
      f"{usage['users']['used']} of {usage['users']['limit']}")

# Two more accounts, which reaches the limit of three.
members = {}
for name in ("amina", "juma"):
    status, _ = request("POST", "/api/auth/register",
                        {"username": name, "password": PASSWORD})
    check(f"{name} registers under the open policy", status == 201, f"HTTP {status}")
    _, session = request("POST", "/api/auth/signin",
                         {"username": name, "password": PASSWORD})
    members[name] = session["token"]

status, _ = request("POST", "/api/auth/register",
                    {"username": "wanne", "password": PASSWORD})
check("the fourth account is refused by the limit", status == 403, f"HTTP {status}")

status, _ = request("PUT", "/api/admin/settings/limits.max_users",
                    {"value": "0"}, operator)
status, _ = request("PUT", "/api/admin/settings/registration.policy",
                    {"value": "closed"}, operator)
check("registration is closed", status == 204, f"HTTP {status}")
settle()

status, _ = request("POST", "/api/auth/register",
                    {"username": "mgeni", "password": PASSWORD})
check("registration is refused while closed", status == 403, f"HTTP {status}")

# Existing accounts are untouched by the policy change.
status, _ = request("GET", "/api/me", None, members["amina"])
check("an existing account still works after the policy changed", status == 200)


print("\n=== 2. A moderator removes a message and suspends somebody ===")

status, room = request("POST", "/api/rooms",
                       {"slug": "general", "name": "General", "visibility": "public"},
                       operator)
general = room["id"]
check("a channel is created", status == 201, f"HTTP {status}")

for name in ("amina", "juma"):
    request("POST", f"/api/rooms/{general}/join", None, members[name])

status, message = request("POST", f"/api/rooms/{general}/messages",
                          {"body": "Ujumbe utakaoondolewa"}, members["juma"])
offending = message["id"]
check("somebody sends a message", status == 201, f"HTTP {status}")

status, _ = request("DELETE", f"/api/rooms/{general}/messages/{offending}",
                    None, operator)
# 201: the deletion is itself an event, and the response is that event.
check("the moderator removes it", status in (200, 201, 204), f"HTTP {status}")

status, timeline = request("GET", f"/api/rooms/{general}/timeline?limit=50",
                           None, members["amina"])
removed = [e for e in timeline if e["id"] == offending]
check("the removed message discloses nothing",
      bool(removed) and removed[0]["redacted"] and not removed[0]["content"].get("body"),
      "the placeholder remains, its content gone")

status, people = request("GET", "/api/admin/users", None, operator)
juma_id = next(p["id"] for p in people if p["username"] == "juma")

status, _ = request("POST", f"/api/admin/users/{juma_id}/suspend",
                    {"reason": "Ujumbe usiofaa mara kwa mara", "hours": 24}, operator)
check("the moderator suspends them", status == 204, f"HTTP {status}")

status, _ = request("GET", "/api/me", None, members["juma"])
check("suspension ends their session at once", status == 401, f"HTTP {status}")

status, people = request("GET", "/api/admin/users", None, operator)
juma = next(p for p in people if p["username"] == "juma")
check("the suspension is legible to whoever reviews it",
      juma["suspended"] and juma["suspensionNote"] == "Ujumbe usiofaa mara kwa mara",
      juma.get("suspensionNote", ""))


print("\n=== 3. A filtered word is blocked ===")

request("PUT", "/api/admin/settings/filter.words", {"value": "matusi"}, operator)
status, _ = request("PUT", "/api/admin/settings/filter.action",
                    {"value": "block"}, operator)
check("the operator configures a content filter", status == 204, f"HTTP {status}")
settle()

status, refusal = request("POST", f"/api/rooms/{general}/messages",
                          {"body": "hii ni matusi"}, members["amina"])
check("a matching message is refused", status == 422, f"HTTP {status}")
check("the refusal does not name the word",
      "matusi" not in json.dumps(refusal),
      json.dumps(refusal)[:70])

status, _ = request("POST", f"/api/rooms/{general}/messages",
                    {"body": "the class was long"}, members["amina"])
check("an innocent message is not caught", status == 201, f"HTTP {status}")


print("\n=== 4. A disposable chat expires ===")

status, chat = request("POST", "/api/rooms",
                       {"slug": "fupi", "name": "Fupi", "visibility": "public",
                        "kind": "disposable", "messageTtlSeconds": 3600}, operator)
disposable = chat["id"]
check("a disposable chat is created", status == 201, f"HTTP {status}")
check("its lifetime is disclosed before anybody posts",
      chat.get("retentionSeconds") == 3600 and chat.get("retentionFromChannel"),
      f"kept {chat.get('retentionSeconds')}s")

status, _ = request("POST", f"/api/rooms/{disposable}/messages",
                    {"body": "Siri itakayopotea"}, operator)
check("a message is sent to it", status == 201, f"HTTP {status}")

# The clock, which a demonstration cannot wait out.
sql(f"UPDATE events SET created_at = created_at - (7200 * 1000) "
    f"WHERE room_id = '{disposable}'")
print("  ....  waiting for the server's own expiry sweep")

expired = False
for _ in range(40):
    time.sleep(10)
    status, timeline = request("GET", f"/api/rooms/{disposable}/timeline?limit=50",
                               None, operator)
    if not any("Siri itakayopotea" in json.dumps(e) for e in timeline):
        expired = True
        break

check("the server expires it without being asked", expired)
check("a client offline throughout is served nothing",
      expired and not any("Siri itakayopotea" in json.dumps(e) for e in timeline))


print("\n=== 5. Every one of those actions is in the audit log ===")

status, entries = request("GET", "/api/admin/audit?limit=200", None, operator)
actions = [e["action"] for e in entries]

for action, what in [
    ("server.setting.changed", "the settings the operator changed"),
    ("message.removed", "the message a moderator removed"),
    ("user.suspended", "the account a moderator suspended"),
    ("user.created", "the accounts that were created"),
]:
    check(f"the log records {what}", action in actions, action)

status, export = None, None
req = urllib.request.Request(f"{API}/api/admin/audit/export",
                             headers={"authorization": f"Bearer {operator}"})
with urllib.request.urlopen(req) as response:
    lines = [l for l in response.read().decode().strip().split("\n") if l]
check("the whole log exports as one entry per line",
      len(lines) > 0 and all(json.loads(l).get("action") for l in lines),
      f"{len(lines)} entries")


print(f"\n{'=' * 60}")
print(f"  {len(passed)} passed, {len(failed)} failed")
if failed:
    for name in failed:
        print(f"    FAILED: {name}")
print(f"{'=' * 60}")
sys.exit(1 if failed else 0)
