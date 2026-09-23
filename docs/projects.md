# Projects and issues

A project is where work agreed in a conversation is tracked: what was decided,
who is doing it, and whether it is done. It lives on the same server as the
conversations that produce the work, so a message can become an issue without
anybody retyping it, and an issue can be discussed without leaving it.

This guide is for the people using projects and for anybody integrating with
them. [Hosting](hosting.md#projects) has the few things an operator needs to
know.

## Projects

A project has a **name**, which can change, and a **key**, which cannot. The key
is two to ten letters or digits starting with a letter — `WEB`, `API`, `MOBILE2`
— and every issue in the project is named by it and a number: `WEB-12`. That
identifier is what people write in commit messages and say in meetings, so it
never changes: not when the issue is renamed, not when it moves between states,
not when the project itself is renamed. A key is typed in any case and stored
upper-cased, and no two projects can have keys that differ only in case.

A project is **public** or **private**. A public project and its issues are
visible to everybody on the server. A private one exists only for its members:
to anybody else it is not refused but absent — it does not appear in any list or
search, and asking for it by key answers exactly as asking for a project that
was never made. Because keys are unique across the server, trying to start a
project with a key a private project already uses is refused as taken; that is
the one thing a key reveals.

Whoever starts a project is its first **administrator**. Administrators decide
who is in it, rename it, change who can see it, and configure its states.
**Members** work in it: they file, move, assign, label and relate issues.

### Starting one

**Projects** in the sidebar lists every project you can see. **New project**
asks for a name and suggests a key from it — edit the key to choose your own —
and whether it is public or private. Starting a project needs the
`project.create` permission, which the role every account holds grants by
default.

## States

An issue is always in exactly one **state**. A project's states are its
workflow, and they are the columns of its board, left to right. A new project
starts with three — _To do_, _In progress_, _Done_ — and its administrators
change them under **Settings**: rename one, add one, move one earlier or later,
and mark which ones **mean done**. Progress counts an issue as finished when it
is in a state marked done, whatever that state is called.

Removing a state that issues are still in asks where they should go, and
refuses to guess. Each issue is then moved there as an ordinary move, recorded
in its own history, before the state is removed — so no issue is ever left in a
state that no longer exists. A project always keeps at least one state.

## Issues

**New issue** on a project's issue list asks for a title and, optionally, a
description. The issue starts in the project's first state and takes the next
number: numbers are never reused, so `WEB-12` can only ever mean one issue.

On the issue's own page, `/issues/WEB-12`:

- **State** moves it. If somebody else moved it between your loading the page
  and choosing, your move is refused rather than silently overwriting theirs,
  and the page shows where it actually is.
- The pencil beside the title edits the title and description. Whoever filed an
  issue may always edit it, as may anybody who may file issues in the project.
- **Delete** hides it from lists and boards. Its history is kept and its number
  is not given to another issue.

### History

Every change to an issue — filed, moved, renamed, described, deleted, and in
later sections assigned, labelled, related and scheduled — is recorded with who
made it, when, and what it was before. The record is the server's signed event
log: the same append-only, tamper-evident log that holds every message. It is
not a separate audit table that could disagree with it, and nothing can edit or
remove an entry — there is no endpoint that writes to a history, and the log
behind it cannot be reached through the room endpoints either.

The history shows the names things had at the time. An issue moved into _Doing_
that was later renamed _In progress_ still reads "moved to Doing", because that
is what happened.

## Who may do what

| Permission                 | What it allows                                   | Held by default by                 |
| -------------------------- | ------------------------------------------------ | ---------------------------------- |
| `project.create`           | Starting a project                               | every account (the _Member_ role)  |
| `project.administer`       | Renaming, changing visibility, managing members  | the project's administrators       |
| `project.configure_states` | Adding, renaming, reordering and removing states | the project's administrators       |
| `issue.create`             | Filing issues, and editing any issue's text      | the project's members              |
| `issue.transition`         | Moving issues between states                     | members, and an issue's own author |
| `issue.assign`             | Assigning people                                 | members                            |
| `issue.label`              | Labels and priority                              | members, and an issue's own author |
| `issue.relate`             | Parent, child and blocking relationships         | members                            |
| `issue.delete`             | Deleting issues                                  | the project's administrators       |

Each of these except `project.create` can also be granted to a role, either
server-wide or for one project only (`PUT /api/admin/roles/:id/project-permissions`).
An explicit denial wins over everything: a role denied `issue.delete` in one
project cannot delete there even as that project's administrator, the same way
a denial beats server administration. None of the project permissions can be
granted on a channel, and none of the channel permissions on a project.

A refusal says which permission would have allowed the action.

Commenting on an issue has no permission of its own: it is posting in the
issue's discussion, which is governed like posting in any conversation.

## Discussing an issue

Every issue has a discussion beside it, and it is a conversation like any
channel: formatting, files, reactions, threads, editing and deleting your own
messages, and mentions all work as they do everywhere else, because it is the
same conversation underneath. Anybody who can see the issue can read the
discussion; **Join** it to take part, the way you would join a public channel.
In a private project only its members can see either.

Taking part is following:

- Whoever filed an issue, and anybody who comments on it, hears about every
  later comment — as if they had set that discussion to **All** themselves. If
  you would rather not, change the setting for that discussion like any other;
  and if you have asked to hear nothing anywhere, commenting does not change
  that.
- Naming somebody — `@amina` — reaches them if they may see the issue,
  according to their own notification settings, exactly as a mention in a
  channel does. They are brought into the discussion so it can. Naming somebody
  outside a private project does nothing at all: they are not told, and not
  told they were named.

Somebody looking at the discussion when a comment arrives is not interrupted
about it, as in a channel.

## Turning a message into an issue

When a conversation agrees that something needs doing, open the message's menu
— right-click, or press and hold on a phone — and choose **Create an issue from
this**. Pick the project; the title and description start as the message's
first line and the rest of it, and are yours to change before filing.

The issue keeps a **link** to the message, not a copy of it. On the issue's
page, **Where this came from** shows the message as it reads in its
conversation now — edited, or removed if it has since been deleted — with a way
back to it, to anybody who can read that conversation. Anybody who cannot sees
only that the issue was made from a message in a conversation they cannot see:
not which conversation, not who wrote it, not what it said, and not whether it
is still there. A private conversation and a message deleted from one look
exactly the same from outside, because telling them apart would itself say
something.

The message, in turn, shows the issue it became — to people who can see that
issue. A message that became an issue in a private project shows nothing to the
conversation's members outside that project, so the conversation does not tell
them the project exists.

You can only link an issue to a message you can read yourself.
