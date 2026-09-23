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
2. **What you wrote** — headline, location, experience, about, portfolio and
   companies. Defaults to _Members_.
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

## Not shared with other servers

The directory belongs to this server. Federation carries rooms an operator has
chosen to federate and nothing else: no federated route reads a directory
profile, a company, or anything built on them, and a peer server learns about
this server's directory only what any stranger could — by browsing it, where the
operator has made that possible. A federated directory would be a separate,
deliberate decision, not something the protocol backs into.

## Over the API

| Route                                                 | What it does                                                     |
| ----------------------------------------------------- | ---------------------------------------------------------------- |
| `GET /api/directory/me`                               | your own profile, all of it                                      |
| `PUT /api/directory/me`                               | create or replace your profile (the `profile` object above)      |
| `DELETE /api/directory/me`                            | delete your profile, keeping your account                        |
| `GET /api/directory/me/export`                        | your profile as `tdc.directory.profile/1`                        |
| `GET /api/directory/profiles/:username`               | a profile as you may see it; signing in is optional              |
| `GET /api/directory/me/companies`                     | your companies, and invitations you have not answered            |
| `POST /api/directory/me/companies/:slug/accept`       | accept a company's invitation                                    |
| `DELETE /api/directory/me/companies/:slug`            | leave a company, or decline its invitation                       |
| `POST /api/directory/companies`                       | create a company                                                 |
| `GET /api/directory/companies/:slug`                  | a company; signing in is optional                                |
| `PUT /api/directory/companies/:slug`                  | change a company (its administrators)                            |
| `DELETE /api/directory/companies/:slug`               | delete a company (its administrators)                            |
| `POST /api/directory/companies/:slug/members`         | invite somebody by username (its administrators)                 |
| `DELETE /api/directory/companies/:slug/members/:user` | remove somebody (its administrators)                             |
| `GET /api/directory/moderation/profiles/:username`    | read a profile as its owner sees it (`directory.profile.manage`) |
| `PUT /api/directory/moderation/profiles/:username`    | change or clear a profile (`directory.profile.manage`)           |

Every directory route answers `404` while the directory is switched off.
