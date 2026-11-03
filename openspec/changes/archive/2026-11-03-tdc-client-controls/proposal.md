## Why

The user guide ended with a list of things finished on the server and unreachable from either client: group direct messages, muting one conversation, pinning conversations to your own list, reviewing or cancelling a scheduled message, giving a status an expiry, appearing offline while connected, and turning off your own typing indicator. Personal API tokens were in the same position — documented for programs, with no screen for people. A capability nobody can reach is, for the people using TDC, a capability that does not exist.

## What Changes

- **Group direct messages.** A new-message dialog, opened from a + beside the direct messages in the sidebar and from the search palette, collects one person or several and opens the conversation between exactly them. The sidebar draws a conversation with several people as overlapping faces with a count. Adding people starts a new conversation, because a direct conversation is the set of people in it.
- **One menu per conversation**, from its row in the sidebar (button, right-click, long press, menu key) and its header: pin and unpin, move up and down, how much to hear about it (every message, mentions and replies, nothing — the server's levels), and mute. Muted conversations are dimmed with no unread badge, and skipped by the next-unread key.
- **Pinned conversations** sit under their own heading at the top of the sidebar, in the order chosen.
- **Scheduled messages** are listed overall and per conversation; each can be reworded, moved, sent now, or cancelled. The server gains rewording, moving and sending now, and refuses to change or cancel a message a delivery pass is already sending.
- **Status** is changed from the person at the foot of the sidebar: a status in one press, or a dialog for a note and when it clears (30 minutes, an hour, four hours, today, this week, a chosen time, never), then shown with how long it has left. Choosing Appear offline also makes the person invisible while connected. The server gains a read of one's own status; appearing offline no longer cancels a status that holds notifications.
- **Typing indicator** can be turned off in privacy settings; the composer stops announcing, and the server already ignored announcements. The indicator is now asked for every few seconds, having had no live frame.
- **API tokens** have a settings section: list, create with a name, lifetime and what the token is left (presented as the denial model the server applies), the secret shown once with copy, revoke with confirmation.
- **Translations.** Remaining English written into components goes through the translation layer.
- **The first download** stays within budget by loading the menus, dialogs, search palette and scheduled-message changes on first use.

## Capabilities

### Modified Capabilities

- `clients/web`: personal controls for every capability above.
- `messaging/channels`: scheduled messages changed, sent now and protected while sending; personal pinned conversations.
- `messaging/realtime`: appearing offline keeps a suppressing status; status expiry and the typing setting offered by the clients.

## Impact

- **API:** `GET /api/me/status`, `GET /api/me/preferences/notifications`, `PATCH /api/me/scheduled/:id`, `POST /api/me/scheduled/:id/send`; `sendsTypingIndication` in `/api/me/privacy`; `PUT /api/me/status` now answers with the person's own status. All additive.
- **Clients:** the shared layer only, so web and desktop change together.
