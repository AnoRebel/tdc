# Integrating with a TDC server

This describes the HTTP API a program can use to read and write a TDC server:
how to authenticate, what the shapes look like, and where the edges are. It is
written for somebody building a bot, a bridge, a script or another client, not
for somebody reading the source.

Every example below runs against a server started with `docker compose up -d`,
reachable on `http://localhost:8080`. Substitute your own address and, if the
server is behind TLS as it should be in anything but a local trial, `https`.

## Shape of the API

Everything lives under `/api`. Requests and responses are JSON, except file
uploads, which are `multipart/form-data`, and file downloads, which are the
file. There is one live connection, a WebSocket at `/api/live`, carrying
everything that happens while a client is connected.

Three endpoints need no credential, because a client needs them before it can
sign in and an uptime check holds no credential:

```sh
curl http://localhost:8080/api/health
curl http://localhost:8080/api/version
curl http://localhost:8080/api/server
```

`/api/health` answers `{"status":"ok","checks":{"database":"ok"}}` with status
200, or the same shape with `"unavailable"` and status 503. It says that the
server is unhealthy and never why: the reason goes to the log, where a caller
with no credential cannot read it.

`/api/version` answers the identity of the build:

```json
{ "version": "0.1.0", "protocolVersion": "0" }
```

`commit` and `buildTime` appear too when the binary was built by the release
workflow; a development build omits them. `protocolVersion` moves independently
of `version`, because the software can be released many times without the wire
protocol changing.

`/api/server` is what a client reads before it offers anybody a sign-in box —
what the server is called, what it can do, and what it wants clients to do:

```json
{
  "name": "TDC",
  "description": "A Tanzania Developers Community server",
  "canonicalAddress": "localhost:8080",
  "version": "0.1.0",
  "protocolVersions": [],
  "capabilities": ["attachments", "invitations", "public-directory", "scheduled-messages"],
  "defaultLanguage": "en",
  "obscureSensitiveMedia": true,
  "branding": { "name": "TDC", "initials": "TDC" }
}
```

**Read `capabilities` rather than assuming.** It lists the optional features
this server actually accepts requests for, and it is derived from the same
values the server enforces on — so a name absent from that list is a request
that will be refused, not merely a control a client could hide. A feature that
also needs something configured, such as GIF search needing a provider, appears
only when both hold.

`protocolVersions` is always empty. Federation is not implemented, so there is
no wire protocol to advertise a version of.

One surprise worth knowing: on a server whose visibility is set to private, or
limited to peers you are not among, `/api/server` answers **404**, not 403. A
refusal that distinguished "you may not know" from "there is nothing here" would
answer the question it was refusing.

## Authenticating

There is one credential: an opaque session token. A browser holds it in a
cookie; anything else sends it as a bearer token. They are the same token, and
the server reads whichever arrives.

### Signing in

```sh
curl -X POST http://localhost:8080/api/auth/signin \
  -H 'Content-Type: application/json' \
  -d '{"username":"founder","password":"correct horse battery staple"}'
```

```json
{
  "user": {
    "id": "0d6f…",
    "username": "founder",
    "displayName": "Founder",
    "address": "founder@localhost:8080"
  },
  "token": "…",
  "expiresAt": "2026-10-17T09:15:00Z"
}
```

The response also sets the `tdc_session` cookie, which is what the web client
uses. For an integration, take `token` from the body and send it on every
subsequent request:

```sh
curl http://localhost:8080/api/me \
  -H "Authorization: Bearer $TOKEN"
```

`address` is the federated form, `user@server`, built from the server's
`canonical_address`. It is already in every response that names a person, so
that identifiers stay unambiguous once servers do exchange messages. An operator
who has not set a canonical address gets `localhost` in that position, which is
one of several reasons to set it.

A failed sign-in answers 401 with `{"message":"the username or password is
incorrect"}` — the same message whether or not the account exists.

### API tokens

A program should not be holding somebody's password. Issue it a token instead:

```sh
curl -X POST http://localhost:8080/api/me/tokens \
  -H "Authorization: Bearer $SESSION" \
  -H 'Content-Type: application/json' \
  -d '{"label":"deploy bot","scope":["channel.post","attachment.upload"]}'
```

```json
{
  "id": "…",
  "label": "deploy bot",
  "scope": ["channel.post", "attachment.upload"],
  "created": "2026-09-21T10:00:00Z",
  "token": "tdc_…"
}
```

**`token` is shown once.** It is stored hashed and nobody can recover it, not
even the person who made it — a lost one is replaced rather than found. It
starts `tdc_` so a secret scanner can recognise it as a credential rather than
as an anonymous string.

Use it exactly like a session token, in the same header, on every route:

```sh
curl http://localhost:8080/api/rooms -H "Authorization: Bearer tdc_…"
```

**A token does what its scope names and what its account still holds**, and
never more. The two are intersected on every request rather than frozen when
the token was issued, so taking a role away from an account takes it from that
account's tokens at the same moment. Scope names come from
`GET /api/admin/permissions`; an unknown name is refused rather than ignored,
and one the account does not hold is refused too — a token is a way to delegate
authority, never a way to acquire it.

`expiresAt` is optional. Without it the token does not expire on its own, which
is right for a deployment that runs for years; with it, a token for an
afternoon's script stops mattering by the evening.

`GET /api/me/tokens` lists them without their secrets, including when each was
last used, so a token nobody has touched for a year is easy to spot.
`DELETE /api/me/tokens/{id}` stops one working immediately.

Two things a token deliberately cannot do. It cannot issue another token — a
program able to mint its own successor would make revocation less than final —
and it does not appear in the account's list of signed-in devices, because it is
not one.

**Recovering an account ends every token it held.** A password is changed after
a compromise, and a token minted by whoever had the account is exactly what
would let them back in.

### Signing in as an account instead

A bot may still authenticate as an account somebody made for it, which is what
to do when it needs to act continuously as a distinct identity rather than on
somebody's behalf. Sessions last 30 days by default
(`server.session_lifetime`) and are renewed by use.

### Signing out

```sh
curl -X POST http://localhost:8080/api/auth/signout \
  -H "Authorization: Bearer $TOKEN"
```

Answers 204 and revokes that one session. A token is not recoverable afterwards.

### First-run setup

A server with no accounts has exactly one way in:

```sh
curl http://localhost:8080/api/setup           # {"setupRequired":true}

curl -X POST http://localhost:8080/api/setup \
  -H 'Content-Type: application/json' \
  -d '{"username":"founder","password":"correct horse battery staple"}'
```

That creates the first administrator and closes the path. Asking again answers
409, and the account cannot be created a second time. There is no default
credential at any point, which is why this endpoint exists at all.

`email` and `displayName` are accepted here too. So is `invitation`, because the
endpoint shares a request shape with registration — but it is ignored, since
there is nobody to have issued one.

### Registering

```sh
curl -X POST http://localhost:8080/api/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"amina","email":"amina@example.org","password":"…","displayName":"Amina"}'
```

Answers 201 with the new account. Under the `invitation` registration policy,
add `"invitation":"<code or token>"`; under `domain`, the email must be in an
admitted domain. A refusal under policy is 403, and a server at its account
limit refuses cleanly before creating anything.

## What is not here

Worth stating plainly, so that nobody builds against it:

- **No OAuth2, and no passkeys.** Password and session token are the whole of
  authentication today.
- **No account recovery.** There is no password-reset endpoint. An account that
  has lost its password is an administrator's problem, not the holder's.
- **No federation.** Addresses name a home server and events are signed, but
  nothing crosses a server boundary. `protocolVersions` is empty for this
  reason.
- **No OpenAPI document.** `GET /api/description` returns a machine-readable
  list of every endpoint — method, path, parameters, what a caller must carry
  and which permission it needs — generated from the routing table, so it
  describes what the server serves rather than what somebody wrote down. It is
  deliberately not OpenAPI: a schema claiming to describe request and response
  bodies would mean inferring shapes that are not there, and a schema that is
  confidently wrong about a body is worse than none, because somebody generates
  a client from it and finds out at runtime. For bodies, this guide is the
  description.
- **No API versioning.** Paths are `/api/…` with no version segment. The
  protocol version in `/api/version` is the thing to check compatibility
  against.

## Errors

Every error is the same shape — one key:

```json
{ "message": "authentication is required" }
```

The status code carries the category and the message carries the detail. A
permission refusal names the permission that would have allowed the request, so
a caller can ask for the right thing rather than guess:

```json
{ "message": "permission channel.create is required" }
```

Nothing else is in the body. A development server with
`server.expose_internal_errors` on adds an `error` key with internal detail, and
that setting exists for local debugging only — it can disclose internals to an
unauthenticated caller, and the server warns about it at startup.

### Rate limits

A refusal for rate is 429 with a `Retry-After` header in seconds. There are no
`X-RateLimit-*` headers; read `Retry-After` and wait.

Signing in is limited per account (8 attempts in 15 minutes, then a 15-minute
lockout) and per source address (30 in 15 minutes, then 5 minutes). The
per-account limit is keyed on a normalised username, so varying the case does
not buy a fresh allowance. A successful sign-in resets it. Registration is
limited to 5 per hour per address.

Filing a report is limited too, and is the one case that answers 429 without a
`Retry-After` header.

## Rooms

A room is a channel, a thread's parent, a direct conversation or a disposable
chat — one type with a `kind`.

```sh
curl http://localhost:8080/api/rooms -H "Authorization: Bearer $TOKEN"
```

```json
[
  {
    "id": "5a1c…",
    "slug": "general",
    "name": "General",
    "topic": "Anything",
    "kind": "channel",
    "visibility": "public",
    "address": "general@localhost:8080",
    "archived": false,
    "member": true
  }
]
```

`retentionSeconds` and `retentionFromChannel` appear when a retention period
applies, the latter saying whether it came from the channel rather than the
server-wide default.

Creating one takes the `channel.create` permission:

```sh
curl -X POST http://localhost:8080/api/rooms \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"slug":"general","name":"General","topic":"A channel to look at"}'
```

Answers 201 with the room. `visibility` may be `public` or `private`; `kind` may
be `channel` or `disposable`, and a disposable room must also give a positive
`messageTtlSeconds` or it is refused with a message saying so.

A private room a caller cannot see answers 404 rather than 403, throughout. A
non-member cannot distinguish "forbidden" from "does not exist", which is the
point.

### Reading a timeline

```sh
curl "http://localhost:8080/api/rooms/$ROOM/timeline" \
  -H "Authorization: Bearer $TOKEN"
```

```json
[
  {
    "id": "b14e…",
    "type": "message.sent",
    "sender": "founder@localhost:8080",
    "sequence": 1,
    "content": { "body": "Hello" },
    "sentAt": "2026-09-17T09:02:11Z",
    "redacted": false
  }
]
```

**The timeline takes no parameters and is not paginated.** It returns the 100
most recent events in the room, oldest first, with thread replies excluded.
There is no `limit`, `before`, `after` or cursor. An integration that needs more
history than that has no endpoint for it today, and an integration that needs to
keep up with a busy room should hold the live connection rather than poll — the
live connection replays what was missed, which is the mechanism that exists for
this.

`content` is passed through as the raw event content, so its shape depends on
the event type. `sequence` is per-room and monotonic, and is the number to
remember for replay.

### Sending a message

```sh
curl -X POST "http://localhost:8080/api/rooms/$ROOM/messages" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"body":"Hello from a script","clientToken":"3f9a2c7e-1"}'
```

Answers **201** with the created message in the same shape the timeline returns.

`clientToken` is the idempotency key, and an integration should always send one.
Generate it before the first attempt and reuse it on every retry: a repeat with
the same token returns the original message rather than posting a second one.
Without it, a request that succeeded on the server and failed on the way back
becomes a duplicate message every time the caller retries.

`attachmentIds` carries files uploaded beforehand — see below.

Editing and deleting are `PATCH` and `DELETE` on
`/api/rooms/:id/messages/:eventId`, and both also answer **201** with the
resulting message rather than 200 or 204. Deleting leaves a placeholder rather
than removing the event: a deleted message leaves evidence that something was
there.

### Offering a choice

A message may carry buttons:

```sh
curl -X POST "http://localhost:8080/api/rooms/$ROOM/messages" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "body": "Deploy to production?",
    "clientToken": "3f9a2c7e-2",
    "actions": [
      {"name": "deploy", "label": "Deploy", "once": true},
      {"name": "hold",   "label": "Not yet"},
      {"name": "log",    "label": "See the build", "capability": "open",
       "url": "https://ci.example.test/builds/41"}
    ]
  }'
```

`name` is what a press reports back, so your program can tell which button it
was without matching on a label somebody may translate. `label` is what a
reader sees. At most five per message.

**There is a boundary worth understanding.** Almost every action does one
thing: a press becomes an event to whoever posted the message, and your program
decides what it means. Your program is the only thing that knows, so it is the
only thing that decides.

The exception is a short, fixed set of things a **client** does by itself,
named by `capability`. There is one today:

| Capability | What it does                       |
| ---------- | ---------------------------------- |
| `open`     | Opens `url`, which must be http(s) |

**A capability this server does not declare is refused when you post the
message**, with a 400 naming what was refused — not when somebody presses the
button. A message that reached the channel and then failed on being touched has
already been read, and somebody has already decided the button was worth
trusting. `javascript:`, `data:` and `file:` links are refused for the same
reason.

`once` makes an action pressable one time in total, by anybody, and `expires`
gives it a deadline. Both are enforced by the server so your program does not
have to defend against replay itself.

A message carrying no `actions` is exactly the message it was before this
existed — the key is absent rather than empty, and nothing extra to parse.

### Hearing that somebody pressed

A press becomes an event in the room, which is how it reaches you: over the
live connection you already hold, through the same delivery every other event
uses. There is no webhook to register and no second mechanism with its own
retries.

```json
{
  "type": "action.pressed",
  "sender": "juma@chat.example.test",
  "content": { "action": "deploy", "pressed": "$abc123…" }
}
```

`action` is the `name` you gave the button. `pressed` is the message it was on.
`sender` is who pressed it — established by the server from the authenticated
request, so it is not something a client can claim.

**Exactly once.** The same person pressing the same button twice is refused
with **409**: a repeat is a retry or a mistake, and a program receiving two
events for one decision cannot tell that from somebody changing their mind. An
action marked `once` is stricter — one press in total, by anybody — so a
question offered to a room is answered once. A press after `expires` is **410**,
and a press naming a button the message does not offer is **404**.

You never have to defend against replay yourself. That is a database rule here,
not something the application remembers.

To press one as a client:

```sh
curl -X POST \
  "http://localhost:8080/api/rooms/$ROOM/messages/$EVENT/actions/deploy" \
  -H "Authorization: Bearer $TOKEN"
```

Answers **201** with the `action.pressed` event. Pressing needs membership of
the channel, not merely the ability to read it — answering a question in a
channel is taking part in it.

### Attachments

A file is uploaded to the room first, and the returned identifier is then
attached to a message. That order is deliberate — a refusal costs only the
upload, and a message that fails to send does not take the file with it.

```sh
curl -X POST "http://localhost:8080/api/rooms/$ROOM/attachments" \
  -H "Authorization: Bearer $TOKEN" \
  -F file=@diagram.png
```

Then send a message with `{"body":"…","attachmentIds":["<id>"]}`.

Ask what the server will accept before uploading:

```sh
curl http://localhost:8080/api/attachments/limits -H "Authorization: Bearer $TOKEN"
```

which gives `maxFileSize`, `maxMessageSize`, `maxPerMessage`, `allowedTypes` and
`blockedTypes`. The server enforces all of them regardless of what a client
checked, and a refusal names the limit that was broken. The declared content
type is not believed: the type is detected from the file's own bytes, and that
detected type is what governs.

Downloading is `GET /api/attachments/:id`, authenticated and authorised —
knowing where a file is stored grants nothing on its own.

## The live connection

One WebSocket at `/api/live` carries messages, presence, typing and read state
while a client is connected.

It authenticates exactly like every other route: the `tdc_session` cookie, or an
`Authorization: Bearer` header. There is no token query parameter. A browser
cannot set headers on a WebSocket handshake and so relies on the cookie; a
non-browser client can send the header, which is what the desktop client does.

The connection's origin is checked against `server.allowed_origins`, falling
back to the server's own canonical address when that list is empty.

Immediately after the upgrade the server sends:

```json
{ "type": "ready" }
```

Then subscribe to the rooms you care about, naming the last sequence you hold
for each:

```json
{
  "action": "subscribe",
  "rooms": ["5a1c…"],
  "since": { "5a1c…": 41 }
}
```

The server answers `{"type":"subscribed","roomId":"5a1c…"}` and replays whatever
was missed. Subscriptions change on a live connection — there is no need to
reconnect to add or drop a room.

Frames from the server carry a `type`:

- `ready` — the connection is established.
- `subscribed` — with `roomId`.
- `event` — with `room` (the `room@server` address), `roomId`, `sequence` and
  `payload`.
- `resync` — with `roomId`. The gap was too large to replay, so fetch the
  timeline afresh.
- `pong` — the answer to `{"action":"ping"}`.
- `error` — with `payload` `{"message":"…"}`.

An `event` payload looks like:

```json
{
  "id": "b14e…",
  "type": "message.sent",
  "sender": "founder@localhost:8080",
  "content": { "body": "Hello" },
  "createdAt": 1758099731000,
  "redacted": false
}
```

`type` is the event's own type — the same string the timeline gives. The ones a
program usually cares about are `message.sent`, `message.edited`,
`message.deleted` and `action.pressed`.

**Note the time encoding differs from the REST API.** Here `createdAt` is epoch
milliseconds as a number; the REST timeline gives `sentAt` as an RFC 3339
string. Same instant, two encodings, and a client reading both needs to handle
each.

The other actions a client may send are `unsubscribe` (same shape as
`subscribe`), `ping`, and `viewing` with a single `room` — the conversation
currently on screen, which is how the server knows not to raise a notification
for something the person is already looking at. An empty `room` means none.

Keepalives are WebSocket protocol pings sent by the server every 45 seconds by
default (`server.keepalive`), not JSON frames. An idle connection therefore
carries no application traffic at all, which is what makes it cheap on a phone's
metered data. The JSON `ping`/`pong` pair is separate and optional.

Two bounds worth designing around: a client message is limited to 16 KiB, and a
client that cannot keep up with its send queue is disconnected rather than
buffered indefinitely. Reconnect with randomised backoff — a fleet that all
dropped together and reconnected in lockstep would arrive as one spike.

Fanout is scoped by authorisation on every frame, and that is evaluated live. A
member removed from a channel stops receiving its events immediately, without
the connection being re-established.

## Search

Two endpoints, both taking the search text as `q`:

```sh
curl "http://localhost:8080/api/search?q=deployment" \
  -H "Authorization: Bearer $TOKEN"

curl "http://localhost:8080/api/search/everything?q=amina" \
  -H "Authorization: Bearer $TOKEN"
```

`/api/search` searches messages. `/api/search/everything` searches messages,
conversations and people together in one request, each kind capped separately
and kept small, because it is built to run on every keystroke of a search box.

They are `GET` requests deliberately, so that a search is shareable and
bookmarkable — the term is the caller's own words about their own conversations.

Authorisation is enforced on every result. Search never returns a message from a
channel the caller cannot read, or a channel they could not otherwise discover.
An empty `q` matches nothing rather than everything.

Nothing you can put in `q` produces a syntax error. The text is treated as words
to find rather than as a query language, so a caller passing through whatever a
user typed does not need to sanitise it.

## Cross-origin requests

A browser-based integration served from another origin needs that origin in
`server.allowed_origins`, which the operator sets. When the list is empty the
CORS middleware is not mounted at all, and no CORS headers appear on any
response — which is correct for the normal deployment, where the web app is
served from the same origin as the API.

Origins are matched exactly. There are no wildcards and no reflection of
arbitrary origins.

There is no CSRF token. What protects a cookie session is `SameSite=Lax` on the
cookie together with that origin allowlist. An integration using a bearer token
is not exposed to the question at all, and a bearer token is the right choice
for anything that is not a browser.

## Federated rooms

**Nothing about federation changes how your program authenticates.** You hold
an API token on one server, you call that server, and it deals with the others.
Server-to-server signing is between servers; your token never touches it.

What changes is what some rooms are. A room whose `homeServer` is set in the
room response lives on another server:

```sh
curl -H "Authorization: Bearer $TDC_TOKEN" \
  http://localhost:8080/api/rooms/$ROOM
```

```json
{
  "id": "…",
  "slug": "townsquare",
  "address": "townsquare@a.example.org",
  "homeServer": "a.example.org",
  "member": true
}
```

An ordinary room omits `homeServer` entirely, so a program written before
federation existed behaves exactly as it did.

### Reading is the same

The timeline, search, and the live connection all work unchanged on a
replicated room. Your server holds a full copy, so reading costs no round trip
to the home server and does not stop working when it is unreachable.

Messages carry a `sender` in `name@server` form, and for a federated room some
of those servers will not be yours. Treat the whole address as the identity —
two people called `amina` on different servers are different people, and
matching on the local part alone will eventually confuse them.

### Posting takes a different endpoint

```sh
# An ordinary room
curl -X POST -H "Authorization: Bearer $TDC_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"body":"hello"}' \
  http://localhost:8080/api/rooms/$ROOM/messages

# A room another server owns
curl -X POST -H "Authorization: Bearer $TDC_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"body":"hello"}' \
  http://localhost:8080/api/rooms/$ROOM/remote/messages
```

Branch on `homeServer` being present. The remote endpoint answers with the
event the home server created, including the `sequence` it assigned:

```json
{ "id": "$abc…", "sequence": 42, "sender": "yourbot@your.server" }
```

Note what it does **not** do: it stores nothing locally on the way out. Your
message appears in the room when the home server replicates it back, which is
moments later and is the same copy everybody else sees. A program that posts
and then immediately reads its own message from the timeline should retry
rather than conclude the post failed.

Joining follows the same shape: `/api/rooms/{id}/remote/join` rather than
`/api/rooms/{id}/join`.

### What can fail that could not before

| Status | What happened                                                                                                               |
| ------ | --------------------------------------------------------------------------------------------------------------------------- |
| `409`  | The home server refused — not a member, room archived, or its own rules said no. The message is its answer, passed through. |
| `409`  | The home server could not be reached. Retry; it is an outage, not a refusal.                                                |
| `404`  | The room is not federated, or your server no longer participates in it.                                                     |

A `409` whose body mentions reaching the peer is worth retrying with backoff. A
`409` naming a rule is not: retrying will produce the same answer.

**Attachments are not forwarded yet.** They are held by the server that holds
the room, and yours has nowhere to put them that the other could read. Posting
text into a federated room works; posting a file does not.

### Finding federated rooms

```sh
curl -H "Authorization: Bearer $TDC_TOKEN" \
  http://localhost:8080/api/federation/servers

curl -H "Authorization: Bearer $TDC_TOKEN" \
  http://localhost:8080/api/federation/servers/a.example.org/rooms
```

The first lists servers yours is connected to; the second lists what one of
them publishes to yours. Both need only your ordinary token. An empty list from
the second means "nothing for you", which deliberately covers both "that server
publishes nothing" and "that server has not listed us" — the two are not
distinguishable, by design.

## Projects and issues

Projects are named by their key and issues by their identifier — `WEB-12` —
rather than by internal ids, in paths as in conversation. Both are matched
without regard to case. [Projects and issues](projects.md) explains what the
objects are and who may do what; this is how to reach them.

A private project, and every issue in it, answers a non-member exactly as a
project that does not exist does: 404, with the same body. A refusal to act on
something the caller can see is 403 and names the permission.

```sh
# Start a project. Answers 201 with the project, its states, and what the
# caller may do in it ("can").
curl -X POST http://localhost:8080/api/projects \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"key":"WEB","name":"Community website","visibility":"public"}'

# File an issue. It starts in the first state unless stateId says otherwise.
curl -X POST http://localhost:8080/api/projects/WEB/issues \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"Search ignores diacritics","description":"…"}'

# Move it. expectedStateId is the state you saw it in; give it, and a move made
# against an issue somebody else has just moved answers 409 with the issue as
# it now is, instead of overwriting their move.
curl -X POST http://localhost:8080/api/issues/WEB-1/state \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"stateId":"…","expectedStateId":"…"}'
```

| Endpoint                                                                               | What it does                                                                                                                                                    |
| -------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /api/projects`                                                                    | Projects the caller can see, with their role in each                                                                                                            |
| `POST /api/projects`                                                                   | Start a project (`project.create`)                                                                                                                              |
| `GET /api/projects/:key`                                                               | A project, its states in board order, and `can`                                                                                                                 |
| `PATCH /api/projects/:key`                                                             | Change `name`, `description` or `visibility`                                                                                                                    |
| `GET /api/projects/:key/members`                                                       | Its members and their roles                                                                                                                                     |
| `PUT /api/projects/:key/members/:username`                                             | Add somebody, or change their `role` (`admin` or `member`)                                                                                                      |
| `DELETE /api/projects/:key/members/:username`                                          | Remove somebody; anybody may remove themselves                                                                                                                  |
| `POST /api/projects/:key/states`                                                       | Add a state: `name`, `isCompletion`                                                                                                                             |
| `PATCH /api/projects/:key/states/:id`                                                  | Rename a state or change whether it means done                                                                                                                  |
| `PUT /api/projects/:key/states`                                                        | Reorder: `order` names every state's id exactly once                                                                                                            |
| `DELETE /api/projects/:key/states/:id`                                                 | Remove a state; `?moveTo=:id` is required while issues are in it                                                                                                |
| `GET /api/projects/:key/issues`                                                        | `state`, `label`, `priority` (ids), `assignee` (a username) narrow together; `sort=priority` orders by the project's scale; `limit` (≤200), `before` (a number) |
| `POST /api/projects/:key/issues`                                                       | File an issue                                                                                                                                                   |
| `GET /api/issues/:ref`                                                                 | One issue, with its project, `can`, and where it came from                                                                                                      |
| `PATCH /api/issues/:ref`                                                               | Change `title` or `description`                                                                                                                                 |
| `DELETE /api/issues/:ref`                                                              | Delete it (`issue.delete`); its history is kept                                                                                                                 |
| `POST /api/issues/:ref/state`                                                          | Move it                                                                                                                                                         |
| `GET /api/issues/:ref/history`                                                         | Every recorded change, oldest first                                                                                                                             |
| `GET /api/rooms/:id/issues`                                                            | Issues made from a room's messages, as the caller may see them                                                                                                  |
| `PUT` / `DELETE /api/issues/:ref/assignees/:username`                                  | Assign somebody who can see the issue, or take them off                                                                                                         |
| `PUT` / `DELETE /api/issues/:ref/labels/:id`                                           | Add or remove one of the project's labels                                                                                                                       |
| `PUT /api/issues/:ref/priority`                                                        | `priorityId`, or null to clear it                                                                                                                               |
| `POST /api/issues/:ref/relations`                                                      | `kind` (what this issue is to the other: `parent`, `child`, `blocks`, `blocked_by`) and `issue` (its identifier); a loop answers 409 naming it                  |
| `DELETE /api/issues/:ref/relations/:kind/:other`                                       | Remove one                                                                                                                                                      |
| `GET`, `POST /api/projects/:key/labels`; `PATCH`, `DELETE …/labels/:id`                | The project's labels: `name`, `colour` (`#rrggbb`)                                                                                                              |
| `GET`, `POST`, `PUT /api/projects/:key/priorities`; `PATCH`, `DELETE …/priorities/:id` | The priority scale; `PUT` reorders it, most urgent first                                                                                                        |
| `GET /api/me/issues`                                                                   | Everything assigned to the caller, in every project they can see                                                                                                |
| `GET /api/projects/:key/board`                                                         | Every state as a column: `stateId`, `name`, `isCompletion`, `total`, the first page of `cards`, and `next` to continue; `limit` up to 50                        |
| `GET /api/projects/:key/board/:state?after=`                                           | The next page of one column, from the `next` its last page gave                                                                                                 |
| `GET`, `POST /api/projects/:key/views`                                                 | A project's saved views; save one: `name`, `layout`, `filter`, `shared`                                                                                         |
| `GET`, `POST /api/views`                                                               | Saved views across projects; save one                                                                                                                           |
| `GET`, `PATCH`, `DELETE /api/views/:id`                                                | A saved view; only its owner changes or deletes it                                                                                                              |
| `GET /api/views/:id/results`                                                           | Open a view as the caller: `issues` for a list, `board` for a board                                                                                             |
| `POST /api/projects/:key/issues/bulk`                                                  | One change to many issues: `issues`, `change` (`kind`, `value`); `results` per issue                                                                            |

An issue made from a message names it on creation, as two identifiers — the
message is referred to, never copied:

```sh
curl -X POST http://localhost:8080/api/projects/WEB/issues \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"…","originChannel":"'$ROOM'","originEvent":"'$MESSAGE'"}'
```

The caller must be able to read the message; one they cannot answers 404,
exactly as one that does not exist. `GET /api/issues/:ref` then carries
`origin`: `{"disclosed": false}` and nothing else for a reader who cannot see
the room, or `disclosed`, `roomId`, `roomName`, `eventId` and `message`
(`sender`, `sentAt`, and `body` as it reads now, or `removed`) for one who can.
`GET /api/rooms/:id/issues` lists the issues made from a room's messages that the
caller may see, as `eventId`, `key`, `title`, `state` and `completed`.

An issue's discussion is a room whose id is the issue's `id`. Read, post, edit
and react through the ordinary room endpoints — `POST /api/rooms/:id/join` to
take part, then `POST /api/rooms/:id/messages` — with the issue's visibility as
the room's. Its `kind` is `issue-discussion`, and it never appears in
`GET /api/rooms`.

A history entry is an event from the project's stream, as recorded: `type`
(`issue.created`, `issue.state_changed`, `issue.title_changed`, …), `actor`
(user@server), `at`, `sequence`, and `content`. A change to a field carries
`field`, `previous` and `new`; a move also carries `previousName` and
`newName`, the names the states had at the time. Times are RFC 3339 instants.

## Administration

The endpoints under `/api/admin` each name the permission that governs them, and
each is enforced on the server. Of most use to an integration:

| Endpoint                           | Permission          | What it is                              |
| ---------------------------------- | ------------------- | --------------------------------------- |
| `GET /api/admin/settings`          | `server.configure`  | Runtime policy and its values           |
| `PUT /api/admin/settings/:key`     | `server.configure`  | Change one                              |
| `GET /api/admin/features`          | `server.configure`  | Feature toggles                         |
| `PUT /api/admin/features/:feature` | `server.configure`  | Turn one on or off                      |
| `GET /api/admin/usage`             | `server.configure`  | Usage against limits                    |
| `GET /api/admin/configuration`     | `server.configure`  | Startup configuration, secrets redacted |
| `GET /api/admin/audit`             | `server.audit.read` | A page of the audit log                 |
| `GET /api/admin/audit/export`      | `server.audit.read` | The whole log, newline-delimited JSON   |
| `GET /api/admin/users`             | `user.suspend`      | Accounts on this server                 |
| `POST /api/admin/invitations`      | `user.invite`       | Issue an invitation to the server       |

Reading the audit log takes its own permission, separate from the ones that let
somebody change things — so that being able to act is not the same as being able
to read who acted.

Direct conversations are not readable through any of these. An administrator
querying ordinary endpoints cannot read other people's direct messages, and that
is enforced rather than merely undocumented.

## A worked example

A program that asks a channel a question and acts on the answer. It is short
enough to read in one go and does the three things every integration does:
authenticate, say something, and listen for what happens next.

**Before you start**, issue it a token and find the channel:

```sh
# Signed in as yourself, in a browser or with curl.
curl -X POST http://localhost:8080/api/me/tokens \
  -H "Authorization: Bearer $SESSION" \
  -H 'Content-Type: application/json' \
  -d '{"label":"deploy bot","scope":["channel.post","channel.read"]}'

# The channel it should talk in.
curl http://localhost:8080/api/rooms -H "Authorization: Bearer $SESSION"
```

Keep the `token` from the first response — it is shown once. Then:

```ts
#!/usr/bin/env bun
const SERVER = process.env.TDC_SERVER ?? "http://localhost:8080";
const TOKEN = process.env.TDC_TOKEN!;
const CHANNEL = process.env.TDC_CHANNEL!;

const authorised = { Authorization: `Bearer ${TOKEN}`, "Content-Type": "application/json" };

async function api(path: string, init?: RequestInit) {
  const response = await fetch(`${SERVER}${path}`, { ...init, headers: authorised });
  if (!response.ok) {
    throw new Error(
      `${init?.method ?? "GET"} ${path}: ${response.status} ${await response.text()}`,
    );
  }
  return response.status === 204 ? undefined : response.json();
}

// Ask the question.
const asked = await api(`/api/rooms/${CHANNEL}/messages`, {
  method: "POST",
  body: JSON.stringify({
    body: "Deploy build 41 to production?",
    clientToken: crypto.randomUUID(),
    actions: [
      { name: "deploy", label: "Deploy", once: true },
      { name: "hold", label: "Not yet" },
    ],
  }),
});

// Listen for the answer.
const socket = new WebSocket(`${SERVER.replace(/^http/, "ws")}/api/live`, {
  headers: { Authorization: `Bearer ${TOKEN}` },
} as never);

socket.addEventListener("message", async (frame) => {
  const message = JSON.parse(String(frame.data));

  // Subscribe once the connection is ready.
  if (message.type === "ready") {
    socket.send(JSON.stringify({ action: "subscribe", rooms: [CHANNEL] }));
    return;
  }
  if (message.type !== "event") return;

  const event = message.payload;
  if (event.type !== "action.pressed") return;
  // Presses on somebody else's message arrive here too.
  if (event.content.pressed !== asked.id) return;

  const who = event.sender.split("@")[0];
  await api(`/api/rooms/${CHANNEL}/messages`, {
    method: "POST",
    body: JSON.stringify({
      body:
        event.content.action === "deploy"
          ? `Deploying build 41, at ${who}'s word.`
          : `Holding, at ${who}'s word.`,
      clientToken: crypto.randomUUID(),
    }),
  });

  socket.close();
});
```

```sh
TDC_TOKEN=tdc_… TDC_CHANNEL=22089cc6-… bun run deploybot.ts
```

The message appears with two buttons. Somebody presses one, and the bot posts
its reply naming who decided.

**Four things in there are not incidental.**

`clientToken` on every post, a fresh one per message. It is what makes a retry
safe.

Subscribing on `ready` rather than on `open`. The socket is open before the
server has said anything; `ready` is the server saying it is listening.

Checking `event.content.pressed` against the message you sent. A subscription
carries every event in the channel, including presses on messages other
programs posted.

Percent-encoding an event identifier if you build a path with one. They begin
with `$`, and `encodeURIComponent` is what a correct client uses — this
example never does, because it only ever reads identifiers out of events.

## Writing an integration that behaves

**Send a `clientToken` on every message, and reuse it across retries.** It is
the only thing standing between a network hiccup and a duplicated message.

**Hold the live connection rather than polling the timeline.** The timeline is
capped at 100 events with no pagination, so polling both misses things on a busy
room and wastes requests on a quiet one. The connection replays the gap.

**Read `capabilities` from `/api/server` and adapt.** A control for a feature the
server has switched off is a control that always fails.

**Honour `Retry-After`.** It is the only backoff signal the server gives.

**Expect 404 where you might expect 403.** Private rooms and undiscoverable
servers refuse that way on purpose, and a client that treats 404 as "gone
forever" will do the wrong thing for somebody who has merely not been invited
yet.

**Treat `content` as opaque unless you know the event type.** It is passed
through from the event, and new types can appear.
