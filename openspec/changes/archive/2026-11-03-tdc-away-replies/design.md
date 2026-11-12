## Context

Statuses are the operator's (`server_statuses`), held per person in `user_presence` with an optional expiry, and "appear offline" is the status keyed `offline` plus the `invisible` flag. `presence.Own` reads a person's status as they set it; `presence.Of` is what others see and withholds the status of somebody invisible. Messages are stored by `rooms` and followed by `afterMessage` (top level) or `afterReply` (threads), which index, notify and publish in the background. Mentions are found by `notify.MentionsIn`, which strips code and formatting first. A program's message carries `via` in its signed content, set by the server. Blocks and the direct-message setting live in `moderation` (`HasBlocked`, `MayDirectMessage`).

## Decisions

### A rule is a row; activity is computed, never stored

`automatic_replies` holds the text, the mention text, `statuses text[]`, `starts_at`/`ends_at` (timestamptz, either open), the `time_zone` it was chosen in, which kinds of message it answers, and `enabled`. Whether a rule applies is worked out at the moment a message arrives from the person's own status and the clock (`autoreply.Applies`), so there is no sweep to run and no state to drift. The window is instants: the client converts the wall-clock time typed in the chosen zone (`@date-fns/tz`), so a machine on UTC and a person in Dar es Salaam agree. The zone is kept only to show the window back as it was meant.

*Why not one reply per person?* "Depending on status" means different words for Away and for Appear offline; a list costs one more column and the first-made-wins rule.

### Where a reply goes

A direct message is answered in the conversation (in its thread, if it was a thread reply). A mention in a channel is answered **in the thread of the mentioning message**: the sender is told (a reply notification), the channel's timeline is untouched, and everybody who opens the thread sees the context. A direct message to the mentioner would open a conversation the person never started, and a reply in the channel is the noisiest choice.

### The mark

The reply is appended as the person (their account, their address, signed) with `via: {kind: "automatic", id: <user id>, name: <display name>}` in its content — the existing program mark, so every path that already treats `via` as "not a person typing" (clients, exports, bots, and the guard below) does so without change. Clients name it "Automatic reply" in words and do not group it with the person's own messages.

### Anti-noise

- Only a person's message: a sender account is required (webhooks have none) and anything carrying `via` is skipped, which covers bots and other automatic replies and so rules out loops.
- Never the sender themselves; only the people named (a mention) or the other members (a direct conversation).
- Once per sender per conversation per **24 hours**, claimed by an upsert on `automatic_reply_log (user, room, sender)` that only succeeds when the last reply there is older than a day — atomic, so two messages arriving together cannot both be answered, and shared by every node. A day rather than "per active period" because a period has no identity once statuses come and go, and a day is what people expect of an out-of-office.
- At most **20** conversations an hour per person, counted from the same log: a ceiling against a mass mention or a script.
- Not on edits, and there is no `@here`/`@channel` in this server to exclude.
- A claim whose reply could not be posted is released.

### Privacy

- **Contact.** Skipped when `MayDirectMessage(sender, person)` is false (which includes the person having blocked the sender, and their "nobody"/"shared channel" setting) or when the sender has blocked the person. The reply would otherwise be the person's words reaching somebody they do not accept words from. Silence is what anybody gets from somebody with no reply, so it discloses nothing about which rule applied.
- **Appear offline.** While invisible, the person's status is withheld from everybody, so matching treats them as holding `offline`: a reply tied to Busy does not fire and announce it. A reply tied to `offline`, or to no status, still fires — it is the person's own words and says nothing the person did not write. The server never adds the status name or note to a reply, and nothing depends on whether the person is connected, so a reply arriving does not reveal presence.
- **Disclosure settings.** Hiding one's status from member lists does not suppress a reply tied to it: the text is what the person chose to send. Settings says so.

### Federation: local only

Replies are posted only in rooms hosted on this server, by local accounts, to local senders. A replica of another server's room is never posted into, which would fork its history; a remote sender has no account here to claim against. Answering across servers can come later under the federation protocol.

### Surfaces

`GET /api/me/status` gains `automaticReply` (any rule applies now), which the sidebar's foot already fetches and shows as words and a mark. The settings section and the composable load lazily; the payload budget is unchanged (220.2 of 222 KB).

## Risks

- The sidebar's "on" reflects the last status read; a window that starts or ends while the page is open shows after the next status refresh (any status change, settings visit or reload). Accepted rather than polling.
- A reply tied to a status the operator later removes stops applying, silently. Rare, and the settings list shows it as not answering.
