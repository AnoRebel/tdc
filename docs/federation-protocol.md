# The TDC federation protocol

This describes how two TDC servers find each other, prove who they are, and
agree on a language to speak. It is written so that somebody with no access to
this source tree could implement the other side of it.

Federation is optional. A server with it switched off is a complete TDC server;
nothing on this page is needed to run one.

> **Status.** This document covers discovery, identity, request signing, room
> replication, and remote membership and posting. Cross-server browsing and
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

Federation is off until an operator turns it on. A server joins a network
because somebody chose that, never because it was the default.
