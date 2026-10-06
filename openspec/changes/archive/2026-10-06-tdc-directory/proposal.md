## Why

TDC's own earlier attempt at this — [`dev-space-tz`](https://github.com/Tanzania-Developers-Community/dev-space-tz), 14 commits, no code — was a developer/company directory, and it stalled before it did anything. The platform foundation proposal named the gap it was trying to close: "there is no way for a Tanzanian developer to be discovered by a client, negotiate work, and track that work in one place." Chat and project management now exist on this server. This change is the directory and the engagement workflow — the piece that makes the platform a place where paid work actually starts, not only a place where a community talks.

The two specs this change implements (`directory/profiles`, `directory/engagements`) were written during the foundation change precisely so the identity scheme and event log did not have to be reshaped for them later. They have not changed; this proposal is the implementation.

The hard part is not the data model. It is that this server already has a considered position on being found: `SearchPeople` returns only a username and a display name because "search must not become the way around a privacy setting", and `user_privacy` lets somebody withhold their presence, status and last-seen from people they already share a channel with. A directory's entire premise is the opposite instinct — a developer wants to be found by a client who is, by definition, a stranger. Building this without reconciling the two makes the platform worse at the thing it already does well. The design section below is where that reconciliation happens; nothing here overrides `user_privacy` or loosens what `SearchPeople` returns to a searcher who has not opted in.

## What Changes

Adds the developer/client directory and the engagement lifecycle as a new, separate opt-in layer on top of identity:

- **Developer and company profiles** — skills, portfolio links, a biography, per-field visibility, availability with expiry. Nothing here is required to use the rest of the platform: an account with no profile is a complete account.
- **A controlled skill vocabulary** — operator-maintained, with a proposal path for a missing skill, so search stays reliable rather than fragmenting into a hundred spellings of the same thing.
- **Verification** — a moderator's attestation that a claim (an identity, a skill, a portfolio item) checks out, distinguishable from the self-asserted claim underneath it, revocable, and cleared automatically when the underlying claim is edited.
- **Directory browsing and search** — scoped to profiles that opted in, rate-limited, and explicitly not a general-purpose account enumerator.
- **Enquiries** — the first contact a client makes to a developer's profile, gated by the recipient's own contact preferences and rate-limited against bulk approaches.
- **The engagement lifecycle** — enquiry, private discussion, proposal (versioned, immutable once sent), negotiation, mutual-acceptance agreement record, optional linking to a project, and a completion step requiring both parties' confirmation.
- **The agreement record** — evidence of what both parties accepted, exportable by both, explicitly and visibly *not* a contract TDC enforces, arbitrates, or guarantees.

**Explicitly not in this change:** payment processing, escrow, dispute arbitration, invoicing, or anything that would make TDC a party to the money changing hands. The platform records what was agreed; it does not move funds or adjudicate disagreements. An operator who wants a payment feature integrates one behind a toggle, same as any other optional integration — not built here.

## Capabilities

### Modified Capabilities

None. `directory/profiles` and `directory/engagements` are specified but unimplemented; this change implements them as written. If implementation surfaces a genuine gap in either spec, it is corrected here with the reasoning recorded, not silently reinterpreted.

### New Capabilities

None beyond the two already specified — this change delivers `directory/profiles` and `directory/engagements` from `openspec/specs/directory/`.

## Impact

- **Database**: new tables for profiles, company profiles and membership, the skill vocabulary, verification records, availability, enquiries, engagements and their state transitions, proposal versions, and agreement records. None of this touches `users`, `user_privacy`, `user_presence`, or `search.sql` — a profile is an additional, optional layer a person opts into, addressed by their existing `user_id`.
- **Search**: `SearchPeople` is untouched. Directory search is a new, separate query path over `directory_profiles`, visible only to profiles that are `public`, and is not merged into or substituted for the existing member-list search.
- **Permissions**: new entries in the `authz` catalogue for profile verification, skill vocabulary moderation, and directory moderation, following the existing `Definition` shape (scope, administrative flag) in `server/internal/authz/permission.go`.
- **Clients**: new web screens (profile edit, directory browse/search, enquiry, engagement thread, proposal view, agreement view) built on the existing shared `packages/ui` layer and `ApiClient` adapter, so desktop gets them by the same mechanism chat did.
- **Operators**: a server can run with the directory capability disabled entirely (feature toggle, per `platform/server-config`), so a chapter that only wants chat is unaffected.
- **No new runtime dependency.** Everything is Postgres tables and existing server infrastructure; no payment, escrow, or third-party verification service is introduced.
