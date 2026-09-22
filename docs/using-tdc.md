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

Ending a session asks first and says what will happen, because signing a device
out is not something to do by accident.

Changing your password invalidates every other session. That is the behaviour
you want if you are changing it because you think somebody else has it.

## Conversations

### Channels

A channel is a named conversation. It may be **public**, which anybody on the
server can find and join, or **private**, which only its members can see.

A private channel is genuinely invisible rather than merely locked. Somebody who
is not a member cannot tell the difference between a channel they are not
allowed into and one that does not exist, anywhere in the application or the
API. That is deliberate — a refusal that distinguished the two would confirm
that the channel is there.

Channels can be archived, which keeps all the history readable and refuses new
messages, and restored, which resumes posting. Nothing is lost either way.

Creating a channel takes a permission. On most servers ordinary members have it;
on some the operator keeps it.

### Direct messages

A direct conversation is between you and one other person. Start one by finding
them in the search box — `Ctrl K` — and choosing them; there is no separate "new
message" button.

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

### Links

Message text is plain text. Links, addresses and phone numbers in it are
detected and made clickable; there is no Markdown or other formatting in a
message body today. Nothing you can type into a message becomes markup, which is
what makes that safe: the text is escaped in full and links are inserted only
around what the application matched itself.

Markdown is rendered for text files somebody attaches, where a README arrives
looking like a README rather than like its source.

A link posted in a conversation may be shown with a preview of what it points
at. That depends on two things: whether your operator has link previews on at
all, and whether you want them — there is a setting of your own. With either
off, the server makes no outbound request for your links.

## Files

Files are uploaded to the conversation and then sent with a message, which is
why a refused upload costs you nothing but the upload and a message that fails
to send does not lose the file.

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

**A detail you turn off is left out of what the server sends**, rather than sent
and hidden by the application. This is the difference that matters: a detail
that reaches the client has been disclosed whatever the client then does with
it, and anybody who can read a network response can read it.

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

Some things happen without being asked for, because they are what make
notifications bearable:

- Nothing is raised for the conversation you are currently looking at.
- A notification is withdrawn when you read the thing elsewhere.
- A burst of messages arrives as one batched notification rather than one each.
- A status that suppresses notifications suppresses them.

Notifications also reach you with the browser closed, where your operator has
configured that. **You can choose to hide the content** in those, so a push for
a private conversation says that something arrived without saying what. That
switch is in your settings, and it fails closed — a push it cannot be sure about
conceals the sender rather than guessing.

Muting a single conversation is in the server and the API but has no control in
either client yet, and it is a plain on or off rather than a duration. Following
a thread you were not part of is not implemented.

### Search

One box searches messages, channels and people together, rather than making you
choose first and search three times.

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

Remove a message. Remove a channel. Suspend an account, which invalidates its
sessions immediately. Restrict somebody from posting, temporarily or
permanently. Ban somebody from one channel, which is confined to that channel
and leaves their other memberships alone.

Everything on that list is recorded in the audit log, with its reason and its
duration, and everything on it is reversible. Temporary restrictions expire by
themselves rather than needing somebody to remember.

Your operator may also have a word filter configured, which can flag a message
for a human, mask the word, or block the message outright. It is off unless
somebody set it up.

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

## Making it yours

### Appearance

Light or dark, following your system by default. Two colour palettes ship,
offered as **Cool** and **Warm**, and the accent colour is yours to choose
independently of the palette — pick a colour and the application derives a
readable shade for light and for dark rather than accepting or refusing what you
picked. A colour that cannot be made readable is refused.

**Your choice is stored on your account**, so it follows you to another browser
or to the desktop client rather than living in one browser's storage. Your
operator sets the server's default, and clearing your own choice returns you to
it.

### Language

English and Kiswahili both ship, and your operator sets the server's default.
Switching changes what is on screen: every sentence the interface writes goes
through the translation layer, so a Kiswahili speaker reads a Kiswahili
application rather than an English one with a Kiswahili menu.

What is never translated is what people wrote — messages, channel names, status
text, somebody's display name. Those are shown as they were typed, in whatever
language they were typed in.

### The keyboard

| Keys              | What it does                                 |
| ----------------- | -------------------------------------------- |
| `Ctrl K`          | Search messages, channels and people         |
| `?`               | Show the shortcut list                       |
| `Ctrl Shift K`    | Switch channel                               |
| `Alt ↑` / `Alt ↓` | Previous and next channel                    |
| `C`               | Write a message                              |
| `T`               | Open the thread on the last message          |
| `Esc`             | Close what is open, or leave the message box |

On a Mac, `Ctrl` here is the command key.

The unmodified shortcuts do nothing while you are typing — a shortcut that stole
a letter mid-sentence would cost more trust than every shortcut is worth.
Shortcuts carrying a modifier work anywhere, because nobody types `Ctrl K` into
a sentence.

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

What the desktop client adds:

- **Notifications from the operating system**, which respect system quiet modes
  and open the conversation they are about when you activate them.
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

- **Federation.** Servers do not exchange messages. Addresses name a home server
  and every event is signed, so the groundwork exists, but your conversations
  are with people on your own server.
- **Project management and the developer directory.** Both are part of what TDC
  is for and neither is built.
- **Changing your own username or display name.** Neither has a control or an
  endpoint, so both are whatever they were when the account was made. The
  server can already change a display name; nothing asks it to.
- **Choosing who may do what, from the interface.** Roles and permissions are
  managed through the API rather than a screen, so an administrator setting up
  a moderator is doing it with a tool rather than a form.
- **Voice and video calls.** The permission list mentions starting and joining
  one; nothing enforces those and no call can be made. They are names waiting
  for a feature, not a feature waiting to be switched on.
- **Message formatting.** No bold, italic, lists or code blocks in a message.

Several capabilities are finished on the server and are waiting only for a
control in the clients. They are noted where they belong above, and collected
here so that nobody goes looking: group direct messages, muting one
conversation, pinning conversations to your own list, reviewing or cancelling a
scheduled message, giving a status an expiry, appearing offline while connected,
and turning off your own typing indicator. The translations are in the same
position — written, and not yet read by the interface.
