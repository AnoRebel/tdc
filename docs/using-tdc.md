# Using TDC

What the application does, from the point of view of somebody using it rather
than running it. Everything here is in the software today; where a capability
depends on a choice your server's operator made, that is said.

There are two applications and they are the same application. The web client
runs in a browser and installs to a phone or a desktop like any other app; the
desktop client wraps the same interface in a native window and adds the few
things a browser will not do. What you read and type is the same code in both,
so they cannot drift apart.

## Getting an account

How you get one depends on your server's registration policy. It may be open to
anybody, restricted to people holding an invitation, restricted to particular
email domains, or closed entirely, in which case the operator makes accounts.

![The sign-in page: a username or email and a password, a Sign in button, and a Create one link for somebody without an account](images/using-tdc/sign-in.webp)

If you were sent an invitation, it is either a link or a short code, and either
one is entered during registration. An invitation may expire, may be usable a
limited number of times, and may be bound to one email address so that
forwarding it to somebody else does not work.

If an invitation does not work, the server will not tell you why. Expired,
already used up, revoked, meant for somebody else and never having existed all
produce the same refusal, because a response that distinguished them would tell
somebody guessing codes when they had guessed right.

**A password reset needs a confirmed email address, and most accounts do not
have one.** Where the server sends mail and your address has been confirmed,
"forgotten your password" sends a link good once and for an hour. Setting a new
password signs every device out and does not sign you in — you sign in with it,
which is what proves you know it.

Confirming is yours to do, from your settings: give an address, a link arrives,
and using it proves the address is yours. Until you do, an account that has lost
its password needs an administrator — so it is worth doing before you need it.
Choose a password you will keep as well, and keep it somewhere.

Changing the address later keeps the old one working until the new one is
proved, so a typo costs nothing.

Beyond a password, there are three other ways in, depending on what the server
offers:

- **A passkey** — your fingerprint, face or device PIN, with the key itself
  staying on your device. Sign in with nothing typed at all. Offered wherever the
  server has a settled address; add one from your settings.
- **Another service** — GitHub, GitLab, a self-hosted Forgejo or Keycloak,
  whichever the operator has configured. The buttons appear on the sign-in form
  when there are any.
- **Two-step sign-in** — something as well as your password. A passkey counts:
  if you have one, signing in with your password asks for it as the second step
  and there is nothing else to set up. Otherwise it is a code from an
  authenticator app, which comes with ten recovery codes, shown once, for the
  day you cannot reach your phone. Where the server sends mail and your address
  is confirmed, a code can also be sent there — a fallback for a phone in
  another room, and the weakest of the options, since it is worth what your
  mailbox is worth. An operator may require two-step sign-in of administrators
  or of everybody.

Recovery codes are not a password reset: they get you past the second step, not
past a forgotten password.

### Your sessions

Every device you sign in on is a session, and you can see all of them: when each
was created, when it was last used, and which one you are looking at. Any of
them can be revoked, which signs that device out immediately.

![Signed-in devices in settings, listing one session marked This device with when it was created](images/using-tdc/sessions.webp)

Ending a session asks first and says what will happen, because signing a device
out is not something to do by accident.

Changing your password invalidates every other session. That is the behaviour
you want if you are changing it because you think somebody else has it.

### Your name

You have two names, and both are changed from **Your name** in your settings.

Your **display name** is what people read beside your messages and in member
lists. Change it whenever you like; it saves at once, and others see it the next
time a list or a profile is loaded. Leave it empty to show your username. Any
script is fine, emoji included. What is refused is only what can deceive: an
invisible or right-to-left character hidden inside a name, a control character,
or a name that is one of the server's reserved words — "Admin", "System",
"Moderator" — however it is spelt. The limit is 64 characters.

Your **username** is the first half of your address, `you@server`, and it is
what links, mentions and other servers know you by. So changing it is allowed,
but with care:

- As you type a new one, the field says whether it is free. A name that is
  taken, reserved, or was given up by somebody else recently is not.
- Changing it asks you to confirm. It shows your new address and what will and
  will not change, and asks for your password. If your account has no password
  — you sign in with a passkey or another service — it asks instead that you
  have signed in within the last 15 minutes; sign out and in again first.
- Your **old name stays yours** for a while (90 days unless your server says
  otherwise): nobody else can register it or take it. Links to your old profile
  keep finding you, and you can still sign in with the old name until somebody
  else takes it.
- **Messages you already sent keep the address they were sent under.** Every
  message is signed with its sender's address, and rewriting it would break the
  signature, so old messages show the old name. They are still yours: you can
  edit and delete them as before.
- Mentions are text. A message that said `@oldname` still says it; a new
  mention needs your new name.
- You can change it again after a wait (30 days unless your server says
  otherwise), so a name cannot be rotated to dodge a block or confuse people
  about who is who.
- **It cannot be changed while you are in a conversation shared with another
  server.** Other servers know you by your address, and the federation protocol
  has no way to tell them it changed — they would keep you as a member under a
  name that no longer exists here. Leave those conversations first; the
  settings page says so when this is what is in the way.

An operator can turn username changes off entirely, in which case the section
says so. Every change, of either name, is in the audit log, and administrators
see the names somebody had before beside their account in the list of people.

Things outside the server that hold your old name keep holding it: a passkey
shows it on your device until you replace it (it still works), and a ringtone
somebody chose for you is set against your old address.

## Conversations

### Channels

A channel is a named conversation. It may be **public**, which anybody on the
server can find and join, or **private**, which only its members can see.

![The golang channel open beside the sidebar, which lists channels and direct messages with unread counts; messages show reactions and a 4 replies link](images/using-tdc/channel.webp)

A private channel is genuinely invisible rather than merely locked. Somebody who
is not a member cannot tell the difference between a channel they are not
allowed into and one that does not exist, anywhere in the application or the
API. That is deliberate — a refusal that distinguished the two would confirm
that the channel is there.

Channels can be archived, which keeps all the history readable and refuses new
messages, and restored, which resumes posting. Nothing is lost either way. An
archived channel says so beside its name, for everybody in it.

Whoever looks after a channel finds its settings behind the gear in the
channel's header — its name and topic, whether it is public or private,
archiving and restoring, and who is in it. The gear appears only for somebody
who may change something there, and each part of the settings only for what
they may change. Every change appears in the channel as it happens, for
everybody who has it open. A direct conversation has no such settings: it
belongs to the people in it.

![Settings for the General channel: its name and topic, Make private, Archive, and the list of members each with a Remove button](images/using-tdc/channel-settings.webp)

Creating a channel takes a permission. On most servers ordinary members have it;
on some the operator keeps it.

### Direct messages

A direct conversation is between you and one other person. Start one by finding
them in the search box — `Ctrl K` — and choosing them; there is no separate "new
message" button.

![A direct conversation between Neema and Amani, with their messages on either side](images/using-tdc/direct-message.webp)

The server can hold a conversation between several people, and the API accepts
it, but no control in either client opens one yet. Group direct messages are in
practice not yet available.

**Administrators cannot read them.** Not "should not" — an administrator using
the ordinary endpoints cannot retrieve other people's direct messages, and that
is enforced on the server rather than being a rule somebody might not follow.

You control who may open one with you: **anyone**, **only people who share a
channel with you**, or **nobody**. Narrowing it does not close the conversations
you already have. Somebody refused is not told which setting refused them, and
moderators can still reach you, because a moderation problem you can make
unreachable is not one anybody can deal with.

### Threads

Any message can become a thread. Replies live in a panel beside the
conversation rather than in the main timeline, so a long tangent does not bury
everything else, and the thread inherits the channel's permissions and
visibility — there is no way for a thread to be more open than the channel it is
in.

The conversation shows how many replies each thread has, so you can see what is
worth opening without opening it.

![A thread open in a panel beside the golang channel, showing the original question and four replies](images/using-tdc/thread.webp)

### Disposable chats

A conversation that deletes its own contents after a period the creator sets.
The expiry is enforced by the server, so a client that was offline through the
whole lifetime of a message never receives it — the content is gone, not merely
hidden by whatever happened to be displaying it.

## Messages

Writing, editing and deleting all work as you would expect, with two things
worth knowing.

**An edited message says it was edited.** Not the old text, but a marker that
the message is not what was originally sent.

**A deleted message leaves a placeholder.** The content goes; evidence that
something was there and was removed stays. A conversation where messages can
silently cease to have existed is one nobody can trust, and quoting somebody who
can then make the original disappear is the specific problem this avoids.

### What you can do to a message

- **React** to it with an emoji, from the picker. The picker is searchable, and
  carries a GIF tab as well where your operator has configured a provider —
  sending a GIF works regardless, since it uploads like any other file.
- **Reply in a thread**, which is what turns it into one.
- **Pin** it, if you have the permission. Pinned messages are the channel's,
  listed together, and pinning speaks for the channel rather than for you — so
  it takes a permission where reading what is pinned does not.
- **Forward** it to another conversation.
- **Report** it, if it needs a moderator.
- **Give it an expiry** — see below.

Right-clicking a message opens these, and so does a long press on a touch
screen. Everything in that menu is reachable another way too: a context menu is
a shortcut, never the only route to something.

![The menu opened by right-clicking a message: Copy text, Reply in thread, Forward, Pin to channel, Select, Create an issue from this, Expire this message, Report, Block and Ban](images/using-tdc/message-menu.webp)

### Writing now and sending later

A message can be written now and sent at a chosen time — the clock in the
composer, which offers a few presets and a picker. At least ten seconds ahead
and at most a year. Whether this is available at all is one of the capabilities
your operator can switch off.

Reviewing and cancelling what you have scheduled is in the server and the API
but has no control in either client yet, so a scheduled message currently cannot
be called back.

### Keeping a conversation to hand

Pinning conversations to the top of your own list — a private bookmark rather
than an announcement — is built on the server but has no control in either
client yet. Pinning a _message_ is the other thing entirely, that one speaks for
the channel, and it works.

### Messages that expire

You can put a lifetime on a single message, in a conversation that otherwise
keeps everything. Three ways to reckon it:

- **A fixed time from sending** — it goes after that period whether or not
  anybody read it.
- **Once everybody has seen it** — it goes as soon as every other participant
  has read it.
- **A period after each person first sees it** — everybody gets the same window
  from their own first reading, and it goes when the last of those runs out.

Only the person who sent a message may set this, or a moderator. And a lifetime
can be **shortened but never extended**. Somebody who sent something meaning it
to disappear in an hour should not find that it now lasts a week.

When an expiring message goes, its attachments go with it.

### Messages that ask you something

A message from a program may carry buttons — "Deploy", "Not yet", that sort of
thing. Pressing one tells the program what you chose; it decides what happens
next, because it is the only thing that knows.

They are ordinary buttons. You can reach them by pressing Tab, press them with
Enter or Space, and a screen reader announces them as buttons and says what
happened after you press. You need to be in the channel to answer — reading one
is not the same as taking part in it.

Some are offered once. A question put to a channel is usually answered once by
anybody, so the second press is refused and says so rather than quietly
counting twice. Some have a deadline, and stay visible but stop working when it
passes — a button that vanished would leave you wondering whether you had
imagined it.

A button that opens a link says so with an icon, and opens it in a new tab. TDC
only lets a message offer `http` and `https` links; anything else is refused
when the message is posted, so a button in a message cannot be a way to run
something on your machine.

#### Menus, fields, and answers only you can see

Some questions are not yes or no. A program may offer a **menu** — pick one of
a list, and your choice is sent as soon as you make it — or a short **field** to
type into, sent with Enter or the arrow beside it. A screen reader announces a
menu as a list of choices and a field by the label written above it.

What you choose or type is checked before the program sees it: a menu only
accepts one of its own choices, and a field only as much text as it asked for.
Some controls are for particular people — approving a deployment, say — and
refuse anybody without the right permission, saying so, without the program
ever hearing that you tried.

A program may answer by changing the message itself, so everybody sees the
question has been settled. If it changes while you are answering, your answer
is refused and you are asked to look again, rather than being counted against
a question you were never shown.

It may also answer **you alone**. That answer appears under the message, marked
"Only you can see this", and nobody else in the channel receives it. It is not
kept: close the conversation and it is gone, and if you were not connected when
it was sent you will not see it later. Anything a program needs you to be sure
to see, it posts as an ordinary message.

### Messages from programs

A message posted by a program rather than a person says so beside its name:
**Webhook** for an outside system posting into the channel — a build server, an
alerting tool — and **Bot** for an application taking part in the
conversation. The marking is the server's, not the program's, so a program
cannot leave it off and pretend to be somebody. You can reply to, react to,
report and (if you moderate) remove these messages exactly as anybody else's.

![Two messages from programs in the nuxt channel: one from Build server marked Webhook with a See the build link, and one from Deploy bot marked Bot with Deploy, Not yet and See the changes buttons](images/using-tdc/program-messages.webp)

### Formatting

A message is written in Markdown — the same marks GitHub and Discord
use — and shown formatted. What you type is what is stored; the formatting is
how it is drawn.

| Type                     | To get                                 |
| ------------------------ | -------------------------------------- |
| `**bold**` or `__bold__` | **bold**                               |
| `_italic_` or `*italic*` | _italic_                               |
| `~~struck~~`             | ~~struck~~ (two tildes; one is just ~) |
| `` `code` ``             | `code`                                 |
| ` ```go ` … ` ``` `      | a code block, labelled and highlighted |
| `> quoted`               | a quote                                |
| `- item` or `* item`     | a list                                 |
| `1. item`                | a numbered list                        |
| `[text](https://…)`      | a link with your own words             |

A **code block** keeps every space and line exactly as typed, scrolls sideways
rather than wrapping a long line, says which language it is when you name one
after the opening fence, and has a **Copy** button. Colours for the common
languages (Go, JavaScript, TypeScript, Python, Rust, shell, SQL, JSON, YAML, HTML,
CSS and others) load the first time a labelled block is on screen.

Some Markdown is deliberately left as text, because in a conversation it is
typed by accident far more often than on purpose: a heading (`# …`) is drawn as
a bold line rather than a title; tables, horizontal rules (`---`), four-space
indented code and HTML are shown exactly as typed. HTML is never interpreted —
`<b>` reads as the three characters you typed. An image written in Markdown is a
link to the picture rather than the picture, since attaching it is how an image
is sent.

**In the message box**, select some text and press:

| Keys           | Wraps it in                    |
| -------------- | ------------------------------ |
| `Ctrl B`       | bold                           |
| `Ctrl I`       | italic                         |
| `Ctrl Shift X` | strikethrough                  |
| `Ctrl E`       | inline code                    |
| `Ctrl Shift C` | a code block, on its own lines |

Pressing the same keys on text that is already wrapped unwraps it. With nothing
selected, the marks go in as a pair with the cursor between them. The **T**
button beside the emoji button shows a row of the same as buttons, with a short
reference to the syntax; whether that row is showing is remembered in this
browser.

`Enter` sends and `Shift Enter` starts a new line, as always — except inside a
code block you have opened and not yet closed, where `Enter` starts a new line
too, so code can be typed or pasted without sending half of it. Type the closing
` ``` ` and `Enter` sends again.

Wherever a message cannot be shown formatted — a notification, a push to your
phone, a desktop banner, a search result, the pinned list — its words are shown
without the marks: `**deploy** is done` reads _deploy is done_. A mention inside
code (`` `@amina` ``) does not notify anybody; one made bold does.

If you would rather see every message exactly as it was typed, switch off **Show
formatting in messages** in your interface preferences (next to motion and
density). Links are still found either way.

### Links

Links, addresses and phone numbers in a message are detected and made
clickable, as they always have been — except inside code, where a URL is an
example rather than a link. A Markdown link (`[text](address)`) may point only
to a web or mail address; anything else is shown as its text. Every link opens
in a new tab and carries no referrer back.

Nothing you can type into a message becomes markup the application did not
mean to produce: raw HTML is escaped, the Markdown is parsed into a small set of
elements, and a sanitiser removes anything outside that set before it reaches
the page.

Markdown is rendered in full for text files somebody attaches, where a README
arrives looking like a README rather than like its source.

A link posted in a conversation may be shown with a preview of what it points
at. That depends on two things: whether your operator has link previews on at
all, and whether you want them — there is a setting of your own. With either
off, the server makes no outbound request for your links.

## Files

Files are uploaded to the conversation and then sent with a message, which is
why a refused upload costs you nothing but the upload and a message that fails
to send does not lose the file.

![An image sent in the General channel with a short caption, shown inline in the conversation with a reaction under it](images/using-tdc/files.webp)

While a file is uploading you see real progress — the actual transfer, not an
animation — and you can cancel it, or retry without choosing the file again. The
conversation stays usable throughout.

The server enforces its own size and type limits regardless of what the client
checked, and a refusal names the limit you exceeded rather than failing vaguely.
A file's type is determined from its contents rather than from its name or what
the uploader claimed, and everything is served in a way that cannot execute in
the application.

**Images are reduced before sending by default**, and you can choose to send the
original instead. The application says which it is sending, and tells you if the
original is too large for the server's limit.

Each file can carry its own caption, so several sent together can each say
something different.

Images, animated images, video, audio and PDFs open in a viewer over the
conversation, with the keyboard to dismiss it and to move between the files on
one message. A type that cannot be displayed still shows its name, size and type
with a download.

### Sensitive media

Anybody can mark their own upload as sensitive, and a moderator can mark
anybody's. Marked media is covered until the viewer chooses to see it. Whether
covering happens at all is the operator's policy rather than each client's, so
that the same server behaves the same way in every client.

## Knowing what happened

### Presence and status

Whether somebody is around is derived from whether they are connected, so it is
right without anybody maintaining it. There are two states, **online** and
**offline** — a connection that drops without saying goodbye reads as offline
within a couple of minutes rather than leaving somebody apparently present for
ever.

Separately from that, you can set a status: **Available**, **Busy**, **Away** or
**Appear offline**. Busy and Appear offline withhold notifications. These four
are seeded when a server is created and there is no interface for changing the
list, so in practice they are what every server has.

![The Status setting with four choices: Available, Busy, Away and Appear offline](images/using-tdc/status.webp)

The server supports giving a status an expiry so that it reverts by itself, and
supports appearing offline while connected, but neither has a control in the
clients yet.

### What a member list says about you

A channel's members can see who else is in it. Your **name and address are
always shown** — they are how a message is attributed and a conversation is
opened, so a list without them is not a member list.

The three details past that are yours, under Settings → Privacy:

- **Whether you are online.** Turning it off does not disconnect you or change
  what you receive; it stops the server answering the question.
- **Your status**, along with the note that goes with it. One setting for both,
  because a status without its note ("Away", with no "back at four") gives away
  the fact while withholding the part that made it useful.
- **When you were last here.** The most revealing of the three — a last-seen
  time is a record of when you work and when you stop — and separate from
  presence, because "online now" and "last here at 03:12" are different things
  to disclose.

All three are shown by default, and each is a separate choice: hiding when you
were last here does not hide whether you are online now.

![Privacy settings: who may start a conversation with you, who may call you, and three switches for whether you are online, your status, and when you were last here](images/using-tdc/privacy.webp)

**A detail you turn off is left out of what the server sends**, rather than sent
and hidden by the application. This is the difference that matters: a detail
that reaches the client has been disclosed whatever the client then does with
it, and anybody who can read a network response can read it. That holds
everywhere the server answers these questions: the member list, and the online
count a conversation shows, which leaves you out of it rather than counting you
as offline.

These are standing choices, and not the same as _Appear offline_. Appearing
offline is about this afternoon and lapses when you turn it off; these hold
until you change them.

**A moderator reviewing a report can see what you have turned off**, and every
time one does it is written to the audit log. A report about somebody's status
cannot be judged without reading it, so the answer is not to refuse the access
but to account for it — which is what lets you see that it happened.

### Typing indicators

You see who is composing, and the indicator disappears by itself when it stops
being refreshed rather than sticking around after somebody wandered off
mid-sentence.

The server honours a per-person preference not to send yours, and respects it
wherever it is set — but there is no control for it in either client yet, so in
practice everybody sends one today.

### Unread state

Unread state is kept on your account rather than in each browser, so it converges
across every device you are signed in on. Reading something on your phone clears
it on your laptop, and a device that was offline catches up when it reconnects.

Convergence happens when the other device next asks rather than being pushed
along the live connection, so the second device catches up within moments rather
than instantaneously.

### Notifications

You are notified about direct messages and mentions of you, about reactions,
replies and forwards of your messages, and about a message being pinned or
somebody joining. A reply notifies whoever started the thread. Never your own
actions.

![The notification list opened from the bell at the bottom of the sidebar, with one entry per conversation](images/using-tdc/notifications.webp)

Some things happen without being asked for, because they are what make
notifications bearable:

- Nothing is raised for the conversation you are currently looking at.
- A notification is withdrawn when you read the thing elsewhere.
- A burst of messages arrives as one batched notification rather than one each.
  Your notification list shows one entry per conversation with a count, and
  your devices are told about the first message of a burst and not again until
  you have read the conversation or a minute has passed. Your operator chooses
  that window, and can turn it off.
- A status that suppresses notifications suppresses them.

Notifications also reach you with the browser closed, where your operator has
configured that. **You can choose to hide the content** in those, so a push for
a private conversation says that something arrived without saying what. That
switch is in your settings, and it fails closed — a push it cannot be sure about
conceals the sender rather than guessing.

#### A summary instead of interruptions

If you would rather not be interrupted at all, you can ask for **a summary by
email** instead, daily or weekly, at an hour you choose. It is in your settings
under _Summaries by email_, and it is off until you turn it on.

- **The hour is yours.** "Daily at 08:00" means eight in the morning where you
  are, taken from your device's time zone when you first choose it and shown
  beside the setting. If you move, the setting offers to switch to the zone
  your device is now in.
- **It replaces the interruptions.** While it is on, your devices are not
  notified. Everything still appears in your notification list and still counts
  as unread, which is what the summary is made from.
- **It counts; it never quotes.** A summary lists each conversation with
  something unread — how many notifications, from how many people, and how many
  were addressed to you — with a link to open it. What anybody wrote is not in
  the mail, because mail leaves this server.
- **Only what you can still read.** Each summary is checked when it is sent: a
  channel you have left, been removed from or muted since is left out, counts
  and all. If nothing is left, no mail is sent.
- **It waits for you.** If your status says not to interrupt you when a summary
  is due, it is held and sent once that changes, with nothing lost.
- **It goes only to a confirmed address**, like account recovery. Confirm one
  in your settings first.

The control appears only where it can work. A server whose operator has not
configured mail cannot send a summary, and does not offer one.

Muting a single conversation is in the server and the API but has no control in
either client yet, and it is a plain on or off rather than a duration. Following
a thread you were not part of is not implemented.

### Search

One box searches messages, channels and people together, rather than making you
choose first and search three times.

![The search box with meetup typed in, listing matching channels and then matching messages with the word highlighted](images/using-tdc/search.webp)

Results are ordered by recency with the matching part highlighted. Typing
partial words works — searching for the beginning of a word finds it.

**Search never returns anything you cannot otherwise read.** Messages from
channels you are not in do not appear, and a channel you could not discover is
not returned.

There is no search syntax to learn, and none to remember. There is no `from:`,
no `in:`, no date filter — type what you are looking for. Whatever you type is
treated as words to find rather than as a query language, so nothing you can
write produces an error.

## Invitations

Inviting somebody to a channel takes a permission, and being a member of that
channel is not enough on its own — an invitation admits somebody to a
conversation, and that is treated as the channel's decision rather than any
member's. The permission is granted at server level, so on most servers this is
something moderators and administrators do.

An invitation is a link, a short code, or both. The code is opt-in, and exists
for reading aloud.

Every invitation can carry an expiry, a limit on how many times it may be used,
and a binding to one person so that forwarding it achieves nothing.

Revoking one takes effect immediately. Somebody already holding the link cannot
be un-sent it, so the only withdrawal that means anything is the server refusing
to honour it.

A channel invitation admits somebody to that channel and nothing else.

Pasting an invitation link somewhere that renders previews shows an invitation
to the server and never names the channel — an invitation posted into a group
chat is previewed for everybody in it, not only the person being invited. It
also previews identically whether it is valid, revoked or invented, so nobody
can test invitations by pasting them.

## The directory

Where the operator has switched it on, **Directory** appears in the sidebar. It
lists people on this server who chose to be found, and the companies they
agreed to be listed by. [The directory](directory.md) describes all of it; the
short version:

- **Nobody is in it unless they put themselves there.** Without a directory
  profile you are not listed, and nothing else about your account changes.
- **Your profile has three visibility settings** — for the profile as a whole,
  for what you wrote, and for your contact details — each _Anyone_, _Members_
  or _Only you_. Contact details start as _Only you_. A section is never more
  visible than the profile it is in.
- **It does not touch your privacy settings.** Having a public profile does not
  make you visible as online, and hiding your presence does not take you out of
  the directory. They are separate decisions.
- **You can delete your profile** at any time without deleting your account,
  and **export it** as a file you can bring back later or elsewhere.
- **Whether you take enquiries is a setting on your profile**, separate from
  who may send you direct messages. An enquiry is one message, which opens a
  private conversation the sender cannot add to until you take it up.
- **Taking up an enquiry makes it an engagement**, with a private conversation
  and a recorded state you both see. Either of you can withdraw before an
  agreement.
- **A proposal is versioned and an agreement needs you both.** Asking for a
  change to one line gets you a new version, with the old one kept as it was;
  the agreement is recorded only when you both accept the same version, and
  cannot be changed afterwards. Either of you can export it. The server keeps
  the record and nothing more: it holds no money and settles no disputes.
- **Finished work needs both of you too.** One says it is complete; it stays
  outstanding until the other confirms.

### From a profile to an agreement

Two people who have never met: Amani, a developer, and Baraka, who needs work
done. Each has an account on the server (**Create one** on the sign-in page)
and the operator has switched the directory on.

![Amani's directory profile as Baraka sees it: headline, location, available for work, skills, about, experience and the company he is listed by](images/directory/person.webp)

**Amani makes a profile.**

1. Open **Directory** in the sidebar, then **Your profile**. It says _You have
   no directory profile_, and nothing about the account changes until one is
   made.
2. Write a **Headline** — one line on what you do — and a **Location**. Add
   skills by typing in **Find a skill** and choosing one; "golang" finds Go.
3. Choose who sees it. **Who can find you** and **Who sees what you wrote**
   are each _Anyone_, _Members_ or _Only you_. **Who sees your contact
   details** starts at _Only you_, and can stay there: people can still reach
   you through the directory without it.
4. **Take enquiries through my profile** is on for a public profile unless you
   turn it off.
5. **Create profile.** _Your profile is created._ appears, and you are in the
   directory for the people you chose.

**Baraka finds Amani and asks.**

6. Open **Directory**. Search by **Skill**, **Location**, a name or words from
   a headline; tick **Available for work only** to see only people who said
   so. Results show only what each person let you see.
7. Open Amani's profile. Under _Send Amani an enquiry_, give a **Subject** and
   describe **The work** — what, roughly when, and anything Amani needs to
   decide. **Send enquiry.** It is one message: you cannot add to it until
   Amani answers. It appears under **Enquiries** as _Waiting for an answer_.

**Amani takes it up.**

8. Amani is notified like any direct message. Under **Directory →
   Enquiries**, **Take it up** turns the enquiry into an engagement and opens
   its page; **Decline** answers it without one. Both of you now find it under
   **Engagements**, and its **Conversation** tab is a private conversation
   between the two of you.

**Agreeing on the work.**

9. On the engagement's **Proposal** tab, either of you chooses **Write a
   proposal**: **What the work is**, **When**, a **Currency** (TZS unless you
   change it), and each deliverable with its **Price**, typed the way you
   would write it. **Send the proposal.**
10. The other person can **Accept**, **Decline**, or **Ask for a change** to
    one part — the work, the timeline, the currency, or one deliverable — and
    say what they would like instead. The person who sent the proposal is
    notified.
11. Answering a request is **Send a new version**: the form starts from the
    latest version, so you change only what was asked about. Every version
    stays readable under **Earlier versions**, with what changed marked.
12. When one of you accepts, the page says the agreement waits for the other.
    When **both of you accept the same version**, the agreement is recorded,
    with both acceptance times. Nobody can change it after that.
13. Either of you can **Export the agreement** as a file. You both get the
    same file.

**Finishing.**

14. Either of you chooses **Start the work**. When it is done, one of you
    chooses **The work is complete: ask … to confirm**, with a note if you
    like; the other sees it and chooses **Confirm the work is complete**.
    Until they do, it shows as outstanding to both of you. The engagement then shows _Completed_, with every step and
    who took it under **History**.

Either of you can **Withdraw** at any point before an agreement, with a reason
the other sees.

**What an agreement record is, and is not.** It is a record, kept by this
server, of what the two of you agreed and when, which either of you can refer
to or export. **The server and whoever runs it are not a party to it.** They do
not hold or move money, offer escrow or payment, guarantee that either of you
will do what was agreed, or settle disputes about it, and no service that does
any of those is connected. Every agreement, on screen and exported, carries
that statement in the same words. Paying, and resolving a disagreement, are
between the two of you.

- **A company can ask to list you; only you can accept**, from your profile
  page. Leave, and it is gone from both the company's page and your profile at
  once.

## When something is wrong

### Blocking

Blocking somebody is a personal boundary rather than an exercise of authority,
so it needs no permission and **the person blocked is never told**. Moderators
keep their full view, because a moderation problem that can be hidden from
moderators is not one anybody can deal with.

### Reporting

Anybody signed in can file a report, including about something they saw in a
channel they have since left. **The person reported never learns who reported
them.** Reports are rate limited, for the obvious reason.

### What moderators can do

Remove a message, or edit one — an edit by a moderator is shown as theirs, not
the author's, and marked as a moderator's. Suspend an account, which
invalidates its sessions immediately. Restrict somebody from posting,
temporarily or permanently. Ban somebody from one channel, which is confined to
that channel and leaves their other memberships alone.

Look after a channel: rename it or change its topic, archive and restore it,
make it private, and take somebody out of it. Each is its own permission, and
each can be given for one channel alone, so a channel can have keepers who have
no say anywhere else. Making a channel **public** needs that permission for the
whole server, because it opens everything ever said in the channel to everybody
on it — the settings ask before doing it. Taking somebody out of a channel is
not a ban: their client is told at once and stops receiving the channel, and
they can join a public channel again whenever they like; a private one needs an
invitation, as it did the first time.

Everything on that list is recorded in the audit log, with its reason and its
duration where it has one, and everything on it is reversible. Temporary
restrictions expire by themselves rather than needing somebody to remember.

Your operator may also have a word filter configured, which can flag a message
for a human, mask the word, or block the message outright. It is off unless
somebody set it up.

Where the directory is switched on, three more permissions may be handed out,
each to whoever the community trusts with that judgement: moderating profiles,
reviewing proposed skills, and verifying claims. [The directory's moderation
section](directory.md#moderation) is the guide for all three — what each
allows, how to approve, merge or reject a skill, how to verify and revoke a
claim, and the audit entry each leaves.

### Deciding who may do what

Whoever may make roles or hand them out finds **Roles and permissions** under
Administration in your settings. It lists every role with how many people hold
it and what it allows. Open one to rename it, describe it, delete it — the
server keeps `Member` and `Administrator` — or set each permission to
**Allow**, **Deny** or **Unset**. Beside each choice is what it will mean:
whether `Member` already gives it to everybody, that a denial wins over every
other role, or that a role which administers the server already has it. The
people holding a role are listed underneath, with a search to add somebody.

To see what one person may do, search for them on the roles screen, or open
**Roles** beside them in the people list. Every permission is listed with the
role that decided it, across the server or in one channel you pick. Their roles
can be given and taken away from there too.

A channel's own settings show **Who may do what here** to the same people: what
each role is allowed or denied in that channel alone. That is how a channel
gets keepers who have no say anywhere else, and how an announcement channel is
made. Deny posting to `Member` there, and allow it to the role whoever
announces holds.

If a change would take away your own authority, you are asked first. One that
would leave the server with nobody to administer it is refused.

### Messages from other servers

If your server federates, some messages in a room come from people on other
servers. Your moderators can act on them exactly as on local ones: remove them,
filter them, and take reports about them. Nothing about a message being foreign
puts it out of reach.

**What your moderators cannot do is make it go away everywhere.** Removing a
message here removes it here. The server it came from keeps its own copy, and
so does every other server in the room, because no server can compel another to
forget something. A protocol that claimed otherwise would be telling your
moderators their decision travelled when it had not.

This works both ways: a moderator on another server can remove one of your
messages from their copy, and your server keeps it.

## Calls

Where your server offers them, any channel or direct conversation can hold a
voice or video call. Calls are off unless the server's operator turns them on,
so if you see no phone button in a conversation's header, that is why.

**Starting one.** The phone button in the header starts a call by voice; you
can turn your camera on once you are in. Everybody in the conversation sees a
bar saying a call is going on and who is in it, with **Join** and **Join with
video**. Your browser asks for your microphone (and camera) the first time;
nothing is sent until you allow it, and nobody is told you have joined until
then either.

![A voice call in the General channel: a bar across the top says you are in the call with neema and juma, and a small call window in the corner shows both people with mute, camera, ring, hold and Leave](images/using-tdc/call.webp)

**Ringing people.** Starting a call in a direct conversation rings the other
person; **Start a video call** does the same with your camera on. In a channel,
the bell in the call window (**Ring people**) lets you ring particular members
into it. A ring reaches every device the person is signed in on, and stops on
all of them when they answer on one. If they decline, you see that for a
moment; if nobody answers within 45 seconds, they get a missed-call
notification. Somebody whose settings do not let you ring them is simply not
rung — you are not told why.

**Being rung.** An incoming call appears as a small window at the edge of the
screen, never over what you are doing; drag it anywhere, or move it from the
keyboard by its grip. It stays while you move between pages, and offers
**Answer**, **Video** (answer with your camera on) and **Decline**. Several
calls at once are listed together, newest first, each with its own buttons.
If you are already in a call, the
window says so and offers **Hold & answer** — the current call waits while you
take the new one — or **End & answer**, with a choice of a few words for the
people you are leaving. **Silence** stops the sound
without declining. When the browser is in the background or closed, a ring
arrives as a notification, where you have allowed notifications; the desktop
application shows a banner, even when closed to the tray.

You can switch between voice and video at any time: answer by voice or with
video, and turn your camera on or off during the call.

**Declining with a message.** The speech-bubble button beside **Decline**
offers quick replies — "Can't talk right now", "I'll call you back soon" and
others — or a box for your own words. The caller sees them at once in their
call window, and they are posted in your conversation with them, where they
stay.

**Several calls at once.** You can be in more than one call, talking in one
and holding the others, as on a phone. Your microphone and camera go only to
the call you are talking in; nothing reaches a held call, and its people see
that you are holding. Held calls are listed in the call window with
**Switch**, which holds the current call and takes you back to that one, and a
button to leave them. **Hold** in the call controls holds the call you are in,
and **Resume** brings it back. Joining another conversation's call while in one
holds the first rather than ending it.

**Merging calls into a conference.** With one call held and another going,
**Merge** on the held call invites its people into the call you are in. Each of
them is asked first, and joins only by accepting; once they have, the held call
ends. People merged in take part in the call and nothing else: they cannot open
its conversation, read its messages or see who else is in it beyond the call.

**Presenting your screen.** The screen button in the call controls shares a
screen, a window or a browser tab — with its sound, where your browser offers
that. Whatever you share takes the stage above everybody's tiles. One person
presents at a time: while somebody is, the button is greyed and says so, and
they stop it themselves (or with the browser's own "Stop sharing"). Somebody who
moderates the conversation can take a presenter off the stage. Sharing needs a
desktop browser; phones generally cannot share their screen.

**Links to a call.** Somebody allowed to (the `call.link` permission, which
an administrator grants) can make a link to a conversation's call from the
link button in the call window: with a label, a lobby where people wait until
somebody in the call lets them in, a passcode, an expiry and a limit on how
many people it lets in. The link is shown once — copy it then. Whoever opens it
sees the conversation's name and who is in the call, signs in if they have not,
and joins the call — only the call: the conversation, its messages and members
stay closed to them. Links already made are listed below the form, with a
button to withdraw each. A link to a call that has not started waits for it.

**Calling from a profile.** The phone and camera buttons on somebody's
directory profile, and beside people in a channel's member list, open your
direct conversation with them — or the one you already have — and ring them.

**Who may call you** is in **Settings → Privacy**: anybody on this server,
only people you share a conversation with, or nobody — you can still join calls
yourself. A separate switch decides whether people on connected servers may
ring you, where your server allows that at all.

**How it sounds** is in **Settings → Ringing**, kept on each device and, in
the desktop application, for each server separately: turn the ringer off,
choose a tone, add your own (an audio file under 400 KB), give particular
people their own tone by address (`name@server`), and, on phones whose browser
supports it, vibrate for incoming calls. The incoming window has a ringer
switch too.

**While you are in it.** The call sits in a small window in the corner, so you
can carry on reading, writing, opening projects — anywhere in the application —
without leaving it. From that window you can:

- **mute** and unmute, and turn your **camera** on and off — everybody sees
  which, on your tile;
- **expand** it to fill the screen, and make it small again;
- **minimize** it to a bar with just your microphone and **Leave**;
- **move** it: drag it by its grip (the dotted handle) anywhere on the screen,
  or focus the grip and use the arrow keys (Shift moves it further). It stays
  where you put it, on this device;
- **picture in picture**: take the call out of the page into a small window
  that stays above your other windows, so you can work in another app. Where
  your browser can only put a single video there, it is the first person's.

Closing the tab or reloading the page ends your part in the call; moving
between pages inside TDC does not. Joining another call holds this one; see
**Several calls at once** above.

**What a call uses.** Calls go directly between the people in them, not
through the server, so what matters is your own connection:

| In a call with   | Voice only (send / receive) | With video (send / receive)  |
| ---------------- | --------------------------- | ---------------------------- |
| one other person | about 0.4 MB a minute each  | about 5–10 MB a minute each  |
| three others     | about 1.2 MB a minute each  | about 15–30 MB a minute each |

Every person in a call sends their own audio and video to every other person,
so each extra person adds to what _you_ send, not only to what you receive. On
a phone, or on a connection that charges for data, prefer voice; TDC warns you
before you join where your device says the connection is metered. A call has a
limit on how many people it can hold, set by your server — four, unless your
operator chose otherwise — and the next person is told the call is full.

Some networks — offices, mobile carriers, some home routers — stop two people
reaching each other directly. Your server's operator can run a relay so those
calls still connect; without one, a call between two such people fails, and
TDC says so.

## Projects

When a conversation decides something needs doing, it can be tracked where it
was decided. **Projects** in the sidebar lists the projects you can see; each
holds issues named by the project's key and a number — `WEB-12` — that never
changes. An issue moves through the states its project defines, and everything
that happens to it is kept in a history nobody can edit.

A message becomes an issue from its own menu — **Create an issue from this** —
and the two stay linked: the issue shows where it came from to anybody who can
read that conversation, and the message shows the issue. Each issue has a
discussion beside it that works like any channel.

Work can be time-boxed in **cycles** and gathered across them into
**milestones**, both optional. A cycle's days are the project's own calendar,
and when one ends with work unfinished the server carries that work forward to
the next cycle or leaves it — never deletes it.

[Projects and issues](projects.md) is the whole guide: starting a project,
configuring its states, filing and moving issues, cycles and milestones, and who
may do what.

![A project's board with columns To do, In progress, In review and Done, each holding issue cards with labels, priority and who is assigned](images/projects/board.webp)

## Making it yours

### Appearance

Light or dark, following your system by default. Two colour palettes ship,
offered as **Cool** and **Warm**, and the accent colour is yours to choose
independently of the palette — pick a colour and the application derives a
readable shade for light and for dark rather than accepting or refusing what you
picked. A colour that cannot be made readable is refused.

![Appearance settings: Follow my device, Light or Dark, the Cool and Warm palettes, and the accent colour](images/using-tdc/appearance.webp)

**Your choice is stored on your account**, so it follows you to another browser
or to the desktop client rather than living in one browser's storage. Your
operator sets the server's default, and clearing your own choice returns you to
it.

![The golang channel in the dark theme](images/using-tdc/dark.webp)

### Language

English and Kiswahili both ship, and your operator sets the server's default.
Switching changes what is on screen: every sentence the interface writes goes
through the translation layer, so a Kiswahili speaker reads a Kiswahili
application rather than an English one with a Kiswahili menu.

What is never translated is what people wrote — messages, channel names, status
text, somebody's display name. Those are shown as they were typed, in whatever
language they were typed in.

### Motion, density and shortcuts

The interface moves a little when something changes: a dialog grows into place,
a new message rises in, a thread panel slides the last few pixels open, a card
glides to its new column on a board, an unread count pops when it appears. Every
one of those is short — nothing takes longer than a quarter of a second — and
nothing waits for it: whatever is appearing can be used from its first frame.

You decide how much of that you want, under **Motion**:

- **Follow my device** — the default. If your operating system is set to reduce
  motion, you get _Reduced_; otherwise _Full_. It keeps following your system
  while TDC is open.
- **Full** — things fade, rise and slide into place.
- **Reduced** — things still fade, so you can see that something changed, but
  nothing slides, grows or bounces, and decorative loops (a shimmering
  placeholder, the dots that say somebody is typing) stay still.
- **Off** — nothing moves at all; every change happens at once.

**Density** makes everything slightly smaller, so more of a conversation fits on
the screen. **Keyboard shortcuts** can be switched off entirely if you keep
setting them off by accident; `Esc` and `Ctrl K` keep working either way.
**Show formatting in messages** is on unless you turn it off; off, every
message is shown as the text that was typed (see [Formatting](#formatting)).

All of these apply the moment you choose them and are kept **in this browser** (or
this desktop app) rather than on your account — you may want motion on a big
screen and not on your phone. Find them in the command palette (`Ctrl K`, then
"Motion and keyboard preferences"), behind **More preferences** in the shortcut
list, and in settings.

### The keyboard

Press `?` for the full list, grouped by where each shortcut works.

![The keyboard shortcut list, grouped under Everywhere and Getting around, each with its keys](images/using-tdc/shortcuts.webp)

| Keys                           | What it does                                        |
| ------------------------------ | --------------------------------------------------- |
| `Ctrl K`                       | Search messages, channels and people                |
| `?` or `Ctrl /`                | Show the shortcut list                              |
| `Esc`                          | Close what is open, or leave the text box           |
| `[`                            | Show or hide the sidebar                            |
| `Ctrl Shift K`                 | Switch conversation                                 |
| `Alt ↑` / `Alt ↓`              | Previous and next channel                           |
| `Alt Shift ↑` / `Alt Shift ↓`  | Previous and next conversation with unread messages |
| `G` then `H`                   | Go to conversations                                 |
| `G` then `P`                   | Go to projects                                      |
| `G` then `I`                   | Go to my issues                                     |
| `G` then `B`                   | Go to the board of the project you are in           |
| `G` then `D`                   | Go to the directory                                 |
| `G` then `S`                   | Go to settings                                      |
| `C`                            | Write a message                                     |
| `E`, or `↑` in an empty box    | Edit your last message                              |
| `T`                            | Reply in a thread to the last message               |
| `R`                            | React to the last message                           |
| `M`                            | Mark as read and jump to the newest message         |
| `N`                            | File a new issue, in a project                      |
| `Ctrl B`, `Ctrl I`, `Ctrl E`   | Bold, italic, inline code, in the message box       |
| `Ctrl Shift X`, `Ctrl Shift C` | Strikethrough, code block, in the message box       |
| `Shift Enter`                  | New line instead of sending                         |

On a board, every card has a move handle. Focus it and press `Enter` or `Space`
to pick the card up, `←` and `→` to choose a column, and `Enter` to put it down
(`Esc` leaves it where it was). `Shift ←` and `Shift →` on the handle move a card
one column straight away.

On a Mac, `Ctrl` here is the command key, `Alt` is option.

The single-key shortcuts do nothing while you are typing — a shortcut that stole
a letter mid-sentence would cost more trust than every shortcut is worth — and
nothing reaches past an open dialog into the screen behind it. Shortcuts
carrying a modifier work anywhere, because nobody types `Ctrl K` into a
sentence. None of them use a key the desktop app's menu already claims, and
the formatting keys (`Ctrl B` and the rest) work only inside the message box,
where they take over from the browser's own meaning for them.

The list under `?` is generated from what the application actually binds, so it
cannot drift from the real behaviour.

### Getting started

A short guided tour introduces the conversation view. It is skippable, and
resumable if you leave it half-way — somebody interrupted at step four has
already read the first three. A tour you have finished or deliberately skipped
is not offered again, and that is remembered on your account, so the
introduction you saw on the web client is not shown to you again on the desktop
one. One tour exists today.

## On a phone

The web application installs to a home screen and behaves as an app: its own
icon, no browser furniture, notifications with the browser closed.

![The nuxt channel on a phone: the conversation fills the screen, with the sidebar behind the menu button and the message box at the bottom](images/using-tdc/phone.webp)

It tolerates being offline. A message you compose with no connection is queued
and sent when one returns — or reported as failed, rather than silently
vanishing — and the queue survives the tab being closed, because a message
written on a train is still a message somebody wrote.

Be clear about what "offline" covers, though. A conversation already open stays
readable when the connection drops, and attachments you have seen are cached.
Reloading while offline does not bring the history back: messages are fetched
from the server rather than kept in the browser, so a cold start with no
connection gets you the application and no conversations.

Swiping does the obvious things: swipe a message to reply, swipe between
channels, pull to refresh. Every one of those is reachable another way, because
a gesture nobody discovers is not a feature.

Nothing is pointer-only. Every action that appears on hover is reachable by
touch, and the whole application works at phone width without sideways
scrolling.

## On the desktop

The desktop client is optional. On a phone or tablet the installed web app is
the one to use; the desktop client exists for what a browser will not do.

**It is installed once and pointed at a server**, because TDC is self-hosted and
the application cannot know which server is yours. You can add more than one —
your community's and your employer's — and each keeps its own session, so
switching is picking a different one rather than signing in again. Removing a
server takes its session with it.

A server reached over plain HTTP must be on your own machine. Everything you
send travels over that connection, and on a network somebody else can see, plain
HTTP hands all of it to anybody on the same wire. Typing a bare hostname gets
`https://`; an address written explicitly as `http://` is never silently
upgraded, because quietly changing what somebody asked for is how people end up
trusting the wrong thing.

**Everything the web client has is here too**: conversations, projects and
issues, the directory where your server offers one, integrations, the
administration pages for those who may use them, and the same settings —
passkeys, two-step sign-in, privacy, signed-in devices. Creating an account,
setting up a new server and recovering a password all work against whichever
server you chose.

What the desktop client adds:

- **Notifications from the operating system**, which respect system quiet modes
  and open the conversation they are about when you activate them. Where your
  system wants permission first, **Settings → This computer** is where to give
  it: TDC asks only when you press the button there, because some systems ask
  only once.
- **Window and update settings** alongside the rest, under the same heading:
  how see-through the window is, starting at login, and when to look for a new
  version.
- **Your session in the OS keychain**, so you are not asked for a password every
  launch. Where there is no keychain it says so and asks each time. It never
  falls back to storing anything in plain text.
- **A tray icon** carrying the unread count, with the few things you want
  without opening the window: show or hide it, mute for fifteen minutes, an
  hour, or until tomorrow, jump to a recent conversation, add a server or a
  channel, or quit. Whether closing the window quits or hides it is yours to
  set.
- **Launching at login**, off unless you turn it on, and your window's position
  restored — adjusted if the display it was on has gone.
- **Updates that verify what they downloaded** and discard anything that fails
  the check rather than installing it.

### Which Linux download to take

Two Linux builds are published for each architecture, and almost everybody wants
the ordinary one — `tdc-<version>-linux-amd64`, with no suffix.

The one ending `-gtk3` is for older distributions. The ordinary build uses GTK4
and WebKitGTK 6.0, which reached the base repositories in **Ubuntu 24.04** and
**Debian 13**; where those libraries are absent it does not start, because there
is nothing for it to load. It also needs glibc 2.38 or newer, which rules out
Ubuntu 22.04 (2.35) and Debian 12 (2.36) whatever else is installed.

The `-gtk3` build uses GTK3 and WebKitGTK 4.1 and needs only glibc 2.34, so it
runs on **Ubuntu 22.04**, **Debian 12**, and their derivatives.

Debian 12 can install WebKitGTK 6.0 from backports, but that does not make the
ordinary build run there — its glibc is still too old. Take the `-gtk3` one.

If you are unsure, take the ordinary build. A build that cannot run says so
immediately and does no harm — it fails to start rather than misbehaving.

The two are not interchangeable afterwards. Each updates only to its own kind,
so a machine on the `-gtk3` build is never offered an ordinary build it could
not run. Moving between them means downloading the other one yourself, which you
would only need to do after upgrading the operating system.

macOS and Windows have one build each; there is nothing to choose.

## Asking before it is too late

Anything irreversible asks first, in both clients, and the question says what
will be lost rather than only whether to continue. Deleting a message, banning
or blocking somebody, removing a server, signing out, ending a session, quitting
with unsent messages.

The confirming button is never the one focus lands on, so a reflexive press of
the space bar cannot destroy anything. Reversible actions do not interrupt you —
a dialog that appears for everything is a dialog nobody reads.

## What is not here yet

So that nothing above is read as implying more than it says:

- **More of calls.** Calls between a few people work, on servers that switch
  them on (see [Calls](#calls)), with ringing, holding, merging into a
  conference and links. Sharing a screen, recording, calls with people on other
  servers and calls of more than a handful of people are still to come.

Several capabilities are finished on the server and are waiting only for a
control in the clients. They are noted where they belong above, and collected
here so that nobody goes looking: group direct messages, muting one
conversation, pinning conversations to your own list, reviewing or cancelling a
scheduled message, giving a status an expiry, appearing offline while connected,
and turning off your own typing indicator. The translations are in the same
position — written, and not yet read by the interface.
