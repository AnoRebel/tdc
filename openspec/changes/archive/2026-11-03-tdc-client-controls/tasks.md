Scope note: controls in the shared client layer for capabilities the server already had, and the small server additions they need. Documentation is written as the work is done.

## 1. Server

- [x] 1.1 `GET /api/me/status` and the response of `PUT /api/me/status` give a person's own status, note, expiry and invisibility; a status that holds notifications keeps holding them while the person appears offline; verify with tests, the suppression one sabotaged
- [x] 1.2 `GET /api/me/preferences/notifications` returns the default and each conversation set apart; verify one person cannot read another's
- [x] 1.3 `sendsTypingIndication` read and written through `/api/me/privacy`, other settings untouched; verify announcements are ignored when off
- [x] 1.4 `PATCH /api/me/scheduled/:id` (body, send time, each optional) and `POST /api/me/scheduled/:id/send`; deleting, changing and sending now refused while a delivery pass holds the message; verify a stranger is refused as not found and a message cannot go twice
- [x] 1.5 Each route in the API catalogue and the OpenAPI bodies regenerated

## 2. Clients

- [x] 2.1 New-message dialog with several people, from the sidebar and the search palette; group conversations drawn with faces and a count
- [x] 2.2 One conversation menu for the sidebar and the header: pin, reorder, notification level, mute; muted conversations dimmed without a badge and skipped by next-unread
- [x] 2.3 Pinned section at the top of the sidebar
- [x] 2.4 Scheduled messages list, overall and per conversation, with reword, move, send now, cancel
- [x] 2.5 Status from the foot of the sidebar: one-press menu, dialog with note and clear-after, time left shown; Appear offline sets invisibility
- [x] 2.6 Typing setting in privacy; composer stops announcing; indicator polled while visible
- [x] 2.7 API tokens section in settings
- [x] 2.8 Optimistic changes with rollback and a spoken notice; menus with accessible names and keyboard access; motion through the shared tokens
- [x] 2.9 Remaining hard-coded English through the translation layer, English and Swahili
- [x] 2.10 First download within the existing budget

## 3. Documentation

- [x] 3.1 The user guide describes each control where it belongs, the closing list of unreachable capabilities is gone, and integrating points to the tokens screen
