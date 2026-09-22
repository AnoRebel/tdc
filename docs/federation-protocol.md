# The TDC federation protocol

This describes how two TDC servers find each other, prove who they are, and
agree on a language to speak. It is written so that somebody with no access to
this source tree could implement the other side of it.

Federation is optional. A server with it switched off is a complete TDC server;
nothing on this page is needed to run one.

> **Status.** This document covers discovery and identity — what a server
> publishes and how a peer checks it. Request signing, room federation and
> message propagation are being built on top of it and will be described here
> as they land. Version `tdc/1` is not frozen until they are.

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
