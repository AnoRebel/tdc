## Context

See `proposal.md` for motivation. This document covers the decisions specific to the directory and engagements — it assumes the platform foundation's decisions (Postgres-only, `sqlc`, the event log, the permission model, zero-cost self-hosting) rather than re-litigating them.

Two constraints shape everything below:

1. **Being found is opt-in, and opting in is legible.** The server already has a stated position — `user_privacy` and `SearchPeople`'s own comment — that presence and searchability default toward restraint. A directory profile is a second, separate decision layered on top of an account, never a side effect of using the rest of the platform.
2. **TDC is not a party to the work.** An engagement is a record of what a client and a developer agreed. It is evidence, not a contract the server enforces, and the design must make that impossible to mistake even under pressure — a dispute is exactly when someone will look for the platform to have promised more than it did.

## Goals / Non-Goals

**Goals:**

- A profile that discloses exactly what its owner chose, verified against the same discipline `user_privacy` already established for presence.
- A directory search that returns strictly less than `SearchPeople`'s comment would allow it to return by accident — opted-in profiles only, fields the owner chose to expose only.
- An engagement lifecycle whose every transition is attributable and auditable, without the server ever representing itself as a counterparty.
- A verification model with a stated meaning, a stated author, and a stated way to be wrong.
- Screens usable on a mid-range Android phone on metered data — this is the surface a Tanzanian developer is most likely to use to answer a client's enquiry.

**Non-Goals:**

- Payment processing, escrow, or invoicing. Recorded as future, optional, toggled integration work if TDC ever wants it — not designed against here.
- Dispute resolution or any adjudicative role for TDC or its moderators over an engagement's outcome. Moderators police platform conduct (abuse, harassment, prohibited content) exactly as they do elsewhere; they do not rule on whether delivered work matched a proposal.
- Cross-server directory federation. `directory/profiles` and `directory/engagements` are local-server capabilities in this change; a federated directory is a distinct, later decision the federation protocol would need to make deliberately, not something this change backs into.
- Identity verification beyond what a moderator can reasonably attest to by hand (e.g. matching a claimed GitHub profile, confirming a company registration document a member uploads). No third-party KYC integration.

## Decisions

### Privacy: a second opt-in layer, not a loosening of the first

**Decision.** A directory profile is a new row a user must create deliberately (`directory_profiles`, keyed on `user_id`), sparse the same way `user_privacy` is sparse: no row means no profile, and no profile means invisible to directory browsing and search — not merely empty. Creating a profile does not touch `user_privacy`, `user_presence`, or any existing disclosure setting; it is an independent, additive decision.

Field-level visibility on the profile is **not** a new bespoke enum. It reuses the three-state shape the spec itself names — visible to anyone permitted to browse the directory, visible to server members only, hidden — stored per profile-section (bio/skills/portfolio one visibility, contact details a separate and more restrictive one by default, per the spec's explicit requirement). This mirrors `user_privacy`'s per-setting granularity rather than a single profile-wide switch, for the same reason that table gives presence, status and last-seen three separate flags: "online generally" and "reachable by strangers" are different disclosures and a person may want one without the other.

**Where this interacts with existing settings, stated explicitly rather than left implicit:**

- **`user_privacy.direct_messages`** governs who can open a conversation with this person at all, independent of the directory. An enquiry is not a direct message — it is a distinct, rate-limited object addressed at a profile — so a person who has closed DMs to `nobody` can still be reached by directory enquiry only if their profile independently says they are accepting enquiries. The two settings are never merged: closing DMs does not silently close enquiries, and opening a profile to enquiries does not reopen DMs. A profile carries its own `accepts_enquiries` flag (default: on if the profile is public, since a public profile with enquiries closed is a confusing dead end — but changeable independently).
- **`user_privacy.shows_presence` / `shows_status` / `shows_last_seen`** are untouched by having a directory profile. "Available for work" (the spec's Availability requirement) is a distinct field on the profile — an explicit statement about the kind of engagement someone wants, with its own expiry — not a read of live presence. A developer can be online, presence hidden, and simultaneously and correctly listed as available for work; the two questions are unrelated and the schema keeps them that way.
- **`SearchPeople`** (member-list search) is not changed, extended, or joined against directory data. Directory search is a new query (`SearchDirectoryProfiles` or equivalent) that starts from `directory_profiles WHERE visibility = 'public'` and only ever surfaces fields that profile's own settings expose. It does not fall back to `SearchPeople`'s columns for a profile it cannot fully disclose, and `SearchPeople` gains no new columns or joins from this change. Two search paths, two disclosure contracts, no shared code path that could leak one into the other.

**Enumeration resistance.** The spec requires the directory not become a way to enumerate every account. Two things enforce this beyond ordinary rate limiting: directory search only ever iterates `directory_profiles`, which is strictly smaller than `users` and only as large as the number of people who opted in; and browsing/search responses are paginated with a server-enforced page size ceiling, the same shape as `SearchRooms`' and `SearchPeople`'s existing `@result_limit` pattern, so a client cannot request the whole table in one call.

### Verification: an attested claim, not a status

**Decision.** Verification attaches to a specific claim, not to a whole profile. A profile has zero or more `profile_verifications` rows, each naming what was verified (a skill, an identity claim, a portfolio item, a company association), who verified it (a moderator's `user_id`), when, and an optional note. There is no single "verified profile" badge that could imply more than was actually checked — a profile with one verified skill and nine unverified ones shows exactly that, not a blanket checkmark.

**What a verification is worth**, stated in the UI wherever it's shown, not left to be inferred: a moderator looked at the evidence offered for this specific claim and judged it credible. It is not TDC vouching for the person's character, work quality, or reliability, and the interface must not phrase it as such. This mirrors the spec's own "the platform is not a party" principle from the engagements side — verification is the same kind of honesty applied to identity and skill claims instead of money.

**How it can be wrong or withdrawn:**

- **Editing the verified claim clears the verification** (spec requirement, implemented by making `profile_verifications` reference an immutable snapshot or a content hash of the claim at verification time — if the live claim no longer matches, the verification is stale and treated as absent until re-verified). This is the same "immutable version, re-verify on change" discipline the engagements spec applies to proposals.
- **A moderator can revoke** a verification directly (evidence turned out to be false, credential lapsed), which is an audited action like any other moderator action, recorded with actor and reason per `platform/audit-log`.
- **Who can verify what** is scoped by permission, not by a single "verifier" role: `directory.verify` is a new permission (server-scoped, following `permission.go`'s existing shape) a moderator or operator-designated verifier holds. It is deliberately not automatic and not self-service — a claim a user "verifies" for themselves is simply the self-asserted claim the spec already requires be distinguishable from a verified one.

### Engagements: the server records, it does not enforce

**Decision.** The agreement record is a database row (`engagement_agreements`) capturing the accepted proposal version's content, both parties' user IDs, and both acceptance timestamps — immutable once written, exportable by both parties in a documented format (JSON, matching the export shape used elsewhere on the platform for portability). Nothing about its schema or its API implies a payment state, a completion guarantee, or an arbitration outcome, because none of those exist here.

**The disclosure requirement is a UI and API concern, not only a database one.** Every surface that displays or exports an agreement carries the same fixed statement of what TDC does and does not do — no escrow, no payment processing, no dispute resolution, unless an operator has explicitly enabled a toggled integration that says otherwise. This is rendered once from a shared component/string, not re-authored per screen, so it cannot drift screen to screen.

**Engagement state machine.** Enquiry → discussion → proposal → negotiation (revise/accept/decline, looping as needed) → agreement (both parties accepted the same version) → active → completion-pending (one party marked complete) → completed. Withdrawal is possible at any point before agreement; after agreement, ending the relationship is not modelled as a state the server adjudicates — the parties simply stop, or one marks it complete without the other confirming, which the spec requires stay visible as outstanding rather than silently resolving. The server never infers or asserts that work was satisfactorily delivered; "completed" means both parties said so, not that the server verified anything about the work itself.

**Private discussion reuses the existing messaging substrate**, not a new conversation type. An engagement gets a `room` of kind `direct`-like-but-engagement-scoped (or a dedicated `kind = 'engagement'` value on the existing `rooms` table, whichever the implementation finds cleaner against the current schema), inheriting the same attachment, retention, and non-discoverability rules `messaging/channels` already enforces for direct messages. This is deliberate reuse: the spec's requirement that engagement discussion "is subject to the same messaging, attachment, and retention rules as other conversations" is easiest to satisfy correctly by literally being the same code path, not a parallel one that has to be kept in sync by hand.

**Linking to a project** (spec requirement) exposes work items to project members while keeping commercial terms — proposals, prices, the private discussion — visible only to the engagement's two parties. This is enforced the same way channel-scoped permissions already are: a project member's view of a linked engagement's issues is a join against the project's own membership and permission model, never against the engagement's participant list, so there is no code path that could accidentally surface a price to someone who only has project access.

### Skill vocabulary: controlled, with a review queue

**Decision.** `skills` is an operator-maintained table (name, optional description, a `status` of `active` or `proposed`). Profiles reference skills by ID through a join table, never by free text, so two spellings of the same skill cannot both become searchable. A user proposing a skill not in the vocabulary inserts a `proposed` row visible to them and to moderators but not searchable and not attachable to anyone else's profile until an operator/moderator with `directory.skills.manage` promotes it to `active` or rejects it. This is a small, deliberately boring queue — the same shape as the existing moderation queue, not a new pattern.

### Data model summary

New tables, all additive, none touching `users`, `user_privacy`, or `user_presence`:

- `directory_profiles` (developer profile: bio, availability, availability_expires_at, visibility settings per section, accepts_enquiries)
- `company_profiles` (name, description, location) and `company_members` (company_id, user_id, role, status: pending/accepted — consent-gated per spec)
- `skills` and `profile_skills` (join table)
- `profile_verifications` (claim reference, verifier, verified_at, revoked_at, note, claim snapshot/hash)
- `portfolio_items` (profile_id, title, url, description)
- `enquiries` (sender, recipient profile, message, created_at, rate-limit bookkeeping)
- `engagements` (parties, state, linked_project_id nullable, timestamps per transition)
- `engagement_transitions` (engagement_id, from_state, to_state, actor, occurred_at — the audit trail the spec requires be visible to both parties)
- `proposals` and `proposal_versions` (immutable once sent; a new version is a new row, never an update)
- `proposal_responses` (accept/decline/request-changes, with change comments attached to a part of the proposal)
- `engagement_agreements` (immutable, references the accepted proposal_version, both acceptance timestamps)

### Low-bandwidth, low-end-device requirements

Directory browsing and search responses stay small and paginated (mirrors `SearchPeople`/`SearchRooms`'s `@result_limit` pattern). Portfolio items store links, not uploaded media, for the profile itself — an avatar or single portfolio image goes through the existing `messaging/attachments` pipeline (size limits, content-type detection already built) rather than a new unbounded upload path. Profile and engagement screens are built on the existing shared `packages/ui` components (the vendored form primitives, TanStack Form + Valibot) so they inherit the responsive, accessible, low-payload behaviour already proven in chat rather than re-deriving it.

## Risks / Trade-offs

- **A second visibility model next to `user_privacy` is more surface to keep straight, not less.** → Mitigated by keeping them structurally separate (different tables, no shared query path) rather than trying to unify them into one settings object that would need to explain to every reader why "profile visibility" and "presence visibility" are different axes.
- **Verification can be mistaken for endorsement despite the disclosed wording.** → The UI states, at the point of display, what was checked and by implication what was not; this is a documentation and copy discipline that must be maintained as new claim types are added, not a one-time fix.
- **An agreement record with no enforcement mechanism may feel like a missing feature to users used to platforms that hold funds.** → Accepted deliberately, consistent with zero-cost self-hosting and TDC not wanting to become a money-handling, regulated party. The spec's own "platform is not a party" requirement exists because this was foreseen, not discovered.
- **The skill-vocabulary review queue is a moderator workload that grows with adoption.** → Kept intentionally simple (approve/reject, no workflow) so it does not become a second product; if it becomes a bottleneck that is a staffing question for TDC, not a design flaw to solve here.
- **Reusing the messaging substrate for engagement discussion means engagement rooms inherit any future change to `messaging/channels`, for better and worse.** → Accepted: the alternative (a parallel private-conversation implementation) is the more likely source of an eventual privacy bug, since it would have to independently re-implement retention and non-disclosure rather than inherit them.

## Open Questions

- Whether `directory.skills.manage` is its own permission or folded into the existing `PermModerationReview` — settle during implementation by checking whether skill review is ever delegated to someone without general moderation power in practice.
- Exact shape of the `engagement`-kind room (new `rooms.kind` value vs. a flag alongside `direct`) — an implementation detail against the current schema, not a spec-level decision.
- Whether availability expiry sends a reminder notification before lapsing, or lapses silently and the person notices next time they check their profile. The spec only requires that it lapses; the reminder is a UX nicety to decide against real usage, not before it.
