## Context

`users.username` is unique by its folded form, and the address `username@server` is derived from it everywhere it is shown. Several things store a person by address or by name rather than by id:

- **Events** carry `sender_address` inside the signed content (`server/internal/events`). Rewriting it would invalidate the signature and the content-derived id.
- **Federation** keeps remote participants by address (`remote_members.address`) and verifies that an event's sender belongs to the server that signed it. There is no message for "this address is now that one".
- **Snapshots** — reactions, pins, attachments, notifications, scheduled messages — keep an address beside a user id for display; every authorisation decision uses the id (`SenderID`, `user_id`).
- **Lookups by name** — profile links (`/directory/people/:username`), saved views filtering by assignee, project membership by name, enquiries, signing in — go through `GetUserByUsername` or three directory queries.
- **Clients** decide "is this message mine" by comparing the sender address with the current address, and the desktop application remembers the username it last signed in with.

## Decisions

### Old names are kept, held, and resolve

`username_history (user_id, username, username_folded, changed_at)` records every name given up. `user_named(folded)` is the holder, or else the account that most recently gave the name up. `GetUserByUsername` and the directory lookups use it, so every "find a person by name" agrees — a profile link, a saved filter, a project invitation by name, and signing in from a desktop that remembers the old name all keep working.

A name given up is **held** for `accounts.username_hold_days` (90): nobody else may take it by renaming or registering. Registration is guarded in `identity.CreateUser`, the one function every way of making an account goes through, so OAuth sign-up and administrator-made accounts are covered without a check at each caller. After the hold, the name is free; until somebody takes it, it still resolves to its last owner.

*Why hold rather than release at once?* Whoever takes a released name receives the mentions, profile visits and trust meant for its previous owner. A hold makes that impersonation wait out the period in which people still remember the old name.

*Why not hold forever?* Names are a finite namespace in a community; operators can set a long hold if they want one.

### Signed history is not rewritten

Old messages keep the address they were signed under. That is the only honest option: the signature covers it, and the event id is derived from the content. The cost is that old messages show the old name. To keep them the person's own, `GET /api/me` returns `formerAddresses`, and the client's ownership test (`isOwnAddress`) accepts them. Server-side, edit and delete authorise by `sender_id` already, so nothing changes there.

Mentions are text resolved at sending time; old `@oldname` text is unchanged and new mentions use the new name.

### Federated members cannot rename

Peers know a person only by address: their `remote_members` row, and the signed events they hold. A rename would leave the person a member, on the home server, under an address that no longer exists here, and their next message would be refused as coming from a non-member. Carrying a rename would need a protocol message, signed by the old identity's server, that every peer applies to membership — a protocol version change and a migration on every implementation.

So a rename is refused while the person is a member of any room hosted elsewhere or hosted here with peers (`InFederatedRoom`), and the refusal says to leave those rooms first. This is checked again inside the rename's transaction.

### Proof, pacing, and races

- The password confirms a change; an account without one must have a session younger than 15 minutes (having just signed in, including any second factor). The 403 for a wrong password is deliberate: a 401 would read to clients as a lost session.
- API tokens are refused: a program holding a token is not the person.
- `accounts.username_change_days` (30) between changes, so a name cannot be rotated to dodge blocks.
- 5 attempts an hour per account (with a lockout), counted before the password check, so a stolen session cannot guess the password through this route. Availability checks: 60 per 5 minutes.
- The change runs in one transaction under advisory locks — on the account (two changes by one person cannot both pass the wait) and on both names in sorted order (a name cannot be claimed in the instant its owner gives it up, and two people swapping names cannot deadlock). The audit entry is written in the same transaction.

### Display names

Free text, with refusals only for what deceives: control characters, invisible format characters (Cf) other than the emoji joiner after a symbol, and reserved names spelt with punctuation. Applied at registration as well, where nothing was checked before; an OAuth provider's suggested name is used only if it passes.

Others see a new display name on their next fetch of a member list or profile; no realtime frame is sent. Display names are read live from `users` everywhere they appear, so nothing is stale beyond the next fetch.

## Risks

- **Ringtones and other per-client settings keyed by address** keep the old address. Documented; the person's friends re-pick a ringtone.
- **Passkeys** show the old name on the authenticator; they keep working because the WebAuthn handle is random, not the name.
- **A released name** resolves to its new holder: a very old link to the previous owner then reaches somebody else. That is what releasing means, and the hold is what bounds it.
