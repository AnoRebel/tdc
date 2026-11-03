Scope note: these tasks bring threads to parity with the channel they belong to. The requirement is in `messaging/channels` (Threads), edited directly.

## 1. Server

- [x] 1.1 One posting path for messages and replies: attachments checked and bound, buttons checked, content filter and review flag, typing withdrawn; the reply endpoint accepts `attachmentIds` and `actions`
- [x] 1.2 One append path for events, with an optional thread; replies carry a content digest; edits, deletions, updates and presses of a reply carry its thread; reply counts count only messages
- [x] 1.3 A thread read resolves a reply to its root and decorates replies as the timeline does (attachments, lifetimes, views recorded)
- [x] 1.4 `thread_reads`: per-person read position and following, automatic follow on starting or replying unless the person chose; follow, unfollow and mark-read endpoints; unread replies in thread summaries; replies left out of the channel's unread count
- [x] 1.5 A reply indexed for search, and notifying followers and mentioned people only
- [x] 1.6 Reactions for a thread; typing within a thread
- [x] 1.7 API catalogue and OpenAPI bodies; API tests for each of the above, with the attachment, follower and unread guards sabotage-verified

## 2. Clients

- [x] 2.1 Shared folding and grouping of messages, mention detection, and message-box keys, with unit tests
- [x] 2.2 `MessageComposer` extracted from the channel: files picked, pasted and dropped, GIFs and emoji, formatting, scheduling, mention suggestions, the up arrow to edit
- [x] 2.3 `TimelineGroup` and `TimelineMessage` extracted: menu, editing, reactions, files, link previews, buttons, private answers, issue links, lifetimes; the channel's operations provided to both the channel and the thread
- [x] 2.4 The thread panel rebuilt on those: live updates through the conversation's connection, a mark where new replies begin, follow toggle, typing, drop zone, full-screen on a phone
- [x] 2.5 New replies counted beside a followed thread's root; a reply named by search, a pin or a notification opens its thread
- [x] 2.6 Translations in English and Swahili; user guide and integration guide updated
