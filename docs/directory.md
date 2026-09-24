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

## Not shared with other servers

The directory belongs to this server. Federation carries rooms an operator has
chosen to federate and nothing else: no federated route reads a directory
profile, a company, or anything built on them, and a peer server learns about
this server's directory only what any stranger could — by browsing it, where the
operator has made that possible. A federated directory would be a separate,
deliberate decision, not something the protocol backs into.

## Over the API

| Route                                                             | What it does                                                                                     |
| ----------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| `GET /api/directory/me`                                           | your own profile, all of it                                                                      |
| `PUT /api/directory/me`                                           | create or replace your profile (the `profile` object above)                                      |
| `GET /api/directory/search`                                       | one page of the directory; filters `q`, `skill`, `available=true`, `location`, `company`, `page` |
| `GET /api/directory/skills?q=`                                    | the vocabulary, and your own proposals                                                           |
| `POST /api/directory/skills`                                      | propose a skill; answers with the existing one where the name already means one                  |
| `GET /api/directory/moderation/skills`                            | the review queue (`directory.skills.manage`)                                                     |
| `POST /api/directory/moderation/skills/:id/approve`               | approve a proposal (`directory.skills.manage`)                                                   |
| `POST /api/directory/moderation/skills/:id/reject`                | reject a proposal, with a `reason` (`directory.skills.manage`)                                   |
| `POST /api/directory/moderation/skills/:id/merge`                 | merge a proposal `into` another skill (`directory.skills.manage`)                                |
| `GET /api/directory/moderation/profiles/:username/claims`         | the verifiable claims on a profile (`directory.verify`)                                          |
| `POST /api/directory/moderation/profiles/:username/verifications` | verify one claim: `kind`, `claimId`, `note` (`directory.verify`)                                 |
| `POST /api/directory/moderation/verifications/:id/revoke`         | revoke a verification, with a `reason` (`directory.verify`)                                      |
| `DELETE /api/directory/me`                                        | delete your profile, keeping your account                                                        |
| `GET /api/directory/me/export`                                    | your profile as `tdc.directory.profile/1`                                                        |
| `GET /api/directory/profiles/:username`                           | a profile as you may see it; signing in is optional                                              |
| `GET /api/directory/me/companies`                                 | your companies, and invitations you have not answered                                            |
| `POST /api/directory/me/companies/:slug/accept`                   | accept a company's invitation                                                                    |
| `DELETE /api/directory/me/companies/:slug`                        | leave a company, or decline its invitation                                                       |
| `POST /api/directory/companies`                                   | create a company                                                                                 |
| `GET /api/directory/companies/:slug`                              | a company; signing in is optional                                                                |
| `PUT /api/directory/companies/:slug`                              | change a company (its administrators)                                                            |
| `DELETE /api/directory/companies/:slug`                           | delete a company (its administrators)                                                            |
| `POST /api/directory/companies/:slug/members`                     | invite somebody by username (its administrators)                                                 |
| `DELETE /api/directory/companies/:slug/members/:user`             | remove somebody (its administrators)                                                             |
| `GET /api/directory/moderation/profiles/:username`                | read a profile as its owner sees it (`directory.profile.manage`)                                 |
| `PUT /api/directory/moderation/profiles/:username`                | change or clear a profile (`directory.profile.manage`)                                           |

Every directory route answers `404` while the directory is switched off.
