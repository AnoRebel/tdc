# Privacy policy

<!-- TEMPLATE. Read the note below before publishing this. -->

## A note to the operator

This is a template, not a finished policy. TDC is self-hosted: there is no
central service and no company behind it. Whoever runs a server decides what it
does, holds its database, and is the data controller for everyone on it. That is
you, and this document is yours to adapt.

Every place you must fill something in is written as `[LIKE THIS]`. Search for
`[` before publishing; anything left is a hole. Sections marked **Only if
enabled** describe optional features. If you have not turned one on, delete the
section rather than leaving it — a policy that describes things your server does
not do is as misleading as one that omits things it does.

**This is not legal advice.** What you are obliged to tell people, and what you
are obliged to let them do about it, varies by where you are and where they are.
For a server run in Tanzania for a Tanzanian community, the Personal Data
Protection Act 2022 and its regulations are the law to read, and registration
with the Personal Data Protection Commission may be required of you before you
process anything. If anyone using your server is in the European Union or the
United Kingdom, the GDPR may reach you regardless of where the machine is. Other
jurisdictions have their own rules.

Nothing here certifies compliance with any of that. It is a starting point,
written to be accurate about what the software actually does, so that a lawyer
you consult — or a member who asks — is reading something true.

The technical claims in this document were written against the source. If you
have modified your copy of TDC, check that they are still true of yours.

---

**Server:** `[SERVER NAME]` at `[https://chat.example.org]`
**Operator:** `[OPERATOR NAME, AND LEGAL ENTITY IF THERE IS ONE]`
**Contact:** `[EMAIL ADDRESS FOR PRIVACY QUESTIONS]`
**In effect from:** `[DATE]`

## Who holds your data

`[OPERATOR NAME]` runs this server and is responsible for the personal data on
it. The data lives on `[WHERE — a machine in a named country, a named hosting
provider and region]`.

TDC is open-source software. The people who wrote it do not run this server, do
not have access to it, and receive nothing from it. Questions about your data on
this server go to the operator above and nowhere else.

## What the software collects

This section describes what TDC stores by design. It is written from the
database schema, so it is a complete account of the personal data the software
itself keeps.

### Your account

A username, and the lower-cased form of it used for lookups. Optionally a
display name, an email address, and whether that address has been verified.

If you have been suspended, the record keeps when, until when, who did it, and
the note they wrote.

Some settings you choose: interface appearance, interface language, whether you
send typing indication, whether links you post are fetched for previews, whether
push notifications may carry message text, who may open a direct conversation
with you, and which introductions you have already been shown.

### How you sign in

One record per way you can prove who you are. A password is stored only as an
argon2id hash. A passkey is stored as its public key and a signature counter.
A sign-in through another service is stored as that service's identifier for
you. **No recoverable secret is stored for any of them.** Each record carries a
label you chose, when it was created, and when it was last used.

If you ask to reset your password, this server stores a hash of the link it
mailed you, the address it went to, when it expires, and the IP address the
request came from — the last so that an attempt on your account is something an
administrator can see afterwards. The link itself exists only in that message.

If you turn on two-step sign-in, this server stores the shared secret your
authenticator app uses — it has to, because every code is derived from it — and
the hashes of your recovery codes, never the codes themselves. It also records
which thirty-second window your last accepted code came from, so the same code
cannot be used twice.

If you enrol a passkey, your account also gains a WebAuthn handle: a random
value your authenticator stores so it can offer this account when you sign in
without typing anything. It is deliberately unrelated to your account id or your
username, because the specification asks for a value that cannot be correlated
with anything else about you.

### Your sessions

Every device you are signed in on has a session, holding the browser or client
you used (the user agent string), your IP address, when it was created, when it
was last seen, and when it expires. The session token itself is stored only as a
SHA-256 hash, so a copy of the database does not hand anyone a working session.

You can see all of your sessions in the application and end any of them.

By default a session lasts `[720 hours / 30 days]` without being renewed.
`[ADJUST IF YOU HAVE CHANGED server.session_lifetime.]`

### What you write

Messages are stored in an append-only log, each carrying its content, who sent
it, which conversation it belongs to, when, and a signature. Alongside them:
reactions, pinned messages, read positions, threads, scheduled messages you have
not sent yet, and drafts of the conversations you have arranged.

Your sender address is stored on each message next to your account identifier.
That is deliberate: it means a message stays readable after an account is gone.

Messages are indexed for search in a separate table holding the message text,
who sent it, which conversation, and when.

### Files you upload

The file itself, plus its detected content type, its size, the filename you
gave, any caption you wrote, who uploaded it, which conversation it was for, and
whether it has been marked sensitive and by whom.

Files are stored `[on the server's own disk / in an S3-compatible object store
at PROVIDER, REGION]`.

### Presence and activity

Whether you are currently connected, how many connections you hold, the status
you have chosen and any text with it, whether you are appearing offline, and
when you were last seen. Typing indicators, which expire on their own.

### Notifications

What you have been notified about, why, and whether you have read it — kept on
the server so that reading something on your phone stops it showing on your
laptop.

### The audit log

Security and administration actions are recorded: who did what, to what, when,
from which IP address, and with which user agent. This includes signing in,
moderator actions, and changes to the server's settings.

The audit log references content by identifier rather than copying it, so it
does not become a second store of your messages. It is append-only — the
application has no path to edit or delete an entry.

By default audit entries are kept indefinitely, because a log that quietly
discards its own history cannot answer questions asked later. `[STATE YOUR
audit.retention_days SETTING IF YOU HAVE SET ONE.]`

### Moderation records

Reports about content — including a copy of what was reported, who reported it,
and who it was about — are kept after the reported message is deleted. That is
the point of them: removing something should not erase the report about it.

Channel bans, posting restrictions, and suspensions are each recorded with who
imposed them, why, and when they lift.

If you block another person, that is recorded. **The person you blocked is never
told**, by the interface or by the API.

### Invitations

If you were invited, the invitation record holds who created it, when, how many
times it may be used, when it expires, and — where an invitation was bound to
one person — the email address it was bound to. The invitation's secret is
stored only as a hash. A record of who accepted which invitation is kept.

## What the software does not collect

**There is no analytics and no telemetry.** TDC contains no analytics library,
no crash reporter, no usage counter, and no code that reports anything to its
authors or to anyone else. No part of the software calls home. This was checked
against the source, not assumed.

The server does not track you across other websites, does not set advertising
cookies, and does not build a profile of you for any purpose other than
operating the service you are using.

## Cookies

One cookie: your session. It is HttpOnly, so scripts on the page cannot read it,
and `SameSite=Lax`, so it is not sent on cross-site requests. On a server served
over HTTPS it is also marked Secure.

It exists so you stay signed in. There are no analytics or advertising cookies,
because there is no analytics and no advertising.

## What leaves this server

By default, almost nothing. Every item below is a feature the operator chose to
switch on, and each is off or avoidable unless they did.

**Only if enabled — link previews.** When someone posts a link, this server
fetches that page itself to read its title and description. The site being
linked to therefore sees a request from **this server's IP address**, not from
the person who posted or read the link. No message content and no account
information is sent — just an ordinary request for the page. The server does not
fetch the preview image; your client decides whether to load that, and if it
does, the image's host sees your browser.

You can turn off link fetching for links you post, in your own settings. The
operator can turn it off for everyone.

`[This server has link previews ON / OFF.]`

**Only if enabled — GIF search.** If the operator has configured a GIF library
(Tenor or Giphy), your search terms are sent to that provider. They are proxied
through this server, so the provider sees one server rather than your address
and a request per keystroke. Your search terms still reach the provider, and
their own privacy policy governs what they do with them.

`[This server uses GIF search from PROVIDER / does not use GIF search.]`

**Only if enabled — push notifications.** If you allow notifications, your
browser gives this server an endpoint at a push service chosen by your browser
vendor (Google, Mozilla, Apple, Microsoft — whoever makes your browser). We
store that endpoint, the public encryption keys your browser generated, and the
user agent that subscribed, so you can recognise the device later.

Notification contents are encrypted to keys whose private halves never leave
your browser. This server cannot read a notification it sent, and neither can
the push service — but the push service does see that a notification was sent to
your device, and when.

You can choose to keep message text out of notifications entirely, in your
settings. Then a notification says only that something arrived.

`[This server has push notifications ON / OFF.]`

**Only if enabled — object storage.** If the operator stores attachments in an
S3-compatible object store rather than on the server's own disk, your uploaded
files are held by that provider.

`[Attachments are stored on this server's disk / at PROVIDER in REGION.]`

**Only if enabled — sign-in through another service.** If you sign in with an
external provider, this server sends that provider a request to authenticate
you, and receives back its identifier for you, and usually an email address and
whether the provider says it has verified it. That provider learns that you
signed in to this server, and when.

We store the provider's identifier for you and the email it asserted. We do not
store any access token the provider issued beyond the moment it is used.

`[This server offers sign-in through: PROVIDERS / only passwords.]`

**Only if enabled — malware scanning.** If the operator has configured a virus
scanner, uploaded files are passed to it. `[Say whether yours runs on the same
machine or elsewhere. If elsewhere, files leave this server.]`

**Only if enabled — media processing sidecar.** If configured, uploaded images
are sent to a separate process for preview generation. `[Say whether that
process runs on the same machine.]`

Nothing on this list is required for TDC to work. A server with all of it
switched off makes no outbound request on your behalf at all.

## How long things are kept

**Messages** are kept `[indefinitely, unless a channel or the server sets a
retention period — STATE YOURS]`. When a retention period removes a message, its
content is blanked and the record of the message remains, so the conversation
around it stays intact and verifiable.

**Messages with a lifetime.** You can give an individual message a lifetime: a
duration after sending, expiry once everyone has read it, or a window that
starts when each person first reads it. When it expires the content is blanked
the same way, and any files it carried are deleted.

**Attachments** live as long as the message carrying them. A file uploaded but
never sent is reclaimed.

**Sessions** expire at their stated time, or when you revoke them.

**Audit entries** are kept `[indefinitely / for N days]`.

**Reports and moderation records** are kept after the content they concern is
gone, because that is what makes them useful.

**Sign-in records in progress** — the short-lived rows that hold a sign-in
between leaving for a provider and coming back — expire within minutes and are
deleted the moment they are used.

**Typing indicators** expire on their own within seconds.

## Your rights

Depending on where you live, you may have the right to see what data is held
about you, to correct it, to have it deleted, to object to how it is used, and
to complain to a regulator.

In Tanzania, the Personal Data Protection Act 2022 gives data subjects rights of
this kind, and the Personal Data Protection Commission is the body to complain
to. In the EU and UK, the GDPR does.

`[STATE HOW SOMEONE EXERCISES THESE RIGHTS WITH YOU: the address to write to,
how long you will take to reply, and what you will ask for to confirm who they
are.]`

**Be honest here about one limitation.** The software as shipped has no
self-service account deletion and no data export. There is no button a member
can press. Honouring a request to see or delete someone's data is something the
operator does by hand, against the database. If you cannot or will not do that,
say so plainly rather than promising a right you will not deliver.

There is also no password reset built in. An account that loses its password
needs you.

**If you ask to be erased.** The operator can run an erasure, and this is
exactly what it does — worth reading before anybody promises more than it
delivers.

Removed: your account and everything attached to it (credentials, sessions, push
subscriptions, notifications, memberships, reactions, blocks, preferences); the
content of every message you sent; the search index's copies of them; the copies
any moderation report kept of your words; and your name from the audit log.

Kept, and why:

- **The messages themselves, emptied.** Removing the rows would break the
  ordering of every conversation you took part in, which is other people's data
  as much as yours. What is left is a placeholder where a message was.
- **That an account existed**, shown as `erased@` this server. A conversation
  where one participant's messages have no author at all reads as broken rather
  than as erased.
- **Audit entries**, with your name replaced and the address and browser
  removed. A log with holes in it is not a log.
- **Reports other people made** about your messages, with the copied content
  removed. The complaint belongs to whoever made it.

One consequence worth stating: an erased message can no longer be
cryptographically verified. The sender's address is part of what this server
signed, so removing it means the message cannot prove itself any more. That is
deliberate — your right to be erased outweighs this server's ability to prove
who said something you are no longer recorded as having said.

**What deletion can and cannot do.** Deleting an account removes the account and
the things attached to it. It does not remove messages the person sent: those
stay, with their sender address, because the message log is append-only and
removing entries from it would break the conversations other people are part of.
A member should understand this before they decide what to post. `[If you intend
to blank a departing member's message content by hand, say so. If not, do not
imply it.]`

## Who on this server can see what

**Other members** see what you post in the channels you share, your username and
display name, your presence and status, and your reactions.

**Moderators and administrators** see reports, the moderation queue, the audit
log, account details including email addresses, and session metadata.

**Administrators cannot read your direct messages.** This is enforced by the
server, not by policy: the ordinary endpoints will not return another person's
direct conversations to an administrator.

An administrator with access to the server's database or filesystem can read
anything stored there, including message content. No software prevents that. It
is worth saying plainly because it is true of every self-hosted service, and a
policy that implied otherwise would be false. `[NAME WHO HAS THAT ACCESS ON THIS
SERVER, AND ANY RULE YOU HOLD YOURSELF TO ABOUT USING IT.]`

## Security

Passwords are stored as argon2id hashes. Session tokens and invitation secrets
are stored only as hashes. Push notification content is encrypted to keys this
server does not hold.

`[STATE WHAT YOU DO: whether the server is served over HTTPS, whether the
database is encrypted at rest, how backups are stored and who can reach them,
and who has administrative access to the machine.]`

No system is perfectly secure. If there is a breach affecting personal data,
`[SAY WHAT YOU WILL DO AND HOW QUICKLY — Tanzanian and EU law both impose
notification duties with deadlines]`.

## Children

`[STATE YOUR MINIMUM AGE, IF ANY, AND HOW YOU HANDLE IT. Data protection law in
many places treats children's data differently, and consent given by a child may
not be valid.]`

## Changes to this policy

`[SAY HOW YOU WILL TELL PEOPLE WHEN THIS CHANGES — an announcement channel, an
email, a notice at sign-in — AND HOW MUCH NOTICE THEY GET.]`

## Contact

`[EMAIL ADDRESS]`

`[POSTAL ADDRESS, IF YOU ARE OBLIGED TO PUBLISH ONE]`
