# The TDC federation protocol

This describes how two TDC servers find each other, prove who they are, and
agree on a language to speak. It is written so that somebody with no access to
this source tree could implement the other side of it.

Federation is optional. A server with it switched off is a complete TDC server;
nothing on this page is needed to run one.

> **Status.** This document covers discovery, identity, request signing, room
> replication, remote membership and posting, files, calls, and the events a
> project's stream holds, though projects are not shared yet. Cross-server browsing and
> moderation are still being built. Version `tdc/1` is not frozen until they
> land.

## What a server publishes

Every TDC server serves one document, without authentication, at a fixed path:

```sh
curl https://tdc.example.org/.well-known/tdc-federation
```

```json
{
  "canonicalAddress": "https://tdc.example.org",
  "keys": [
    { "id": "2026-01-key", "publicKey": "Yx3K…", "retired": true },
    { "id": "2026-07-key", "publicKey": "9fQ2…" }
  ],
  "versions": ["tdc/1"],
  "enabled": true
}
```

- **`canonicalAddress`** — how this server is known. It is a value the operator
  configured, never inferred from a request header, so it stays correct behind
  a proxy that rewrites `Host`.
- **`keys`** — the public halves of the keys this server signs events with.
  `publicKey` is base64url-encoded Ed25519. `id` is what an event's `keyId`
  names.
- **`versions`** — the protocol versions this server accepts, newest first.
- **`enabled`** — whether this server will actually federate.

The document is public. It contains an address the operator already publishes,
public keys, and a version list; there is nothing in it to protect.

### Retired keys stay published

A key that no longer signs anything is still listed, marked `"retired": true`.
This is not an oversight to be tidied up later — it is the point.

A peer holds events this server signed months ago. If a rotation removed the
old key from this document, every one of those events would become
unverifiable the moment the peer next refreshed: not _wrong_, but no longer
provable, which for a signed event log is the same thing. Publishing retired
keys means history keeps verifying across any number of rotations.

So: verify a signature against **any** published key, current or retired. Sign
with the current one only.

### A server that does not federate still answers

A server with federation disabled serves this document anyway, with
`"enabled": false`. Every other federated route refuses.

That is deliberate. A peer that gets a connection refused has learned nothing
it can act on — the server might be down, misconfigured, firewalled, or
deliberately not participating, and those need different responses from an
operator. A document saying `"enabled": false` is an answer somebody can read
in a log and act on.

## Verifying who a server is

**A server's identity is only worth what the domain serving it is worth.**

Anyone can serve a document claiming to be `tdc.example.org`. What makes the
claim mean something is that it was fetched _from_ `tdc.example.org` — the same
trust anchor that DNS and TLS already provide, and the reason discovery is a
fetch rather than an assertion a peer makes about itself over a connection it
already controls.

Concretely, before trusting a document:

1. Fetch `https://<domain>/.well-known/tdc-federation` over TLS.
2. Compare `canonicalAddress` in the response to `<domain>` you fetched from.
3. If they name different hosts, **refuse the peer entirely.** Exchange no
   content. This is impersonation, not a misconfiguration to work around.

Comparison is by host only:

- **Case is ignored.** `EXAMPLE.org` and `example.org` are the same server.
- **Ports are ignored.** A server reached on `:8443` is still that domain, and
  requiring a port match would break most self-hosted deployments.
- **Redirects are not followed.** A redirect would let one server answer for a
  domain it does not serve, which is exactly what step 3 exists to refuse.

## Caching, and what happens when a key rotates

Fetching a peer's keys before every signature check would make verification a
network operation. So cache the document — TDC's own implementation keeps it
for an hour.

That creates one problem worth stating: a peer rotates a key, and for up to an
hour you hold a document that does not list it. Signatures made with the new
key verify against nothing you have.

The answer is not a shorter TTL, which only makes every peer poll more often
on the chance that somebody rotated. It is:

> **On meeting a `keyId` you do not recognise, re-fetch the document before
> refusing the signature.**

An unrecognised key ID is what a rotation _looks like_ from the outside. Treat
it as a cache miss, not as an error. A peer that did the right thing — rotated
its key and published both halves — becomes verifiable again immediately, and
nothing polls.

Re-fetch on a genuine verification failure too: if the signature still does not
verify against the freshly fetched keys, then it is a real failure and refusing
is correct.

## Agreeing on a version

**This document describes `tdc/1`.**

Both servers advertise a list. The newest version present in both is the one
they use.

The **caller's own preference order decides**, not the peer's. This matters
more than it looks: if each side preferred the other's ordering, the same pair
of servers could settle on different versions depending on which one initiated,
and each would believe the other was speaking something else.

When the lists share nothing, refuse — and say so in terms naming both lists.
Two servers far enough apart in age genuinely cannot talk to each other; the
requirement is not that this never happens but that it fails legibly rather
than as a parse error several layers down that nobody can trace back to a
version mismatch.

## Signing a request

Every federated request except version negotiation carries an RFC 9421
signature. Version negotiation is exempt because a peer has to agree a version
before it can sign anything the other side knows how to read.

A signed request carries three headers:

```
TDC-Origin-Server: https://a.example.org
Content-Digest: sha-256=:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=:
Signature-Input: tdc=("@method" "@path" "@authority" "content-digest")
  ;created=1758553650;expires=1758553680;keyid="2026-07-key";alg="ed25519"
Signature: tdc=:base64signature:
```

- **`TDC-Origin-Server`** says whose keys to fetch. It is only a claim; the
  signature is what makes it worth anything.
- **`Content-Digest`** is SHA-256 of the body, in the RFC 9530 form, present
  even for an empty body — a covered component that is sometimes absent is one
  an attacker can remove.
- **`Signature-Input`** lists the covered components and the parameters.
- **`Signature`** is the Ed25519 signature over the signature base, base64.

### What must be covered

All four of `@method`, `@path`, `@authority` and `content-digest`. A receiver
MUST refuse a signature covering fewer, even though it verifies: a signature
over `@method` alone is cryptographically perfect and means nothing. What is
covered is the receiver's decision, not the sender's.

Each one refuses a specific attack: `@method` and `@path` stop the request
being turned into a different one, `@authority` stops it being replayed against
a different server, and `content-digest` stops the body being altered.

### `@authority` is not the same as the server's identity

This distinction costs people an afternoon, so it is worth stating plainly.

For **identity** — deciding which server this is — the port is irrelevant.
`tdc.example.org` on `:8443` is the same server as on `:443`.

For **`@authority`** — the string both sides must reproduce to build the same
signature base — the port is part of the value, normalised per RFC 9110
§4.2.3: host lowercased, default port for the scheme omitted, any other port
included.

```
https://example.org        -> example.org
https://example.org:443    -> example.org
https://example.org:8443   -> example.org:8443
```

Strip the port where it belongs and every signature verifies on a server
running on `:443` and fails on every deployment behind any other port.

### The signature base

One line per covered component, then the parameters, LF-separated:

```
"@method": POST
"@path": /api/federation/ping
"@authority": b.example.org:8443
"content-digest": sha-256=:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=:
"@signature-params": ("@method" "@path" "@authority" "content-digest");created=1758553650;expires=1758553680;keyid="2026-07-key";alg="ed25519"
```

The receiver rebuilds this from the request it actually received, using the
authority **it knows itself to be** rather than anything the sender supplied.
That is what makes the destination binding real: comparing the sender's claim
against itself would check nothing.

### Time

Every signature also carries a **`nonce`**, and it is not optional. A GET with
no body has nothing varying in it — same method, path, authority, empty digest
— so with only a timestamp to separate them, two such requests in the same
second sign identically and the second is refused as a replay of the first. A
server browsing a peer twice in a second is not an attacker.

`created` and `expires` are **integer seconds since the Unix epoch** — absolute
instants, with no timezone. This is not incidental. Two servers in different
zones must agree about when a signature stops being valid, and any
representation carrying a local offset, or derived from wall-clock fields, gets
this wrong in a way that works perfectly in one deployment and fails by hours
in another.

Signatures are valid for **30 seconds**. A receiver:

1. refuses anything past `expires`;
2. refuses anything whose `created` is more than **5 seconds** in the future;
3. refuses a signature it has already seen inside its window.

Step 2 is a choice this protocol makes, not something RFC 9421 requires — the
RFC defines no skew tolerance and leaves it to the application. Zero tolerance
would mean a peer whose clock is a second fast has every request refused, and
the failure presents as a signature problem when it is really an NTP problem.

Step 3 is what the window alone cannot do. A captured request replayed to the
same destination inside its window is valid by every other check; only
remembering it refuses it.

**If pings fail between two servers that look correctly configured, check the
clocks first.** A refusal naming an expiry that has already passed, or a
creation time in the future, is a clock fault rather than a key fault.

### Key rotation, and what it does not do

A signature naming a `keyId` the receiver does not hold is not immediately a
failure. Re-fetch the sender's discovery document once and try again: an
unrecognised key id is what a rotation looks like from the far side.

**There is no revocation.** Rotating is the only mechanism, and it is not
instant. A peer that fetched your document an hour ago holds the key you have
just rotated away from, and will keep accepting signatures made with it until
its cache expires — up to the TTL, which in this implementation is an hour.

So the honest statement of the exposure is: **a compromised key stays usable
against any given peer until that peer next fetches your document.** Publishing
a new key does not reach out to anybody; peers discover it when they look.

If a key is compromised, rotate immediately and then tell your peers' operators
to refresh, because that is the only thing that shortens the window. There is
no message you can send that makes their caches forget sooner, and none is
planned: a "forget this key" request that a peer is free to ignore would look
like revocation without being it, which is worse than having none.

Retired keys stay published for verification and must never sign again. Peers
keep accepting what they signed in the past, which is what keeps history
verifiable across rotations — a retired key and a compromised one are not the
same thing, and a compromised key should be rotated away from _and_ its
retirement understood as "this still verifies old events", not "this is safe".

## Replicating a room

A room belongs to exactly one server — its **home server** — which assigns every
event's position in the room's order. Other servers hold a replica.

### Addresses name the home server

A room's address is `slug@home-server`, and it is the same string on every
server holding the room. This matters more than it looks: the address is part
of what each event is signed over, so if each server computed its own version
of it, every server would reject every other's events as belonging to a room it
had never heard of.

The same goes for a user: `name@their-server`.

### Two endpoints

```
GET  /api/federation/rooms/{slug}/events?since={cursor}
POST /api/federation/rooms/{slug}/events
```

Both require a signed request. The peer is taken from the **verified origin of
that signature**, never from a header — otherwise any server could read any
room by naming a peer that participates in it.

`GET` serves events after a cursor, to a peer that participates in the room. A
peer with no part in the room receives nothing, reported the same way as a room
with no new events: telling the two apart would disclose which rooms exist.

`POST` delivers events. The response says how many were **newly stored**, which
is not how many were sent — a redelivery stores nothing, and saying otherwise
would imply it did something.

Both are capped at 200 events. A peer returning from a long outage asks
repeatedly; `"more": true` says another request would bring more.

### What a receiver checks before storing anything

Four checks, and all four run on **every event before any is stored**. One bad
event refuses the whole batch: storing the valid prefix of a delivery
containing a forgery leaves a room half-updated from something nobody should
have trusted.

1. **The signature verifies** against the claimed origin's published keys.
2. **The origin hosts the sender** — `sender`'s server half must equal
   `origin`. A server may speak for its own users and nobody else's, and a
   flawless signature over a lie about whose message this is passes check 1.
3. **The event names this room.**
4. **The origin is the room's home server.** A participating peer relaying its
   own account of a room, rather than passing on the home server's, is how two
   servers start to disagree.

### A receiver may refuse what its own policy forbids

A server is never obliged to store content its operator's policy forbids, even
from a peer it trusts entirely. A delivery containing such an event is refused
with `422`, naming the event so the sender knows which one.

Refusal is the only action available on arrival. Masking would rewrite a body
the signature covers, producing an event that verifies nowhere — including on
the next server the sender relays it to. So a receiving server refuses or
accepts; it does not alter.

### Sequences are never reassigned

A receiving server stores the sequence the home server assigned and orders by
it. Renumbering locally is the first step toward two servers deciding a room's
order independently, and there is no correct way to reconcile that afterwards.

### Redelivery is a no-op

Storage is idempotent on the event's content-derived id. A retry, an
overlapping catch-up, or a peer sending a batch twice writes nothing — this is
a uniqueness constraint in the database rather than a check a caller has to
remember.

### Catch-up is a pull

A server that was unreachable asks its home server for everything after the
highest sequence it holds, rather than waiting for the home server's retry
queue to walk through a backlog of individual pushes.

## A member on another server

A room's home server decides who is in it and what order things happened in.
It does **not** get to decide what its members said.

### Who signs what

When a user on server B posts into a room hosted on A:

1. **B builds and signs the event.** `origin` is B, `sender` is `user@b`.
   There is no sequence yet.
2. **B forwards it to A**, over a signed request.
3. **A verifies B's signature** against B's published keys, checks membership,
   assigns the sequence, and stores it — **without re-signing**.
4. **A relays it** to every other participating server, B's signature intact.

This split is the security property, and it is why `sequence` is deliberately
not part of what is signed: A can assign one without invalidating anything, so
**ordering is A's and authorship is B's**. A compromised home server can drop,
delay or reorder messages — which is inherent in one server holding the order —
but it cannot invent a message from a member it does not host, because it holds
none of their keys.

### Two questions, not one

A receiving server checks both:

- **Who wrote it** — `origin` must equal `sender`'s server, and the signature
  must verify against _that_ server's keys. Not the deliverer's: one batch can
  carry events written by several servers.
- **Who delivered it** — must be the room's home server, because only the home
  server assigns positions.

For a local member's message these are the same server; for a remote member's
they are not, and conflating them either blocks remote posting or lets any peer
inject events into any room.

### Membership events

A join or leave is the home server's own decision, so the home server signs it
as itself: `sender` is `server@its-host`, with the person named in the content.
An event claiming to be _from_ somebody on another server while signed by this
one is exactly the impersonation the checks above refuse — including when the
statement itself is true.

### Joining

```
POST /api/federation/rooms/{slug}/join    {"member": "user@b"}
POST /api/federation/rooms/{slug}/leave   {"member": "user@b"}
POST /api/federation/rooms/{slug}/post    {"event": {...}}
```

All three require a signed request, and **the member named must be on the
server that signed it**. A server may act for its own users and nobody else's.

The room's own rules apply to a remote join exactly as to a local one — public
rooms only, not archived. The joining server's rules are irrelevant: the room's
home server decides who is in its rooms, or membership means nothing.

## Files

A message's files travel as descriptions inside its signed content, and their
bytes are fetched separately, only when somebody opens one:

```json
"content": {
  "body": "",
  "attachments": [
    { "id": "6f1c…", "filename": "beach.png", "contentType": "image/png",
      "size": 482113, "sha256": "9b2f…", "width": 1600, "height": 1200, "caption": "" }
  ]
}
```

The `sha256` is the part that matters. It is signed by the author's server, so
bytes obtained from anywhere can be checked against it, and the server they came
through is trusted for availability and nothing else. `filename` and
`contentType` are the author's claims; a receiver decides the type again from
the bytes. A description whose `sha256` is not 64 lowercase hex characters, or
whose `size` is negative, is ignored; at most ten files are honoured per message.

```
GET /api/federation/rooms/{slug}/files/{sha256}
```

Signed like every other request, with an empty body. Answered with the raw
bytes (`application/octet-stream`, `Content-Length` set) only when the signing
peer participates in the room — it is in the room's peers — and a sent message
in that room carries a file with that digest. Anything else is **404**, the
same for "no such room", "not yours to read" and "never sent", so asking for
digests in turn learns nothing.

**Where to ask.** A replica asks the room's **home server**; the home server,
for a file one of its remote members sent, asks **that member's server** (the
event's `origin`). The home server is the one server every participant already
talks to, so this follows authorisation that exists rather than needing a
membership check no replica could make. A home server asked for a file it has
not copied yet copies it from the origin first, then answers.

**What a receiver checks** before storing a copy: it reads no more than the
described `size` plus one byte; the length must equal `size`; the SHA-256 must
equal `sha256`; the type is detected from the bytes and must pass the
receiver's own type limits and scanner; and the file must be within the
receiver's `files.federation_copy_max_bytes` and storage limit. A copy is made
once per server, however many of its members open it, and a deletion arriving
from the home server (`message.deleted` with `redacts`) removes it.

### Retry, and giving up

Delivery retries with exponential backoff and jitter (2s doubling to 5 minutes,
randomised between half and full). The jitter matters: without it every
delivery failing when a peer goes down retries in the same instant and arrives
as a thundering herd the moment it returns.

After **10 consecutive failures** — roughly forty minutes — the peer is
abandoned: delivery stops, the reason is recorded, and the operator is told.
It resumes only when they say so. A peer unreachable for a day is not coming
back on the next attempt.

A success resets the count. Consecutive failures are what matter; a peer that
fails once an hour is not one that has been down for an hour.

## Calls

A call between people on different servers is **held by one server**, its
_home_: the server of whoever rang, or, for a call in a room shared between
servers, the room's home server. The home server keeps the list of who is in
the call and relays negotiation, exactly as it does for its own people. Every
other server is a go-between for its own people: it authorises them, carries
their negotiation to the home server, and passes back what the home server
sends them. Media never travels through this protocol — WebRTC carries it,
between the people (a mesh) or to the home server's forwarding unit — and
each person's client gets its relay credentials from **its own** server.

Both servers must have calls enabled and `calls.federated` set to `true`. A
server with it off refuses every request below with `404`, and sends none.

All requests are `POST`, signed as above, with JSON bodies. In every request
that names a `member`, **the member must be on the server that signed it** —
a server acts for its own people and nobody else's — and a request naming
somebody else is refused with `403`.

### People

A person is named by their address, `name@their-server`, and also carries
`id`, the UUID their own server knows them by. The home server shows them in
the call by that same `id`, so that the person's own client, which knows itself
by it, recognises itself in the call's state. A home server refuses an `id`
that is already somebody else's — one of its own people, or another server's.

### Ringing somebody on another server

```
POST /api/federation/calls/ring
{
  "call": "4b1c…",                       // the call, as the home server names it
  "roomId": "9e0a…",                     // the call's room on the home server
  "to": "baraka@b.example",
  "from": { "address": "amina@a.example", "displayName": "Amina" }
}
```

Sent by the home server to the server of the person rung. That server decides,
by its own policy and the person's own settings, whether to ring them: whether
it takes calls from other servers at all (`calls.federated`), and whether the
person accepts calls from people on other servers. It answers **`202` whether
it rang or not**, so the caller learns nothing of either setting. Ringing is
limited at the home server as it is for local rings.

A server that rings its person records that this call, at this home server,
may now reach them. Only that home server, and only about that call, may
deliver to them afterwards.

### Joining, and what follows

```
POST /api/federation/calls/join
{ "call": "4b1c…", "member": "baraka@b.example", "id": "77d2…",
  "displayName": "Baraka", "audio": true, "video": false }

POST /api/federation/calls/join
{ "room": "general", "member": "baraka@b.example", "id": "77d2…", ... }
```

Sent by the person's server to the home server. The first form joins a call the
person was rung into; the second joins, or starts, the call in a shared room
(`room` is the room's slug on its home server), which the person must be a
member of. The home server applies its own rules — the call is open, the
participant limit, the room's — and answers `200` with the call's state:

```json
{
  "id": "4b1c…",
  "roomId": "9e0a…",
  "startedAt": "2026-10-24T09:00:00Z",
  "mode": "mesh",
  "presenter": "",
  "participants": [
    {
      "userId": "77d2…",
      "username": "baraka",
      "displayName": "Baraka",
      "address": "baraka@b.example",
      "audio": true,
      "video": false,
      "held": false,
      "screen": false,
      "joinedAt": "…"
    }
  ]
}
```

or `409` with the reason (the call is full, and why), or `404` (no such call,
or not one this person may join — the two are not told apart).

```
POST /api/federation/calls/view    { "room": "general", "member": "…" }        → 200 state, or 404
POST /api/federation/calls/media   { "call": "…", "member": "…", "audio": true, "video": true, "held": false } → 200 state
POST /api/federation/calls/leave   { "call": "…", "member": "…" }              → 204
POST /api/federation/calls/present { "call": "…", "member": "…" }              → 204
POST /api/federation/calls/signal  { "call": "…", "member": "…", "to": "…", "signal": { … } } → 202
```

- **`view`** reads the call in progress in a shared room, so a member can see
  there is one before joining.
- **`media`** says what the person is sending, as the web client's own `PATCH`
  does.
- **`present`** is sent every fifteen seconds while the person is connected to
  their own server. A home server takes out of its calls anybody from another
  server not heard of for a minute, which is how a server that went away stops
  holding a place in a call.
- **`signal`** carries one piece of WebRTC negotiation — `{"description": …}`
  or `{"candidate": …}` — to one participant, named by `to` (their `userId` in
  the call's state), or to the forwarding unit, named by the zero UUID
  `00000000-0000-0000-0000-000000000000` when the call's `mode` is `sfu`.
  Signals must be sent in order; a server sends one person's signals one at a
  time.

### What the home server sends back

```
POST /api/federation/calls/deliver
{ "call": "4b1c…", "to": "baraka@b.example",
  "type": "call.signal", "payload": { … } }
```

Sent by the home server to the person's server for anything the call sends
them. `type` is one of:

| Type              | Payload                                                                                      |
| ----------------- | -------------------------------------------------------------------------------------------- |
| `call.state`      | the call's state, as above; a state not listing the person means they are out                |
| `call.signal`     | `{ "call", "from", "signal" }`: negotiation from `from` (the zero UUID: the forwarding unit) |
| `call.ring_ended` | `{ "call" }`: stop ringing                                                                   |
| `call.declined`   | `{ "call", "userId", "displayName", "message" }`                                             |
| `call.reaction`   | `{ "call", "from", "kind", … }`                                                              |

The person's server accepts a delivery only from the server it recorded as
that call's home for that person, only while they were rung or are in it, and
only of these types; anything else is `404`. It answers `202` and passes the
payload to the person's devices unchanged, except that for a call in a shared
room it may rename `roomId` to its own copy of the room.

### What is not carried

Raising hands, presenting and recording are not yet carried between servers:
a person on another server sees them in the call's state but cannot do them.

## A project's stream

> **Status.** Projects are not yet shared between servers: nothing below is
> sent to a peer today. It is written down now because a server holding a copy
> of a project will rebuild the project from exactly this, and the event family
> is part of what `tdc/1` will carry.

A project is a room of kind `project`, sharing its identifier with the
project. Its log holds no messages: every change to the project and to every
issue in it is an event, signed and sequenced like any other (see
[Replicating a room](#replicating-a-room)), appended by the project's home
server in the same transaction as the change itself. People are named by
address, `name@server`. An event the server made by itself — a cycle reaching
its end, a baseline — has the server's bare name as its `sender`, with no `@`.

### The project event family

Identifiers are UUIDs as strings. Days are `YYYY-MM-DD` in the project's
calendar. Content may carry more fields than listed — names as they were, for
a reader of the history — and a receiver ignores what it does not use.

| Type                                                                       | Content a projection reads                                                                                                                                                     |
| -------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `project.created`                                                          | `key`, `name`, `description`, `visibility`, `timeZone`, `cyclePolicy`. The sender becomes the project's first administrator.                                                   |
| `project.updated`                                                          | One or more of `name`, `description`, `visibility`, `timeZone`, `cyclePolicy`, each `{previous, new}`.                                                                         |
| `project.member_added`                                                     | `member` (address), `role` (`admin` or `member`), `previous` (the role before, empty for somebody new).                                                                        |
| `project.member_removed`                                                   | `member`.                                                                                                                                                                      |
| `project.state_added`                                                      | `state`, `name`, `position`, `isCompletion`.                                                                                                                                   |
| `project.state_changed`                                                    | Either `state`, `name`, `isCompletion` (and `previous`), or `order`: every state, left to right.                                                                               |
| `project.state_removed`                                                    | `state`. Its issues were moved out first, each by its own `issue.state_changed`.                                                                                               |
| `project.label_added`                                                      | `label`, `name`, `colour` (`#rrggbb`).                                                                                                                                         |
| `project.label_changed`                                                    | `label`, `name`, `colour`, and `previousName`, `previousColour`.                                                                                                               |
| `project.label_removed`                                                    | `label`. It came off every issue first, each by its own `issue.unlabeled`.                                                                                                     |
| `project.priority_added`                                                   | `priority`, `name`, `rank` (1 is most urgent).                                                                                                                                 |
| `project.priority_changed`                                                 | Either `priority`, `name` (and `previousName`), or `order`: every level, most urgent first.                                                                                    |
| `project.priority_removed`                                                 | `priority`. Every issue at that level was cleared first, each by its own `issue.priority_changed`.                                                                             |
| `project.cycle_added`, `project.cycle_changed`                             | `cycle`, `name`, `startsOn`, `endsOn`.                                                                                                                                         |
| `project.cycle_removed`                                                    | `cycle`.                                                                                                                                                                       |
| `project.cycle_ended`                                                      | `cycle`, `policy`, `closedAt` (RFC 3339), and what became of each unfinished issue was recorded before it as `issue.cycle_changed` with `reason: "cycle_ended"`.               |
| `project.milestone_added`, `project.milestone_changed`                     | `milestone`, `name`, `description`, `targetDate` (a day or `null`).                                                                                                            |
| `project.milestone_removed`                                                | `milestone`.                                                                                                                                                                   |
| `project.baseline`                                                         | Every row the project held when it was appended. See below.                                                                                                                    |
| `issue.created`                                                            | `issue`, `key` (`WEB-12`, whose number is the issue's), `title`, `description`, `state`. The sender is its author.                                                             |
| `issue.state_changed`, `issue.title_changed`, `issue.description_changed`  | `issue`, `field`, `previous`, `new`.                                                                                                                                           |
| `issue.priority_changed`, `issue.cycle_changed`, `issue.milestone_changed` | `issue`, `field`, `previous`, `new` — an identifier, or `null` for none.                                                                                                       |
| `issue.deleted`                                                            | `issue`. Hidden, never removed; its number is never reused.                                                                                                                    |
| `issue.assigned`, `issue.unassigned`                                       | `issue`, `assignee` (address).                                                                                                                                                 |
| `issue.labeled`, `issue.unlabeled`                                         | `issue`, `label`.                                                                                                                                                              |
| `issue.related`, `issue.unrelated`                                         | `issue`, `kind` (`parent`, `child`, `blocks`, `blocked_by` — what this issue is to the other), `other`. Recorded twice, once from each issue; both name the same relationship. |

### Rebuilding a project from its stream

A projection applies the stream in sequence order, one event at a time, as a
function of nothing but the rows so far and the next event — no clock, no
other table. Applied to the same stream it gives the same rows wherever it
runs. The rules a second implementation needs:

- **A cycle's and a milestone's instants are worked out, never sent**: a
  cycle runs from the start of `startsOn` to the end of `endsOn` in the
  project's `timeZone`, a milestone is due at the end of its `targetDate`, and
  a `timeZone` change recomputes every one of them on the same days.
- **Removing a cycle or a milestone takes any issue still in it out of it**,
  as a database's cascade would; only a deleted issue can still be in one.
- **A `project.baseline` replaces everything.** Its content is the project's
  rows — the project, members by address, states, labels, priorities, cycles,
  milestones, issues with their labels, assignees, cycle and milestone, and
  relationships — as they stood when it was appended. Replay starts from the
  latest baseline in the stream; what came before it is already in it. A home
  server appends one, once, to a project made before every change was an
  event, so that its stream alone describes it from there on.
- **An event of a type the projection does not know stops it.** It does not
  skip the event: a projection that skipped what it did not understand would
  disagree with its source without saying so. The project stays as it was at
  the event before, and says why.

Not in the stream, and so never part of a projection: when each row was
written on the home server, saved views, who follows a discussion, and the
search index, which a server works out from the rows itself.

**Erasure is the exception.** When a person's data is erased, the content of
everything they sent is emptied — project events included — and their account,
with its memberships and assignments, is removed without events recording it.
An erased person's events cannot be applied, and a projection reaching one
stops as for an unknown type. This conflict between erasure and a complete
history is not yet resolved; it is recorded in the project federation design.

## Whether a server will deal with you at all

Before a signature is examined, the receiving server decides whether it deals
with your server at all. Three modes — `open`, `allowlist`, `blocklist` — and a
block is honoured under every one of them.

A refusal here arrives as `401` before any cryptography runs, so an
implementation seeing every request refused regardless of signature should
check whether it has been blocked or left off an allowlist rather than looking
for a signing fault.

Servers are identified by **hostname**, without the port: `peer.example.org` on
`:8443` and on `:443` are the same server to these lists. Note this is the
opposite of `@authority` above, which must carry the port — identity and
addressing are different questions.

## Browsing what a server publishes

```
GET /api/federation/rooms
```

Signed like everything else, which is the point: the answer depends on which
server is asking, and that must be a server the responder has identified rather
than one a request claims to be.

**Two checks in series, neither substituting for the other:**

1. **Server visibility** — may this peer see anything here at all? A `limited`
   server answers only peers its operator listed.
2. **Room visibility** — which rooms are public? Private and archived rooms are
   excluded by the query itself, so "a private room on a public server remains
   undiscoverable" holds whether or not a caller remembers to filter.

A limited server that has not listed the asking peer returns an **empty list,
not a refusal**. A distinct refusal would itself disclose that there is
something to be refused, which is what limited visibility exists to avoid — so
"nothing for you" and "nothing here" are deliberately the same answer.

### The header that used to matter

`TDC-Peer-Server` exists on unsigned client-facing routes and is a claim
anybody can make. On signed federated routes it is ignored entirely: the peer
identity comes from the signature, verified before any handler runs. What the
header still protects on an unsigned route is correspondingly weak — it keeps a
limited server out of casual listings, not away from a determined caller.

## What a version bump requires

`tdc/1` is not frozen while federation is still being built. Once it is, these
are the rules.

**A new version is required when** a receiving server built against the old one
would misunderstand something rather than merely miss it: a change to the
canonical form or to how identifiers are derived, a new required field, a
changed meaning for an existing field, or a new check a sender must satisfy.

**A new version is not required when** an old implementation can ignore the
change safely: a new event type, a new optional field in `content`, a new
endpoint, or a new setting. Unknown event types are stored and relayed
unchanged precisely so that adding one is not a network-wide breaking change.

**When a version is added**, servers advertise both for at least one release
cycle, this document describes both, and the older one is removed only after
the network has had time to move. A server supporting exactly one version is a
server that cannot talk to anybody mid-upgrade.

The negotiation above is what makes this survivable: two servers pick the
newest they share, so a server that has learned a new version keeps talking to
one that has not.

## Reaching private networks

TDC refuses to open outbound connections to private address ranges by default,
checked at connect time, after DNS resolution — which closes the rebinding
window that a resolve-then-connect check leaves open.

Federation between two servers on one private network is a legitimate
deployment that this would otherwise make impossible, so an operator can permit
it:

```
federation.allow_private_addresses = true
```

It is a separate decision from the general outbound guard because the inputs
differ in kind. A link to unfurl is named by whoever typed it; a federation
peer is named by the operator. Permitting the second does not permit the first.

## Settings

| Setting                              | Default | What it does                                                                                                                          |
| ------------------------------------ | ------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| `federation.enabled`                 | `false` | Whether this server federates at all. Off means the discovery document still answers, saying so; every other federated route refuses. |
| `federation.allow_private_addresses` | `false` | Whether peers on private network addresses may be reached.                                                                            |
| `calls.federated`                    | `false` | Whether calls cross servers here: people on connected servers ringing and calling people here, and people here them. Needs calls on.  |

Federation is off until an operator turns it on. A server joins a network
because somebody chose that, never because it was the default.
