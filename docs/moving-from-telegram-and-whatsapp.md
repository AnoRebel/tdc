# Moving from Telegram and WhatsApp

A community that already talks somewhere is being asked to talk somewhere else,
and that is a harder thing than it sounds. This describes what the move
involves: what an organiser decides before anybody is invited, how the shape of
a Telegram or WhatsApp community maps onto what TDC actually has, and — at
length, because it is the part that decides whether the move sticks — what is
genuinely better and what is genuinely worse.

Nothing here is aspirational. Where TDC does not have something, it says so.

## Before anybody is invited

### Decide the registration policy

Who may create an account is a server setting, `registration.policy`, and it
takes one of four values. It changes while the server runs, so it is not a
decision that has to be right for ever — but it should be deliberate before the
first invitation goes out, because the wrong default is discovered by strangers.

| Policy       | Who may register                                     |
| ------------ | ---------------------------------------------------- |
| `open`       | Anybody who finds the server                         |
| `invitation` | Only somebody holding an invitation                  |
| `closed`     | Nobody; an administrator creates every account       |
| `domain`     | Anybody with an email address in a domain you listed |

The shipped default is `open`. For a community moving off Telegram, `open` is
usually wrong on day one and sometimes right later: a Telegram group with a join
link has a membership somebody curated, and reproducing that on a server anybody
can register on discards the curation. `invitation` reproduces it.

`domain` suits a company or a university chapter, and suits a general community
badly — members do not share an email domain.

`closed` is the strictest and costs the most: every account is an administrator's
job, including every correction of a typo'd username.

Most communities want `invitation` for the move and can loosen it afterwards
once they have seen what turns up.

### Understand how little a new account can do

TDC has no fixed account types. Authority is a set of named permissions
collected into roles, and roles are assigned to people. The permissions that
matter when planning a move:

- `channel.create` — creating channels.
- `message.pin` — pinning speaks for the channel rather than for the person, so
  it takes a permission where reading what is pinned does not.
- `channel.invite` and `user.invite` — admitting somebody to a channel, and to
  the server. Being a member of a channel does not let you invite to it.
- `moderation.review` and `moderation.restrict` — handling reports, and
  restricting somebody from posting.
- `server.configure` and `server.administer` — changing policy and granting
  permissions. These are the accounts worth attacking.

**Two roles exist without you doing anything.** `Administrator`, carrying
`server.administer`, goes to the first account during first-run setup.
`Member` goes to every account as it registers, and grants two things: creating
a channel, and inviting somebody to one.

That default is a choice, not a fact. If your community would rather channels
were an organiser's business, take `channel.create` away from `Member` — one
call, and nothing else about the server changes:

```
PUT /api/admin/roles/{member-role-id}/permissions
{ "permission": "channel.create", "granted": false }
```

Roles beyond those two are yours to make. A `Moderator` who can suspend accounts
but not reconfigure the server is a role, two granted permissions and an
assignment — which is worth setting up before the move, so that the people who
moderated your Telegram group are moderating here on the first day rather than
asking you to do it for them.

Note what a role does **not** cover. Thirteen of the twenty-three catalogued
permissions are enforced anywhere; the rest are names waiting for the features
they belong to. `channel.post` is one of the unenforced ones, so posting is
governed by membership, bans and posting restrictions rather than by a role — a
read-only announcement channel is still the awkward case described below.

Decide who holds what before the move rather than during it. An organiser who
was an admin in Telegram is not automatically anything here.

### Decide whether a second factor is required

`auth.second_factor.policy` is `optional` by default; `administrators` requires
one of anybody who can configure the server, and `everybody` of everyone.

`administrators` is the setting most servers want. Requiring one of a whole
community that has not been warned is a community locked out on a Tuesday
morning, and there is no password reset to rescue them — see below.

### Understand that there is no password reset

This is the single most important thing to know before inviting anybody.

TDC as shipped has no account recovery: no reset email, no self-service recovery
for a forgotten password. An account that loses its password needs an
administrator to intervene against the database. Somebody who loses both their
authenticator app and their recovery codes needs the same.

Tell people this when you invite them, in the same message as the invitation.
"Choose a password you will keep, and keep it somewhere" is advice that costs
nothing to give and a great deal to omit.

## How accounts get created

### Invitations

An invitation is a link, a short code, or both — the code exists for reading
aloud, which matters when the person you are inviting is standing in front of
you or on a phone call.

Each one can carry an expiry, a limit on how many times it may be used, and a
binding to one email address so that forwarding it achieves nothing. Revoking
one takes effect immediately, which is the only withdrawal that means anything:
somebody already holding a link cannot be un-sent it.

An invitation admits somebody to the server, or to a single channel and nothing
else. They are separate things.

Two behaviours worth knowing before you field the support questions:

- **A refusal never says why.** Expired, used up, revoked, meant for somebody
  else and never having existed all produce the same message, because a response
  that distinguished them would tell somebody guessing codes when they had
  guessed right. When a member says "it doesn't work", the answer is to issue a
  new one rather than to diagnose.
- **A pasted invitation link previews as an invitation to the server and never
  names the channel**, and it previews identically whether it is valid or
  invented. Posting one into your old Telegram group shows everybody in that
  group an invitation, which is usually what you want, and tells nobody which
  channel it is for.

### Bulk invitations are one at a time

There is no bulk import of members, no "invite everybody in this list", and no
way to pre-create accounts from a spreadsheet. For a community of a few hundred
people, the realistic approach is one multi-use invitation posted where the
community already is, with an expiry, rather than individual invitations issued
to each person.

That is a trade: a multi-use link is a link anybody who sees it can use. Bind it
to a channel and give it an expiry rather than leaving one open for ever.

### What a member does

They open the server's address in a browser, register with a username and a
password, entering the invitation if your policy requires one, and they are in.
There is no app to find in a store and no account with anybody else to make.

If your operator has configured them, signing in through another service —
GitHub, a self-hosted Forgejo, Keycloak — and passkeys are both available. Both
are the operator's setup rather than the member's, and neither is on by default.

## Mapping what you have onto what TDC has

### The short version

| On Telegram or WhatsApp    | On TDC                                          |
| -------------------------- | ----------------------------------------------- |
| Group                      | Channel — public or private                     |
| Forum topic                | A channel, or a thread on a message             |
| Broadcast channel          | A channel where only organisers may post        |
| Pinned message             | Pinned message, listed together for the channel |
| Group admin                | A role carrying the relevant permissions        |
| Direct message             | Direct message                                  |
| Group of 3+ people in a DM | Not available in either client yet              |
| Voice or video call        | Not available at all                            |
| Disappearing messages      | Disposable chats, and per-message lifetimes     |
| Bots                       | The HTTP API; no bot framework                  |

### Channels

A channel is a named conversation, public or private. Public means anybody on
the server can find it and join; private means only its members can see it — and
genuinely see it, not merely open it. Somebody who is not a member cannot tell a
private channel they are not allowed into from one that does not exist, anywhere
in the application or the API.

That is a real improvement over a Telegram private group, which announces its
existence to anybody who gets a link.

Channels can be archived, which keeps every message readable and refuses new
ones, and restored again. This is the right thing to do with a channel whose
purpose has passed — better than deleting it, because the history survives.

### Topics and threads

A Telegram forum with topics maps onto TDC in one of two ways, and the choice
matters.

**Make each topic a channel** when the topics are durable — `#jobs`, `#android`,
`#events`. Channels are what people join, mute by leaving, and search within.

**Use threads** when the tangent is a tangent. Any message can become a thread,
and replies live in a panel beside the conversation rather than in the main
timeline, so a long argument does not bury everything else. The conversation
shows how many replies each thread has.

A thread inherits its channel's permissions and visibility, so there is no way
to make a thread more open than the channel around it.

The mistake to avoid is making a channel per topic when the topics are really
threads. Thirty channels with four messages each is worse than three channels
with threads in them, and this is the most common thing a community gets wrong
in the first month.

### Announcement channels are moderation, not a channel type

TDC has three kinds of room — a channel, a direct conversation, and a disposable
chat. There is no read-only or broadcast channel type.

Nor is posting permission-controlled. There is a `channel.post` permission in
the catalogue, but the endpoint that accepts a message does not consult it: what
actually stops somebody posting is not being a member, being banned from that
channel, being restricted from posting, having the account suspended, or the
channel being archived.

So an announcement channel is built out of moderation instead: everybody is a
member and reads it, and the people who should not post are restricted from
posting in it. Restrictions are per-person and per-channel, they can be temporary
or permanent, and they expire by themselves.

**Be clear-eyed about what this costs.** For a community of two hundred people
where three should be able to post, that is a restriction applied to a hundred
and ninety-seven people, and to each new member as they arrive. There is no
single switch that makes a channel read-only. If a broadcast channel is central
to how your community works today, this is the largest piece of friction in the
move, and it is worth knowing before you promise anybody an announcements
channel.

A workable alternative for many communities: keep the announcement channel small
and private with only the organisers in it, and let the rest of the community
read announcements pinned in the main channel.

### Pinned messages

Pinning works, and pinned messages belong to the channel rather than to the
person who pinned them — they are listed together, and pinning takes a permission
where reading what is pinned does not.

This is the natural home for what a WhatsApp group keeps in its description and
a Telegram group keeps pinned: the rules, the code of conduct, the link to the
next meetup.

Pinning a _conversation_ to the top of your own list is a different thing, and
that one exists on the server but has no control in either client yet.

### Admins

There is no "admin" checkbox. Authority is a role carrying named permissions,
and a moderator who can remove messages and restrict posting is a different role
from an administrator who can change registration policy — keeping them separate
is the point of the system.

As noted above, the system is ahead of the tooling: roles are created and
assigned in the database rather than through the application, and only the
`Administrator` role exists without somebody making one. For the first months of
a move, expect your moderators to be administrators in practice, which is more
authority than they need and worth acknowledging rather than glossing over.

Everything a moderator does — removing a message, removing a channel, suspending
an account, restricting somebody, banning them from one channel — is recorded in
the audit log with its reason and its duration, and all of it is reversible.
Neither Telegram nor WhatsApp gives you that record.

### Direct messages

One-to-one direct messages work, and administrators cannot read them. Not
"should not" — an administrator using the ordinary endpoints cannot retrieve
other people's direct conversations, and that is enforced by the server.

Each person controls who may open a conversation with them: anyone, only people
who share a channel, or nobody.

**Group direct messages are not available.** The server holds a conversation
between several people and the API accepts one, but no control in either client
opens one. A WhatsApp group of five friends who are not a channel has no home
here yet; make a private channel instead.

## What to expect

This is the section to read to the sceptics in the community, and the one to be
honest in. A guide that oversells is one nobody trusts the second time.

### What is lost, and is not coming back in the move

**No message history comes with you.** There is no import from Telegram, no
import from WhatsApp, no bridge, and no bot that copies the old conversation in.
Nothing migrates. Years of scrollback — the answer somebody gave about tax
registration in 2023, the link to the venue, the argument that settled a
convention — stays where it is. The new server starts empty on its first day.

This is the largest single cost of the move and the one most likely to be
underestimated. Plan for it: see the cut-over section below.

**No voice or video calls.** Not reduced, not lower-quality — absent. There is no
calling code in the server at all. A community that runs voice meetings on
Telegram or WhatsApp keeps running them there, or moves them to Jitsi, Meet or a
phone, and TDC carries the text around them.

**No account recovery.** Covered above; it belongs in this list too. A forgotten
password is an administrator's problem, every time.

**No account deletion and no data export — for anybody.** There is no button a
member can press to leave and take their data or to erase it, and there is no
administrator button either. The strongest action the application offers against
an account is suspension, which invalidates its sessions and leaves everything
else in place. Actually deleting an account is a database operation, and
producing a copy of somebody's data is another one. If your community has members
who will ask — and under Tanzania's Personal Data Protection Act 2022 they have
the right to — decide who will do that work, by hand, before you promise it.

**Deleting an account does not delete its messages.** Even done directly in the
database, the messages stay: the sender identifier is cleared but the sender
address is kept alongside it, because the message log is append-only and removing
entries would break the conversations other people were part of. A member should
understand that before they decide what to post. It is not what people expect
from "delete my account" in a consumer app, and saying so late is worse than
saying so now.

**No message formatting.** No bold, no italic, no lists, and no code blocks.
Message text is plain text, with links, addresses and phone numbers detected and
made clickable. For a developer community this is a real loss: a code snippet
pasted into a message is a wall of unformatted text. Markdown _is_ rendered for
attached text files, so a snippet attached as a file arrives looking right —
which is the workaround, and it is a worse workaround than a code fence.

**No federation.** Servers do not exchange messages. Your community's
conversations are with people on your own server and nowhere else. The groundwork
exists — every address names a home server and every event is signed — but
nothing crosses a server boundary today.

**No native mobile app in any store.** The mobile experience is the web
application installed to the home screen: its own icon, no browser furniture, and
notifications with the browser closed where the operator has configured them. It
is a good progressive web app, and it is not an app anybody will find by
searching Play Store for it. Every member has to be given a link and told to
install from the browser menu, which is an extra step that some people will not
complete without help.

**The interface is English.** Kiswahili translations are written and ship with
the server, but the interface does not read from them yet — the text on screen is
English whichever language is chosen.

**No bots the way Telegram has bots.** There is an HTTP API and a live connection
that anybody can write against, which is more than WhatsApp offers, and there is
no bot framework, no BotFather equivalent and no gallery of existing bots to
adopt. Whatever your community relies on a Telegram bot for today, somebody will
have to write.

**Somebody is now responsible for the server.** This is the one that is easy to
agree to and hard to live with. If the machine runs out of disk, or Postgres
stops, or the TLS certificate expires, or the host has an outage, the community
cannot reach each other at all — and the usual channel for telling everybody
what has happened is the thing that is down. Telegram's operators do this work
invisibly and for free. Someone in your community now does it, and needs a
second person who can do it when they are unreachable.

Backups are part of that responsibility and are not automatic. The database and
the attachments want backing up on different schedules, and a backup nobody has
restored is a belief rather than a backup — restore one into a throwaway stack
and read a message from it before you need it for real.

### What is genuinely better

**A private channel is actually invisible.** A refusal that distinguished "not
allowed" from "does not exist" would confirm the channel is there, and TDC
refuses identically in both cases, everywhere.

**Administrators cannot read direct messages**, enforced by the server rather
than by policy. The caveat is real and stated plainly: anybody with access to the
machine's database or filesystem can read anything stored there. That is true of
every self-hosted service, and of Telegram's own servers, and no software
prevents it. What it means practically is that the list of people who can read
your community's direct messages is now a list your community chooses, and knows.

**Search that respects permissions.** One box searches messages, channels and
people together. It never returns anything you could not otherwise read — no
messages from channels you are not in, and no channel you could not discover.
There is no query syntax to learn, and searching across the whole community
rather than one group at a time is something neither Telegram nor WhatsApp offers
in any useful form.

**A real moderation record.** Reports, a moderation queue, and an audit log where
every action carries its reason and its duration, all reversible. Reporting is
anonymous to the reported person. Blocking is invisible to the blocked person,
and moderators keep their full view regardless, because a moderation problem
somebody can hide from moderators is not one anybody can deal with.

**Deletions leave evidence.** A deleted message leaves a placeholder: the content
goes, the fact that something was there and was removed stays. An edited message
says it was edited. Quoting somebody who can then make the original silently
disappear is the specific problem this avoids.

**Unread state that converges.** Kept on your account rather than in each
browser, so reading something on your phone clears it on your laptop.

**Notifications designed to be bearable.** Nothing is raised for the conversation
you are looking at. A notification is withdrawn when you read the thing
elsewhere. A burst of messages arrives batched rather than one buzz each. Push
notifications can be set to hide their content, so a push about a private
conversation says that something arrived without saying what.

**It works on the phones your members actually have.** Offline messages queue and
send when a connection returns, and the queue survives the tab being closed.
Images are reduced before sending by default, with the original an explicit
choice — which matters on metered data.

**It costs nothing to run** beyond the machine. No account with anybody, no API
key, no paid service. Everything optional is genuinely optional and off by
default.

**You can leave.** The data is in a Postgres database you control and the
software is open source. Whatever else is true of this move, it is the last one
you are forced to make.

### What is different without being better or worse

- **There is no "last seen".** Presence is online or offline, derived from
  whether somebody is connected. Separately, a person can set Available, Busy,
  Away or Appear offline.
- **Typing indicators are always sent.** The server honours a preference not to
  send yours, but neither client has a control for it yet.
- **Reactions are emoji, and GIFs upload like any other file.** A GIF tab in the
  picker exists where the operator has configured a provider.
- **Muting one conversation has no control in either client yet.** Leaving a
  channel is the available way to stop hearing from it.
- **A scheduled message cannot be called back.** Scheduling works; reviewing and
  cancelling what you scheduled is on the server but has no control yet.

## Doing the move

### Run both for a while

Do not cut over on a Monday and hope. Run the new server alongside Telegram and
WhatsApp for a few weeks — long enough that the sceptics have used it, and long
enough that whoever is operating it has restored a backup once.

### Who moves first

Start with the organisers and the people who post most, not with everybody. A
server with four hundred accounts and no conversation in it reads as abandoned,
and people who join an empty room do not come back. Get twenty people talking
first, then invite the rest into somewhere that is already alive.

### Set up the channels before anybody arrives

Make the handful of channels you actually want and pin what belongs in them —
the rules, the code of conduct, the introduction. This is the administrator's
job, since nobody else can create a channel, so it has to happen before people
arrive rather than as they ask.

Resist making one channel per topic on day one; most of those should be threads,
and a server with thirty empty channels is harder to join than one with four busy
ones.

### Pick a cut-over date and name it

A date when the organisers stop answering questions in the old groups and answer
them in the new one. Announce it more than once, and in the old groups, where
everybody still is.

The cut-over that works is about where _organisers_ reply, not where members are
forbidden to post. You cannot make people move; you can make the new place the
one where things happen.

### Do not delete the old groups

Two reasons, and the first is the one that matters.

**The history is only there.** Nothing was imported and nothing can be. Deleting
the Telegram group deletes the community's archive permanently. Keep it, even
long after nobody posts in it.

The second reason is that a group with a pinned pointer to the new server is how
people who were inactive for six months find you. Set the old group to
announcements-only if the platform allows it, pin a message saying where
everybody went and how to get an invitation, and leave it be.

### Expect a long tail

Some members will not move. Some will install the PWA, use it twice and drift
back. That is normal, and it is not a verdict on the software — it is what
happens whenever a community changes where it talks. Plan for the old groups to
have a slow-declining population for a year, and for somebody to keep answering
there.

## Data and privacy, honestly

When the community runs its own server, the data stops being Telegram's problem
and becomes the community's responsibility. That cuts both ways and both should
be said.

**What improves.** The messages are in a database your community controls rather
than one nobody in Tanzania has access to or standing over. There is no
advertising business attached, no analytics beyond what the software stores to
work, and no third party receiving anything by default — a server with the
optional features off makes no outbound requests for link previews, GIF search or
anything else.

**What the operator can see.** An administrator cannot read direct messages
through the application. An administrator with access to the machine's database
or filesystem can read anything stored on it, including message content. No
software prevents that, and it is true of every self-hosted service. The
difference from Telegram is not that nobody can read it — it is that the people
who can are people your community chose and can name.

Name them. `docs/privacy-policy.md` is a template to adapt, and it has a place to
state who holds that access and what rule they hold themselves to. Publish the
result before you invite anybody, not after somebody asks.

**What members should be told up front**, because these are the things that
surprise people later: there is no data export, deleting an account does not
delete its messages, and there is no password reset.

## What members need

**A browser**, and the server's address. That is the whole requirement.

**The web app installed to the home screen**, on a phone — its own icon, no
browser furniture, notifications with the browser closed where the operator has
configured them. It is worth walking people through this once, in person or with
screenshots, because "add to home screen" is buried in a browser menu and the
people who most need the reminder are the ones least likely to find it.

**The desktop client, optionally.** It is the same interface in a native window,
and it adds operating-system notifications, the session held in the OS keychain
rather than asked for every launch, a tray icon carrying the unread count, and
launching at login. It is installed once and pointed at your server, because the
application cannot know which server is yours. Building it is documented in
`apps/desktop/README.md`; on a phone or tablet the installed web app is the one
to use.

## Realistic scale

**One server, one process, one database.** Multi-node deployment is not
implemented. That is the shape of the deployment and it will comfortably hold a
community.

**One to two gigabytes of memory** for the machine. The server process targets
under a gigabyte; Postgres wants its own share, and attachments want disk rather
than memory. A cheap VPS is the intended deployment, and that is a design
constraint rather than a hope.

**No limits are set by default.** `limits.max_users`, `limits.max_channels` and
`limits.max_storage_bytes` all default to zero, which means no limit — a limit
nobody chose is a limit that surprises somebody. They are there when you want
them, and the one most worth setting early is storage: attachments are the thing
that fills a disk, and a full disk stops the server.

**Attachments are the thing to watch**, not messages. Text is small and
predictable. A community that shares screenshots and PDFs will grow its
attachment store faster than anybody expects, which is why the compose file keeps
the database and the attachments in separate volumes — so that they can be backed
up on different schedules and sized independently.

## Where to read next

- **[Using TDC](using-tdc.md)** — what the application does, for the people who
  will use it. The document to send to members.
- **[Hosting](hosting.md)** — deploying, TLS, backup and restore. The document
  for whoever is responsible for the machine.
- **[Privacy policy](privacy-policy.md)** — the template to adapt and publish
  before you invite anybody.
- **[Terms of use](terms-of-use.md)** — the other template, covering acceptable
  use, moderation and content ownership.
