# Terms of use

<!-- TEMPLATE. Read the note below before publishing this. -->

## A note to the operator

This is a template, not a finished agreement. TDC is self-hosted. There is no
central service and no company behind it. Whoever runs a server sets its rules,
enforces them, and carries whatever responsibility comes with that. These terms
are between you and the people using your server, and nobody else is party to
them.

Every place you must fill something in is written as `[LIKE THIS]`. Search for
`[` before publishing.

**This is not legal advice.** What you can disclaim, what you must promise, and
what a court in your jurisdiction will actually enforce varies. Limitation of
liability clauses in particular are read narrowly in many places and are void in
some. A community server in Tanzania, one run by a registered company, and one
run by an individual as a hobby are not in the same position. Have a lawyer read
this if anything about your server is more than a hobby.

Where these terms handle personal data, they lean on the
[privacy policy](privacy-policy.md), which is the document that describes what
your server actually collects. Publish both or neither.

---

**Server:** `[SERVER NAME]` at `[https://chat.example.org]`
**Operator:** `[OPERATOR NAME, AND LEGAL ENTITY IF THERE IS ONE]`
**Contact:** `[EMAIL ADDRESS]`
**In effect from:** `[DATE]`

## What this is

`[SERVER NAME]` is a chat and collaboration server run by `[OPERATOR NAME]` for
`[WHO IT IS FOR]`. It runs TDC, which is open-source software.

By creating an account or using this server, you agree to these terms. If you do
not agree to them, do not use the server.

These terms cover this server only. Another server running the same software is
run by somebody else, under their own rules.

## Who may use this server

`[STATE YOUR MINIMUM AGE AND ANY ELIGIBILITY RULE — membership of an
organisation, an invitation, a verified email domain. If your server is open to
anybody, say that.]`

You are responsible for what happens under your account. Keep your password.
**There is no password reset in this software** — an account that loses its
password needs the operator, and `[SAY WHETHER YOU WILL RESTORE ONE AND HOW YOU
WILL CONFIRM WHO SOMEBODY IS]`.

You may hold one account `[/ more than one account]`. Do not impersonate anybody
else, and do not create an account to evade a suspension.

## Acceptable use

The operator sets the rules of this community. `[STATE THEM, OR LINK TO THEM.
This is the section a member will actually read, so write it in your own words
about your own community rather than leaving a generic list.]`

At a minimum, do not use this server to:

- post content that is illegal where this server is run or where you are;
- harass, threaten, or abuse another person;
- post sexual content involving children, in any form;
- incite violence against anybody;
- post another person's private information without their consent;
- distribute malware, or attempt to compromise this server or anyone on it;
- send bulk unsolicited messages;
- evade a moderation decision made against you.

Automated access is `[ALLOWED WITHIN THESE LIMITS / NOT ALLOWED]`. The server
has an HTTP API and bots are a normal use of it; what is not acceptable is using
it to overwhelm the server or to collect information about members in bulk.

## Moderation

This server has moderators. `[SAY WHO THEY ARE, OR HOW THEY ARE APPOINTED.]`

They can:

- **remove a message**, which blanks its content while leaving the message in
  place, so the conversation around it stays intact;
- **mark media as sensitive**, so it is obscured until a viewer chooses to see
  it — an uploader can mark and unmark their own, a moderator's decision is not
  the uploader's to undo;
- **restrict posting**, in one channel or across the server, which lets you keep
  reading while stopping you writing;
- **ban you from a channel**, permanently or until a stated time, which does not
  affect any other channel;
- **suspend your account**, permanently or until a stated time, which stops you
  signing in at all.

A ban, a restriction and a suspension each require a reason to be recorded. You
are entitled to that reason.

The server may also have a word filter configured by the operator. It matches a
list of words literally. Depending on how the operator set it, a matching
message is flagged for a moderator to look at, has the matching words masked, or
is refused. A refusal does not say which word matched. The filter has no
judgement — it cannot tell a slur from a discussion of one — so what it mostly
does is decide what a human is shown.

Every moderation action is recorded in the server's audit log.

`[STATE HOW SOMEBODY APPEALS A DECISION, TO WHOM, AND HOW LONG YOU TAKE.]`

## Your content

**You keep ownership of what you post.** Nothing here transfers copyright in
your messages, your files, or anything else you write to the operator.

By posting, you grant the operator the permission needed to run the service:
storing your content, showing it to the people you sent it to, backing it up,
indexing it so it can be searched, and generating previews of files you upload.
That permission is limited to operating this server and ends when the content is
removed, except in backups until those are rotated out.

You confirm that you have the right to post what you post.

**What removal actually does.** Deleting a message blanks its content. The
message itself stays in place, because the log is append-only and pulling
entries out of it would break the conversations other people are part of. A copy
may also survive in a report, if somebody reported the message before you
removed it, and in backups until those expire. Understand this before you post
something you would need to be able to unsay.

**Content set to expire.** If you give a message a lifetime, or a channel has a
retention period, the content is blanked when it expires. That is the same
mechanism as a deletion, and the same limits apply.

## The operator's content

`[ANYTHING THE OPERATOR POSTS — the server's name, logo, pinned rules, uploaded
branding — STAYS THE OPERATOR'S. Say so here if it matters to you.]`

## The software, which is separate

TDC — the software this server runs — is open source under the Apache License,
Version 2.0. You can read it, run it, modify it, and run your own server with
it, subject to that licence. The licence is in the project repository.

That licence covers the software. It does not cover what is on this server.
Messages, files and anything else posted here belong to the people who posted
them and are governed by these terms, not by the software licence.

The people who wrote TDC do not run this server, have no access to it, and are
not party to these terms. They are not responsible for anything that happens
here. Do not contact them about this server.

## Availability

This server is run `[on a best-effort basis / to the following standard]`. There
is no uptime guarantee `[unless you are offering one — if you are, state it and
mean it]`.

The operator may take the server down for maintenance, upgrade it, change what
features are switched on, or stop running it altogether.

`[SAY HOW MUCH NOTICE YOU WILL GIVE IF YOU SHUT DOWN, AND WHETHER PEOPLE WILL BE
ABLE TO GET THEIR DATA OUT. Note that the software has no export feature, so if
you promise this, it is you doing it by hand.]`

## Ending your use

You may stop using this server at any time.

The software has no self-service account deletion. `[SAY HOW SOMEBODY ASKS YOU
TO DELETE THEIR ACCOUNT, AND WHAT YOU WILL ACTUALLY DO. Deleting an account does
not remove the messages that account sent — see the privacy policy.]`

The operator may suspend or end your account for breaking these terms, or
`[STATE ANY OTHER GROUNDS — leaving the organisation, a period of inactivity]`.
Where it is possible to give notice and a reason, one will be given.

## No warranty

This server is provided as it is, without warranty of any kind. To the extent
the law allows, the operator makes no promise that it will be available, that it
will be free of errors, that it is secure against every attack, or that anything
stored on it will not be lost.

Back up anything you cannot afford to lose. The operator's backups exist to
recover the server, not to recover your individual files.

## Limitation of liability

`[THIS SECTION NEEDS A LAWYER. What can be limited, and how, differs by
jurisdiction, and a clause copied from an American service will not necessarily
be enforceable where you are. In many places you cannot limit liability for
death, personal injury, fraud or gross negligence at all, and a clause that
tries to will be struck out — sometimes along with the rest of the section.

Write it for where you are, and do not claim more protection than your law will
give you.]`

To the extent the law allows, the operator is not liable for indirect or
consequential loss arising from your use of this server, including lost data,
lost business, or anything another member of this server does or posts.

Nothing in these terms limits liability that cannot lawfully be limited.

## Other members' content

Content on this server is posted by the people using it. The operator does not
review it before it appears, and does not endorse it.

If something here breaks these terms, report it. There is a report button on
every message. `[ADD ANY OTHER ROUTE — an email address, a moderator to
contact.]`

## Governing law

These terms are governed by the law of `[JURISDICTION]`, and disputes are
subject to the courts of `[JURISDICTION]`.

`[For a server run in Tanzania for a Tanzanian community, this is likely to be
Tanzania. Consider whether that is realistic for members who are elsewhere, and
note that consumer protection law in some countries overrides a choice of
foreign law regardless of what an agreement says.]`

## Changes to these terms

`[SAY HOW YOU WILL TELL PEOPLE WHEN THIS CHANGES AND HOW MUCH NOTICE THEY GET.
Continuing to use the server after a change is usually treated as accepting it,
but that only holds if people were actually told.]`

## Contact

`[EMAIL ADDRESS]`
