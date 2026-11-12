## Why

A thread was a lesser conversation. Its box took one line of text and nothing else: no files, no formatting toolbar or shortcuts, no emoji, no mentions, no sending later. Its replies could not be reacted to, edited, deleted, reported, pinned, forwarded or turned into an issue, showed no files or link previews, and a program's buttons in them did nothing. On the server a reply skipped attachment binding, was never indexed for search, and told only the person who started the thread; its edits landed in the channel's timeline where nothing showed them. People who wanted any of that answered in the channel instead, which is what threads exist to prevent.

## What Changes

- **A reply is a message.** The reply endpoint takes what posting a message takes — attachments, an idempotency token, a program's buttons — through the same code path, so the same permission, ban, restriction, content-filter and attachment checks apply and cannot drift. Replies are indexed for search, carry a content digest like every other event, and edits, deletions and updates to a reply belong to its thread.
- **One composer and one message row.** The channel's message box and its message rendering are extracted into shared components (`MessageComposer`, `TimelineGroup`, `TimelineMessage`) with the folding of edits and deletions in one library function. The thread panel uses the same ones, so everything the channel offers — files picked, pasted or dropped, GIFs and emoji, formatting, mentions, scheduling, the message menu, reactions, link previews, buttons, editing with the up arrow — is offered in a thread.
- **Mentions are suggested.** Typing `@` suggests the conversation's members, in the channel and in a thread alike.
- **Following and read state.** Starting or replying in a thread follows it; anybody may follow or stop following explicitly. A reply notifies followers and the people it mentions, and nobody else. Each person's read position in a thread is kept; the channel shows how many replies are new in a followed thread, and the thread marks where they begin. Replies no longer count toward the channel's unread badge.
- **Typing in a thread** is shown in the thread, not the channel.
- **Opening a reply from elsewhere** — a search result, a pin, a notification — opens its thread at that reply.

## Capabilities

### Modified Capabilities

- `messaging/channels`: the Threads requirement now states that a reply is a message in every respect, and adds following and per-thread read state.

## Impact

- **API:** `POST /api/rooms/:id/threads/:eventId/replies` accepts `attachmentIds` and `actions`; `GET /api/rooms/:id/threads/:eventId` resolves a reply to its thread and returns `readSequence` and `following`; new `POST …/threads/:eventId/read`, `PUT` and `DELETE …/threads/:eventId/follow`; thread summaries gain `unread`; `GET /api/rooms/:id/reactions?thread=`; typing takes and reports `threadRoot`; messages carry `threadRoot`.
- **Database:** one migration adds `thread_reads` and `typing_indicators.thread_root_id`.
- **Behaviour change:** a channel's unread count no longer includes thread replies, and a member set to be notified of everything in a channel is no longer notified of every reply in every thread — only of threads they follow.
