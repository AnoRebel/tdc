Scope note: this implements `directory/profiles` and `directory/engagements`, both already specified in `openspec/specs/directory/`. Tasks reference specs for what to build and `design.md` for how. Nothing here touches `users`, `user_privacy`, `user_presence`, or `SearchPeople` — see design.md for why the directory is a separate, additive layer.

**Version rule: this change's milestone tags are reserved in the range `v1.22.0-dev` to `v1.28.0-dev`.** The four changes in flight hold separate ranges so that any of them can be tagged without waiting on the others; `v1.0.0-dev` stays unused, held for the foundation's undemonstrated deployment milestone.

**Documentation rule: documentation is written as the work is done, not after it.** A phase that adds a setting, an endpoint, or a constraint leaves the relevant document current before its milestone is claimed.

**Phasing rule: every phase ends in a running, demonstrable system.** A phase's milestone is the gate: if it cannot be demonstrated by using the software, the phase is not done, regardless of how many tasks are ticked.

## 1. Phase 1 — A profile exists and only its owner controls it

Goal: a person can create a developer profile, set its visibility, and see that an empty or absent profile changes nothing else about their account.

- [x] 1.1 Add the `directory_profiles`, `portfolio_items`, and per-section visibility migration, sparse per design (no row means no profile); verify a fresh account has no profile row and every existing account feature works unchanged
- [x] 1.2 Implement profile create/edit — bio, portfolio links, per-section visibility (public/members/hidden), contact-details visibility defaulting to the most restrictive setting; verify a profile created without adjusting visibility exposes no contact details publicly
- [x] 1.3 Implement the `directory.profile.manage` permission scope in `authz/permission.go` for a moderator editing someone else's profile (e.g. clearing abusive content); verify an ordinary user cannot edit another user's profile and a moderator's edit is audited
- [x] 1.4 Implement profile deletion independent of account deletion, and profile export in a documented machine-readable format; verify deleting a profile removes it from the directory while the account, messages, and sessions remain untouched, and an exported profile round-trips
- [x] 1.5 Implement company profiles and `company_members` with consent-gated association (pending until the user accepts); verify a company cannot list a user who has not accepted, and a user who leaves is removed from both sides immediately
- [x] 1.6 Build the web profile-edit and company-profile screens on the shared form primitives (TanStack Form + Valibot); verify a profile can be created, edited, and made visibility-restricted end to end on a phone-width viewport
- [x] **MILESTONE 1** — Demonstrate: a user with no profile uses chat normally; a second user creates a profile, sets contact details hidden, and confirms nothing changed about their `user_privacy` settings or presence. A moderator edits a profile and the action appears in the audit log. Tag `v1.22.0-dev`.

## 2. Phase 2 — Skills, availability, and being found

Goal: the controlled skill vocabulary, availability, and directory search — the moment a profile becomes discoverable rather than merely stored.

- [x] 2.1 Add the `skills` and `profile_skills` migration with an operator-seeded starter vocabulary and a `proposed` status for new entries; verify an unrecognised skill enters review rather than becoming searchable immediately
- [x] 2.2 Implement the skill review queue and `directory.skills.manage` permission for promoting or rejecting a proposed skill; verify a promoted skill becomes attachable and searchable, and a rejected one is not
- [x] 2.3 Implement availability (open to work, kind of engagement sought, expiry) as a field independent of `user_presence`; verify a developer can be marked available while presence is hidden, and that availability past its expiry is excluded from filtered results without the profile itself changing
- [x] 2.4 Implement `SearchDirectoryProfiles` as a new query, structurally separate from `SearchPeople`, starting from `directory_profiles WHERE visibility = 'public'` and returning only fields each profile's own settings expose; verify a hidden profile and a hidden field never appear in results, and `SearchPeople`'s existing behaviour and output are unchanged
- [x] 2.5 Implement directory browsing and search by skill, availability, location, and company, paginated with a server-enforced page-size ceiling matching the existing `@result_limit` pattern; verify a client cannot retrieve more than one page per request
- [x] 2.6 Implement directory-specific rate limiting on search and browse; verify requests at an abusive rate or pattern are refused and cannot be used to enumerate the full profile set faster than the limit allows
- [x] 2.7 Build the web directory browse and search interface with skill/availability/location/company filters; verify searching and filtering work end to end and respect visibility on a phone-width viewport
- [x] **MILESTONE 2** — Demonstrate: a developer sets three skills (one proposed, entering review), marks themselves available, and appears in a client's filtered directory search; hiding the profile removes it from that same search immediately, and searching for a member-list-only (non-directory) user via `SearchPeople` still behaves exactly as before this change. Tag `v1.23.0-dev`.

## 3. Phase 3 — Verification

Goal: a moderator can attest to a specific claim on a profile, distinguishably from the self-asserted claim, and that attestation can be wrong or withdrawn.

- [x] 3.1 Add the `profile_verifications` migration referencing a specific claim (skill, identity, portfolio item) with an immutable snapshot of the claim's content at verification time; verify a verification record cannot be attached without naming exactly which claim it covers
- [x] 3.2 Implement the `directory.verify` permission and the moderator action of granting a verification with a required note on what was checked; verify granting is refused without a permission check and is recorded in the audit log with actor and reason
- [x] 3.3 Implement verification revocation, also audited; verify a revoked verification is immediately no longer shown as verified and the revocation reason is in the audit log
- [x] 3.4 Implement automatic clearing of a verification when the underlying claim is edited; verify editing a verified skill or portfolio item reverts its display to self-asserted until re-verified
- [x] 3.5 Build the web verification display — visually distinct verified vs. self-asserted state, with the fixed disclosure text on what a verification means (a specific claim checked by a moderator, not an endorsement of the person) shown wherever a verified badge appears; verify the same disclosure text renders identically on every screen that shows a verified claim
- [x] 3.6 Build the moderator verification-review interface (grant, note, revoke) reusing the existing moderation-queue pattern; verify a moderator can perform every verification action above without using the API directly
- [x] **MILESTONE 3** — Demonstrate: a moderator verifies one skill on a profile that has three; the profile shows one verified and two self-asserted claims, distinguishably. The user edits the verified skill's description and it reverts to unverified. A moderator revokes a separate verification and the audit log shows who, when, and why for every action in this sequence. Tag `v1.24.0-dev`.

## 4. Phase 4 — First contact: enquiries

Goal: a client can reach a developer through their profile without it becoming a way around `user_privacy`'s direct-message settings or a bulk-messaging vector.

- [x] 4.1 Add the `enquiries` migration with sender, recipient profile, message body, and rate-limit bookkeeping distinct from `user_privacy.direct_messages`; verify an enquiry and a direct message are governed by independently checkable settings
- [x] 4.2 Implement the `accepts_enquiries` profile flag, defaulting on only for public profiles, changeable independently of direct-message settings; verify closing enquiries refuses new ones with a clear response while existing conversations (if any) are unaffected
- [x] 4.3 Implement enquiry delivery and notification to the recipient, reusing the existing notification pipeline; verify a sent enquiry notifies the recipient exactly as a mention would, with no self-notification path
- [x] 4.4 Implement per-sender rate limiting on enquiries, verified against the abusive-rate requirement in the spec; verify a client sending enquiries at an abusive rate is refused further sends while normal use is unaffected
- [x] 4.5 Build the web enquiry interface — send from a profile, view received enquiries; verify sending, receiving, and a closed-enquiries refusal all work end to end
- [x] **MILESTONE 4** — Demonstrate: a client sends an enquiry to an available developer's profile and the developer is notified and can read it; a second developer with enquiries closed refuses the same attempt with a clear message; a client attempting many enquiries in quick succession is rate-limited. Tag `v1.25.0-dev`.

## 5. Phase 5 — The engagement lifecycle and private discussion

Goal: enquiry becomes a tracked engagement with documented states, both parties always seeing the same state, and a private conversation reusing the existing messaging substrate rather than a parallel implementation.

- [x] 5.1 Add the `engagements` and `engagement_transitions` migrations with the state machine from design.md (enquiry → discussion → proposal → negotiation → agreement → active → completion-pending → completed, withdrawal before agreement); verify every transition writes a row with actor and time and no transition can be skipped
- [x] 5.2 Implement engagement creation from an accepted enquiry, and withdrawal by either party before agreement closing the engagement cleanly; verify both parties see identical current state at every point and a withdrawn engagement carries no agreement
- [x] 5.3 Reuse the existing room/messaging infrastructure for engagement discussion (new `rooms.kind` value or equivalent, per design.md) rather than a parallel conversation type; verify the engagement conversation is subject to the same attachment and retention rules as any other conversation, and is invisible to search and browsing for non-participants
- [x] 5.4 Enforce that engagement conversations never appear in directory or channel search for anyone outside the engagement; verify a search by either party's contacts, or a moderator's ordinary search, does not surface the conversation
- [x] 5.5 Build the web engagement thread interface showing current state and the conversation together; verify a state change is visible to both parties without a refresh, consistent with the live-delivery behaviour chat already has
- [x] **MILESTONE 5** — Demonstrate: an accepted enquiry becomes an engagement with a private conversation; both parties watch its state change together in real time; one party withdraws before any proposal and the engagement closes as withdrawn with no agreement, visible to both. Tag `v1.26.0-dev`.

## 6. Phase 6 — Proposals, negotiation, and the agreement record

Goal: scope, price, and timeline get recorded, revised with full version history, negotiated, and — on mutual acceptance — captured as an immutable agreement that is evidence, not a contract the server enforces.

- [x] 6.1 Add the `proposals`, `proposal_versions`, `proposal_responses`, and `engagement_agreements` migrations, versions and agreements both immutable by design (no update path exists); verify an attempt to alter a sent proposal version or a recorded agreement is refused at the database and API layers, not only the UI
- [x] 6.2 Implement proposal creation and revision as new versions with full history visible to both parties; verify revising a sent proposal creates a new version while every prior version remains visible to both parties
- [x] 6.3 Implement negotiation responses — accept, decline, request changes attached to a specific part of the proposal; verify a change request is recorded against the part it targets and the developer is notified, and a decline lets either party propose again or withdraw
- [x] 6.4 Implement agreement recording on mutual acceptance of the same proposal version, capturing both identities and both acceptance times, and refuse advancing to agreement without both; verify one-sided acceptance does not advance the engagement and mutual acceptance of the same version does
- [x] 6.5 Implement agreement export in a documented machine-readable format for both parties, and the fixed, shared disclosure text (no escrow, no payment processing, no dispute resolution, unless an operator-enabled integration says otherwise) rendered identically everywhere an agreement is shown or exported; verify the same disclosure text appears on the in-app view and the exported document, and that neither implies TDC is a party to or guarantor of the agreement
- [ ] 6.6 Implement linking an engagement to a project, exposing work items to project members while keeping proposals, prices, and the private discussion visible only to the engagement's two parties; verify a project member who is not a party sees the linked issues but not the commercial terms or conversation
- [x] 6.7 Implement completion — either party marks complete, the other must confirm, and an unconfirmed completion stays visibly outstanding to both rather than expiring silently; verify the record persists with full agreement and history after completion, and that an unconfirmed completion request remains visible as outstanding indefinitely
- [x] 6.8 Build the web proposal, negotiation, and agreement interface — compose, revise, respond, accept, view version history, export; verify the full cycle from first proposal to recorded, exported agreement works end to end on a phone-width viewport
- [x] **MILESTONE 6** — Demonstrate: a developer sends a proposal, the client requests a change to one line item, the developer sends a revised version, both accept the revised version, an agreement is recorded and both parties export it showing identical content and the same non-party disclosure. One party marks the work complete; the engagement shows completion outstanding until the other confirms, then shows completed with full history intact. Tag `v1.27.0-dev`.

## 7. Phase 7 — Ready to run alongside the rest of the platform

Goal: the directory is a capability an operator can enable or disable, documented, and verified not to regress anything the earlier phases built.

- [ ] 7.1 Implement the `directory` feature toggle per `platform/server-config`'s existing framework, disabled meaning no directory routes, no enquiry delivery, and no directory data exposed anywhere including search; verify a server with the toggle off behaves exactly as it did before this change, and turning it on requires no data migration for existing accounts
- [ ] 7.2 Extend the end-to-end test suite to cover the full directory-to-engagement-to-agreement cycle across two concurrent clients, and extend the permission-enforcement suite to prove `user_privacy` and directory visibility settings compose correctly (each independently checkable, neither overriding the other); verify these pass on the existing cross-browser CI backbone
- [ ] 7.3 Write the directory and engagements section of the user guide — creating a profile, controlling visibility, sending and receiving enquiries, negotiating and recording an agreement, and the explicit statement of what an agreement record is and is not; verify a person who has never seen the repository can follow it to complete a full enquiry-to-agreement cycle without reading source
- [ ] 7.4 Write the moderator guide addition for verification and skill-vocabulary review; verify it covers granting, revoking, and the audit trail each produces
- [ ] 7.5 Update the operator hosting guide with the new feature toggle and any new configuration (rate-limit thresholds, skill-vocabulary seeding); verify an operator following it can enable the directory on a running server without downtime
- [ ] **MILESTONE 7** — Demonstrate: on a running server, an operator enables the directory toggle, two new people — a developer and a client — complete profile creation, discovery, enquiry, negotiation, and a recorded, exported agreement entirely through the documented guide, and disabling the toggle again removes every directory surface while chat and existing profiles' underlying data remain intact. Tag `v1.28.0-dev`.
