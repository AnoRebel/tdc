# The directory

The directory is where somebody on a TDC server can choose to be found by people
who do not already know them — a client looking for a developer, a developer
looking for a company — and where the two can agree on work.

It is **off unless the operator turns it on**, and **nobody is in it unless they
put themselves there**. An account with no directory profile is a complete
account: chat, channels, direct messages and everything else work exactly as
they do without it, and nobody can find you through the directory.

This document describes what the directory does and why it behaves as it does.
[Using TDC](using-tdc.md#the-directory) has the short version for people using
it, [Hosting](hosting.md#the-directory) covers switching it on, and the
[privacy policy](privacy-policy.md#your-directory-profile) lists what it stores.

## Why it is a separate decision

Everything else on a TDC server discloses things to people you already share a
conversation with, and the server leans towards restraint there: a member list
shows your name and address, and whether you are online, your status and when
you were last seen are yours to withhold. Member search returns only a name and
an address, deliberately.

A directory is the opposite instinct — its whole purpose is being found by a
stranger. Rather than loosen the first to make room for the second, the
directory is a layer of its own:

- **Its own record.** A directory profile is a separate thing you create. It
  does not read or change your privacy settings, your presence, or anything a
  member list shows. Creating a public profile does not make you visible as
  online; hiding your presence does not remove you from the directory.
- **Its own search.** Searching the directory looks only at directory profiles.
  Member search is unchanged by the directory and returns nothing new.
- **Its own visibility settings**, described below.

## Your profile

A profile can say:

- a **headline** — one line on what you do;
- your **location** and **experience**;
- an **about** section;
- **portfolio links** — up to twenty, each a title, a web link and an optional
  description. Links rather than uploads: your work already lives somewhere, and
  a profile is read on phones over metered data. Only `http` and `https` links
  are accepted;
- **contact details** — an email address, a phone number and a website;
- your **skills**, each with what you have done with it (below);
- whether you are **available for work**, and what kind (below);
- the **companies** you have agreed to be listed by (below).

Everything is optional. A profile with nothing but a visibility setting is a
valid profile.

### Who sees what

Each profile has three visibility settings, each one of:

| Setting      | Who                                                                                    |
| ------------ | -------------------------------------------------------------------------------------- |
| **Anyone**   | anybody who can browse this server's directory, including people who are not signed in |
| **Members**  | people signed in to this server                                                        |
| **Only you** | nobody else                                                                            |

The three settings govern:

1. **The profile as a whole** — whether it appears in the directory at all.
   Defaults to _Members_.
2. **What you wrote** — headline, location, experience, about, portfolio,
   skills, availability and companies. Defaults to _Members_.
3. **Your contact details.** Separate, because being findable by strangers and
   being reachable by them are different things. Defaults to **_Only you_**, so
   a profile saved without touching this setting publishes no contact details
   to anybody.

**A section is never more visible than the profile it belongs to.** Contact
details set to _Anyone_ on a profile set to _Members_ are visible to members
only; on a profile set to _Only you_, to nobody.

"Anyone" reaches people who are not signed in only on a server the operator has
made publicly discoverable. On a limited or private server, the directory is as
undiscoverable as the server itself, and _Anyone_ means the same as _Members_.

**What you may not see is left out of what the server sends**, not sent and
hidden by the application. It is withheld in the database query itself, so a
hidden field is never read on behalf of somebody who may not see it. An absent
field means only "nothing you may see": somebody who wrote no bio and somebody
who hid theirs look the same from outside.

**Every way of not being allowed to see a profile looks the same.** No such
person, a person with no profile, a profile set to _Only you_, and a
members-only profile asked for by somebody not signed in all receive the same
`404` with the same body.

### Skills

Skills come from a list the server keeps — the vocabulary — rather than being
typed freely, because free text is how one skill becomes five spellings nobody
can search for. The vocabulary also knows other spellings: "golang" is Go,
"postgres" is PostgreSQL, "k8s" is Kubernetes. Typing either finds the one
entry, and searching for either finds everybody who has it.

A skill that is not on the list can be **proposed**. It goes on your profile
straight away, marked _Waiting for review_, and:

- only you see it there — other people see your profile without it;
- nobody can find you by it;
- nobody else can add it to their profile.

A reviewer then approves it (it joins the list, and becomes searchable for
everybody who has it), rejects it (it leaves every profile it was on), or
decides it is a skill the list already has under another name and merges it —
your profile then carries that skill, keeping what you wrote about it, and your
spelling becomes another name for it.

Each skill on your profile can carry a line on what you have done with it —
"six years, mostly payment integrations". That line is your claim about the
skill, and it is what a moderator checks if they verify it.

A profile holds at most thirty skills.

### Availability

You can say you are **open to work**, what kind of work you want, and for how
long — a week to six months. It has to lapse: a statement that never expired is
how a profile somebody abandoned would go on advertising them indefinitely.
Once the time passes you are simply not shown as available, with nothing else
about your profile changed, until you say so again.

**Availability has nothing to do with whether you are online.** It is a
statement about work, not a reading of your presence: you can hide your
presence and be listed as available, or be online all day and not looking.

### Deleting it

Deleting your profile removes it, and everything in it, from the directory at
once. Your account, your messages, your channels and your sessions are not
touched — this is leaving the directory, not the server.

### Taking it with you

**Export** gives you your whole profile as a JSON file, in the documented format
`tdc.directory.profile/1`:

```json
{
  "format": "tdc.directory.profile/1",
  "exportedAt": "2026-09-23T09:00:00Z",
  "server": "https://tdc.example",
  "username": "amina",
  "displayName": "Amina",
  "profile": {
    "headline": "Backend developer",
    "bio": "Go and Postgres.",
    "location": "Dar es Salaam",
    "experience": "Eight years",
    "contactEmail": "amina@example.com",
    "contactPhone": "",
    "contactUrl": "",
    "visibility": "members",
    "detailsVisibility": "members",
    "contactVisibility": "hidden",
    "portfolio": [
      {
        "id": "0b3c…",
        "title": "Clinic booking",
        "url": "https://example.com/clinic",
        "description": ""
      }
    ]
  },
  "companies": [{ "slug": "kilima", "name": "Kilima Labs", "role": "Backend lead" }],
  "createdAt": "2026-09-01T08:00:00Z",
  "updatedAt": "2026-09-23T08:59:00Z"
}
```

Times are absolute instants in RFC 3339, in UTC. The `profile` object is exactly
what `PUT /api/directory/me` accepts, so an export can be sent straight back —
to this server after deleting, or to another — to recreate the profile. The
portfolio identifiers are this server's; sent to a server that does not know
them, each link is simply added as new.

## Finding people

**Directory** in the sidebar lists the profiles you may see, those currently
available first and then the most recently updated. It can be narrowed by any
of:

- a **name, or words** — matched against names, and against headlines and
  about sections you are allowed to read;
- a **skill** — by its name or any other spelling the vocabulary knows;
- **available for work only**;
- a **location**;
- a **company** — its address or part of its name.

**A filter only matches what a person lets you see.** Somebody whose location is
hidden from you is not found by searching for their town, because finding them
that way would tell you where they are. A filter on a hidden detail never
matches, rather than matching and hiding the result.

Results come **twenty to a page**, and there is no way to ask for more at once.
Paging stops after twenty-five pages however many profiles match. Searching and
opening profiles are also **rate limited** — thirty a minute for somebody
signed in, ten a minute for each address browsing without signing in, both
adjustable by the operator. Together these are what keeps the directory a place
to look for somebody rather than a way to copy everybody out of it.

Member search — the search box at the top of the sidebar — is not changed by
the directory. It finds people by name and address as it always has, returns
nothing a directory profile says, and describes somebody with a profile exactly
as it describes somebody without one.

## Enquiries

An enquiry is the first contact a client makes with somebody through their
profile: a subject, and a message saying what the work is.

**Whether you take enquiries is your profile's setting, not your direct-message
setting.** The two are separate decisions and neither changes the other:

- Somebody who takes direct messages from nobody can still take enquiries.
- Somebody who closes enquiries can still be messaged directly by whoever their
  direct-message setting allows.

Until you choose, a public profile takes enquiries — a profile advertised to
strangers that nobody could contact would be a dead end — and any other profile
does not. Once you choose, your choice stands whatever you later do with the
profile's visibility. Closing enquiries refuses new ones; the ones you already
have, and their conversations, are untouched.

An enquiry to somebody not taking them is refused, and the sender is told so in
those words. **Somebody who has blocked you appears exactly as somebody taking
no enquiries** — the same refusal, and a profile that says it takes none — so a
block is never disclosed.

**An enquiry is one message.** Sending one opens a private conversation between
the two of you, holding the enquiry, which the recipient is notified about
exactly as for any direct message addressed to them. The sender is not in that
conversation, and cannot add to it, until the recipient takes the enquiry up;
the sender's own list shows it as _Waiting for an answer_. Like every direct
conversation it is never listed, searchable or discoverable by anybody but the
two people in it.

You may have one unanswered enquiry with any one person at a time. Sending
enquiries is limited to five an hour for each sender — plenty for somebody
looking for a developer, very little for somebody sending the same message to
everybody listed — and the operator can change the figure. Refusals do not count
against it.

Enquiries you received and sent are under **Directory → Enquiries**. Each
enquiry's text is kept with the enquiry as well as in its conversation, so the
list still says what was asked after the conversation's own retention has done
its work.

## Engagements

An enquiry the recipient takes up becomes an **engagement**: a record of the
work the two of you discuss, propose, agree and finish, and the conversation it
happens in. Either of you finds it under **Directory → Engagements**; nobody
else can find it at all.

An engagement moves through these states, one step at a time:

| State                            | What it means                                               |
| -------------------------------- | ----------------------------------------------------------- |
| Discussion                       | the enquiry was taken up, and you are talking it through    |
| Proposal                         | one of you has sent a proposal                              |
| Negotiation                      | a proposal was declined or changes were asked for           |
| Agreement                        | you both accepted the same proposal                         |
| Work under way                   | the agreed work is in progress                              |
| Completion awaiting confirmation | one of you said the work is complete; the other has not yet |
| Completed                        | you both said so                                            |
| Withdrawn                        | one of you withdrew before an agreement                     |

**You both see the same state**, because there is one record and both of you
read it. **Every step is recorded** with who took it and when, and either of you
can read the history. A step cannot be skipped: the server refuses it, and the
database refuses to record it even if something tried.

**Either of you can withdraw at any point before an agreement**, with a reason
the other will see. The engagement closes for both of you, nothing was agreed,
and it cannot be reopened. Its conversation is kept, and stops taking new
messages. After an agreement there is no withdrawing — see below for what
happens instead.

When the other person changes the engagement, your page follows without being
reloaded: the change is announced in the engagement's conversation, so it
arrives the way a message does, including after you were offline.

### The conversation

Every engagement has a private conversation between the two of you — the one the
enquiry opened. It is an ordinary direct conversation, and so has every rule a
direct conversation has: files, retention and moderation work exactly as they do
for any other, nobody else can read it, and it never appears in search or
browsing for anybody but the two of you — including administrators using the
ordinary search.

### Proposals

Open the engagement's **Proposal** tab and either of you can write one: **what
the work is**, **when** — in your own words, such as "six weeks from
agreement" — and a list of **deliverables, each with a price**. The total is
added up by the server, not typed.

The other person can then **accept** it, **decline** it, or **ask for a change
to one part of it**: the work, the timeline, the currency, or a single
deliverable by its number. A change request says which part and what you would
like instead, and the person who sent the proposal is notified with exactly
that. Declining or asking for a change moves the engagement to negotiation.

Answering is done with a **new version**. Writing again starts from the latest
version, so changing one line means changing one line; what is sent is version
2, and **version 1 stays exactly as it was**. Both of you can read every
version and every answer under **Earlier versions**, and the latest marks each
part that changed since the one before it. Only the latest version can be
accepted.

### Money

Prices are kept as **whole numbers of the currency's smallest unit** — cents,
or the hundredth of a shilling — together with the **three-letter currency
code** (TZS unless you choose another). 1,500,000.00 TZS is kept as 150000000,
so nothing is ever rounded. You type prices the way you would write them, with
or without separators, and the form refuses more decimal places than the
currency has. One proposal is in one currency.

### The agreement

**An agreement exists only once both of you have accepted the same version.**
Accepting alone records your acceptance and nothing else; the screen says the
agreement is waiting for the other person. When the second acceptance arrives,
the server records the agreement in the same step: which version, a copy of its
terms, and when each of you accepted.

**Nothing can change an agreement once recorded** — nor a sent version, nor an
answer to one. There is no way to edit them in the application or over the API,
and the database itself refuses an attempt, even by its owner. The one change it
allows is the one account deletion needs: if either of you deletes your account,
the record stays for the other with your username, and only the link to your
deleted account is cleared.

### What the server is not

Wherever an agreement is made or shown, the page carries this statement, and
the exported agreement carries the same words:

> This is a record of what the two parties agreed, kept by this server so both
> can refer to it. The server and its operator are not a party to this
> agreement. They do not hold or move any money, provide escrow or payment,
> guarantee that either party will perform, or resolve disputes about it. No
> payment, escrow or dispute-resolution service is connected to this server.

It is the server's own text, given to the page with the agreement rather than
written into the application, so the screen and the file cannot say different
things. It is shown as written, in English, in every language the application
is in.

### Exporting the agreement

Either of you can **Export the agreement** as a JSON file. Both of you get the
same document, apart from when it was exported:

| Field                | What it holds                                                                  |
| -------------------- | ------------------------------------------------------------------------------ |
| `format`             | `tdc.directory.agreement/1`                                                    |
| `exportedAt`         | when this copy was made                                                        |
| `server`             | the server's address                                                           |
| `disclosure`         | the statement above                                                            |
| `engagementId`       | the engagement                                                                 |
| `title`              | its title, from the enquiry's subject                                          |
| `version`            | which version was agreed                                                       |
| `client`/`developer` | each party's `username` and when they accepted (`acceptedAt`)                  |
| `terms`              | `scope`, `timeline`, `lineItems`, `currency`, `totalMinor`, `sentBy`, `sentAt` |
| `recordedAt`         | when the agreement was recorded                                                |
| `history`            | every change of state: `from`, `to`, `actor`, `at`, and any `note`             |

Every time is an absolute instant in UTC (`2026-10-02T09:30:00Z`); every amount
is in minor units beside its currency. Nobody outside the engagement can export
it.

### Finishing the work

After the agreement, either of you **starts the work**. When one of you says
**the work is complete** — with a note, if you like — it is recorded as
_completion awaiting confirmation_ and **stays that way, for both of you to see,
until the other confirms**. The person who said it cannot confirm it themselves.
When the other confirms, the engagement is **completed**, and its history shows
every step from the enquiry on, with who took each.

## Companies

Anybody may create a company profile — a name, a description, a location, and
whether it is visible to _Anyone_ or to _Members_. Whoever creates it is its
first administrator, and administrators may change it, invite people, and remove
them. A company always keeps at least one administrator; the last one deletes
the company rather than leaving it.

**A company can ask; only you can accept.** Inviting somebody lists nobody: the
invitation appears on the invitee's own profile page, and until they accept, the
company page does not show them and their profile does not mention the company.
Accepting shows your name and role on the company's page to everybody who can
see the company, and the company on your profile.

**Leaving ends it on both sides at once.** Leave, or decline an invitation, and
the association is gone from the company's page and from your profile in the
same moment. A company administrator can remove somebody the same way.

Companies are not accounts. They cannot sign in, send messages, or hold
permissions.

## Moderation

A moderator holding **`directory.profile.manage`** can:

- **read a profile as its owner sees it**, including what the owner hid — every
  such read is written to the audit log (`directory.profile.viewed`), because a
  privacy setting a moderator can step around silently is not a privacy
  setting;
- **change or clear what a profile says**, and hide it, with a reason that is
  required. The audit log records the moderator, whose profile it was, which
  fields changed and why (`directory.profile.moderated`) — never the content
  that was removed.

Nobody else can change a profile but its owner.

### Reviewing skills

A reviewer holding **`directory.skills.manage`** works through the proposed
skills under **Directory → Review**. For each one:

- **Approve** — it joins the vocabulary. Anybody may add it, and everybody who
  has it can be found by it.
- **Merge into another** — it is a skill the vocabulary already has under
  another name. Everybody who added the proposal has the existing skill
  instead, keeping what they wrote about it, and the proposal's spelling
  becomes another name for it, so the next person to type it lands in the right
  place.
- **Reject** — with a reason. It leaves every profile it was on.

Each decision is written to the audit log as `directory.skill.approved`,
`directory.skill.merged` or `directory.skill.rejected`, naming the reviewer and
the skill, and — for a merge — what it became, or — for a rejection — why.

A reviewer who proposes a skill adds it to the vocabulary directly
(`directory.skill.added`); there is nobody for them to ask. That is how an
operator grows the vocabulary beyond the starting list.

The queue is deliberately plain. If it grows faster than people can clear it,
that is a question of who reviews, not of the tool.

## Verification

A moderator can verify **one specific claim** on a profile: a skill and what its
owner says about it, a portfolio link, an association with a company, or the
person's identity — their name and the link they give as who they are
elsewhere. There is no verification of a profile as a whole. A profile with one
verified skill and nine others shows exactly that.

**What a verification means**, and the interface says so wherever one is shown,
in the same words everywhere: somebody with the permission to verify checked the
evidence for this one claim and judged it credible. It is **not** an
endorsement of the person, their work, or whether they will deliver — nobody
checked any of that.

Anybody reading a profile sees which claims are verified, by whom and when.
**What the moderator wrote about what they checked is shown only to the profile's
owner**, since it may mention something the owner hides from everybody else —
the link that was checked, say.

**Editing a verified claim clears its verification.** What is on the profile
afterwards is not what anybody looked at, so it goes back to being
self-asserted, and it stays that way even if the claim is edited back — until a
moderator checks it again. Leaving a company clears a verified association with
it the same way. Your own profile page lists your verified claims, with what
was checked, and says as much before you edit.

Nobody verifies their own claims, whatever permissions they hold.

### Verifying and revoking

A moderator holding **`directory.verify`** works under **Directory → Review →
Verify claims**: open a profile by username, read each claim as it stands, and
for one at a time either

- **verify** it, with a note on what was checked — required; or
- **revoke** a standing verification, with the reason — also required. The
  claim is shown as self-asserted again at once.

A hidden profile cannot be opened for verification: its owner has not asked to
be found. Every grant, revocation and clearing is written to the audit log:

| Action                           | Actor                           | Detail                                           |
| -------------------------------- | ------------------------------- | ------------------------------------------------ |
| `directory.verification.granted` | the moderator                   | whose profile, which claim, and what was checked |
| `directory.verification.revoked` | the moderator                   | which claim, and why                             |
| `directory.verification.cleared` | the owner whose edit cleared it | which claim, and that it was edited or removed   |

Each verification keeps a copy of the claim as it was when it was checked, so a
record reviewed later shows exactly what was attested to.

### Reading the trail

Every directory decision a moderator or reviewer makes is in the server's audit
log, under these actions:

| Action                           | Written when                                              |
| -------------------------------- | --------------------------------------------------------- |
| `directory.profile.viewed`       | a moderator read a profile including what its owner hid   |
| `directory.profile.moderated`    | a moderator changed, cleared or hid a profile             |
| `directory.skill.added`          | a reviewer added a skill directly                         |
| `directory.skill.approved`       | a reviewer approved a proposed skill                      |
| `directory.skill.merged`         | a reviewer merged a proposal into an existing skill       |
| `directory.skill.rejected`       | a reviewer rejected a proposed skill                      |
| `directory.verification.granted` | a moderator verified a claim                              |
| `directory.verification.revoked` | a moderator revoked a verification                        |
| `directory.verification.cleared` | an owner's edit cleared a verification on their own claim |

Reading the log takes **`server.audit.read`**, which is deliberately separate
from the permissions that let somebody moderate: being able to act is not the
same as being able to review who acted. Whoever holds it reads the log under
**Audit** in the web client, or narrows it to one kind of decision over the API:

```sh
curl "https://tdc.example/api/admin/audit?action=directory.verification.revoked" \
  -H "Authorization: Bearer $TOKEN"
```

Each entry names who acted, when, and on what, with the reason where one was
required. None of them holds what a moderator removed from a profile, or the
hidden contents they read — only that they did.

## Not shared with other servers

The directory belongs to this server. Federation carries rooms an operator has
chosen to federate and nothing else: no federated route reads a directory
profile, a company, or anything built on them, and a peer server learns about
this server's directory only what any stranger could — by browsing it, where the
operator has made that possible. A federated directory would be a separate,
deliberate decision, not something the protocol backs into.

## Over the API

| Route                                                                | What it does                                                                                             |
| -------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| `GET /api/directory/me`                                              | your own profile, all of it                                                                              |
| `PUT /api/directory/me`                                              | create or replace your profile (the `profile` object above)                                              |
| `GET /api/directory/search`                                          | one page of the directory; filters `q`, `skill`, `available=true`, `location`, `company`, `page`         |
| `GET /api/directory/skills?q=`                                       | the vocabulary, and your own proposals                                                                   |
| `POST /api/directory/skills`                                         | propose a skill; answers with the existing one where the name already means one                          |
| `GET /api/directory/moderation/skills`                               | the review queue (`directory.skills.manage`)                                                             |
| `POST /api/directory/moderation/skills/:id/approve`                  | approve a proposal (`directory.skills.manage`)                                                           |
| `POST /api/directory/moderation/skills/:id/reject`                   | reject a proposal, with a `reason` (`directory.skills.manage`)                                           |
| `POST /api/directory/moderation/skills/:id/merge`                    | merge a proposal `into` another skill (`directory.skills.manage`)                                        |
| `GET /api/directory/moderation/profiles/:username/claims`            | the verifiable claims on a profile (`directory.verify`)                                                  |
| `POST /api/directory/moderation/profiles/:username/verifications`    | verify one claim: `kind`, `claimId`, `note` (`directory.verify`)                                         |
| `POST /api/directory/moderation/verifications/:id/revoke`            | revoke a verification, with a `reason` (`directory.verify`)                                              |
| `POST /api/directory/profiles/:username/enquiries`                   | send an enquiry: `subject`, `body`                                                                       |
| `GET /api/directory/me/enquiries`                                    | enquiries you received, or with `?box=sent` those you sent                                               |
| `POST /api/directory/enquiries/:id/accept`                           | take up an enquiry you received; it becomes an engagement                                                |
| `POST /api/directory/enquiries/:id/decline`                          | decline an enquiry you received                                                                          |
| `POST /api/directory/enquiries/:id/withdraw`                         | withdraw an enquiry you sent                                                                             |
| `GET /api/directory/me/engagements`                                  | your engagements                                                                                         |
| `GET /api/directory/engagements/:id`                                 | an engagement you are party to, with its history                                                         |
| `POST /api/directory/engagements/:id/withdraw`                       | withdraw before an agreement, with an optional `reason`                                                  |
| `GET /api/directory/engagements/:id/proposals`                       | every version, every answer, the agreement if there is one, and the `disclosure`                         |
| `POST /api/directory/engagements/:id/proposals`                      | send a version: `scope`, `timeline`, `currency`, `lineItems` of `description` and `amountMinor`          |
| `POST /api/directory/engagements/:id/proposals/:versionId/responses` | answer the latest version: `kind` of `accept`, `decline` or `request_changes`, with `part` and `comment` |
| `POST /api/directory/engagements/:id/start`                          | start the agreed work                                                                                    |
| `POST /api/directory/engagements/:id/complete`                       | say the work is complete, with an optional `note`                                                        |
| `POST /api/directory/engagements/:id/confirm-completion`             | confirm the other party's completion                                                                     |
| `GET /api/directory/engagements/:id/agreement/export`                | the agreement as `tdc.directory.agreement/1`                                                             |
| `DELETE /api/directory/me`                                           | delete your profile, keeping your account                                                                |
| `GET /api/directory/me/export`                                       | your profile as `tdc.directory.profile/1`                                                                |
| `GET /api/directory/profiles/:username`                              | a profile as you may see it; signing in is optional                                                      |
| `GET /api/directory/me/companies`                                    | your companies, and invitations you have not answered                                                    |
| `POST /api/directory/me/companies/:slug/accept`                      | accept a company's invitation                                                                            |
| `DELETE /api/directory/me/companies/:slug`                           | leave a company, or decline its invitation                                                               |
| `POST /api/directory/companies`                                      | create a company                                                                                         |
| `GET /api/directory/companies/:slug`                                 | a company; signing in is optional                                                                        |
| `PUT /api/directory/companies/:slug`                                 | change a company (its administrators)                                                                    |
| `DELETE /api/directory/companies/:slug`                              | delete a company (its administrators)                                                                    |
| `POST /api/directory/companies/:slug/members`                        | invite somebody by username (its administrators)                                                         |
| `DELETE /api/directory/companies/:slug/members/:user`                | remove somebody (its administrators)                                                                     |
| `GET /api/directory/moderation/profiles/:username`                   | read a profile as its owner sees it (`directory.profile.manage`)                                         |
| `PUT /api/directory/moderation/profiles/:username`                   | change or clear a profile (`directory.profile.manage`)                                                   |

Every directory route answers `404` while the directory is switched off.
