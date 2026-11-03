## Why

Nobody can change what they are called. The user guide lists it under "What is not here yet": a display name and a username are whatever they were when the account was made. A display name chosen in a hurry, a username chosen at sixteen, a name somebody no longer uses — each is a support request to an operator who has no tool to answer it either.

The display name is easy: nothing is built on it. The username is not. It is the first half of the address `user@server`, and the address is what profile links, saved filters, mentions, API clients, audit entries, signed events and other servers name a person by. Changing it carelessly leaves all of those pointing at nobody — or, worse, at whoever registers the name next.

## What Changes

- **Display name, freely.** `PUT /api/me/display-name`. Registration's rules, now actually stated and enforced at registration too: at most 64 characters, no control or invisible formatting characters (right-to-left overrides, zero-width spaces), the emoji joiner allowed after a symbol, and not one of the reserved usernames however punctuated ("A.d.m.i.n"). Empty shows the username.
- **Username, safely.** `GET /api/me/username` (may I, and what will it ask), `GET /api/me/username/availability` (debounced as somebody types), `PUT /api/me/username`:
  - the operator decides whether changes are allowed and how often (`accounts.username_changes`, on; `accounts.username_change_days`, 30);
  - the password confirms it, or for an account with none a sign-in in the last 15 minutes; API tokens are refused; attempts are rate limited;
  - the old name is held for its owner (`accounts.username_hold_days`, 90) — nobody may register or take it — and keeps resolving to them wherever a name is looked up until somebody else takes it;
  - refused while the person is in a room another server takes part in, with the reason;
  - recorded in the audit log with both names; administrators see former names in the list of people.
- **Settings.** A "Your name" section: display name, username with live availability, and a confirmation step that says what changes and what does not before asking for the password. Shared by web and desktop through the layer.
- **Own messages stay own.** `GET /api/me` carries `formerAddresses`; the chat treats messages signed under them as the person's (edit, delete, grouping).

## Capabilities

### Modified Capabilities

- `identity/authentication`: a requirement for account names — display name rules, and the conditions on a username change.

## Impact

- **Schema:** `username_history`, and `user_named(text)`, the one definition of "whose name is this" used by every username lookup.
- **Federation:** no protocol change. Renames are refused rather than carried; see design.md.
- **Operators:** three settings under `accounts.`, documented in the hosting guide.
