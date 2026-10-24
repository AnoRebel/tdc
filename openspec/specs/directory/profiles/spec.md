## Purpose

Defines the developer and company profiles that make TDC members discoverable — skills, experience, portfolio, and availability — so that a Tanzanian developer can be found by a client, which is the need the community's earlier directory attempt set out to meet.

## Requirements

### Requirement: Developer profiles

A user SHALL be able to maintain a profile describing their skills, experience, portfolio links, and a short biography.

A profile SHALL show the user's account location (see `messaging/realtime`, Location) rather than hold a location of its own, and MUST NOT show it more widely than the profile's own details.

Profile completion MUST be voluntary; no user may be prevented from using messaging because their profile is incomplete.

#### Scenario: Profile is created and viewable

- **WHEN** a user completes their profile
- **THEN** it is viewable by others according to its visibility setting

#### Scenario: Empty profile does not block use

- **WHEN** a user has not completed a profile
- **THEN** they can still use messaging and every other capability

### Requirement: Company profiles

An organisation SHALL be able to maintain a profile with a name, description, and location, and to associate users with it in stated roles.

Association MUST require the user's consent, so no profile can claim a person without their agreement.

#### Scenario: Company lists its members

- **WHEN** a user accepts an association with a company
- **THEN** the company profile lists them and their profile references the company

#### Scenario: Association requires consent

- **WHEN** a company adds a user without their acceptance
- **THEN** the association is pending and not displayed until the user accepts

#### Scenario: User can leave

- **WHEN** a user removes their association with a company
- **THEN** the association ends and neither profile displays it

### Requirement: Skills

Profiles SHALL carry skills drawn from a controlled vocabulary maintained by the operator, so that searching by skill is reliable.

Users MUST be able to propose a skill not yet in the vocabulary, subject to review.

#### Scenario: Skills are searchable

- **WHEN** a client searches for a skill
- **THEN** profiles listing that skill are returned

#### Scenario: New skill is proposed, not silently created

- **WHEN** a user proposes a skill outside the vocabulary
- **THEN** it enters review rather than immediately becoming searchable

#### Scenario: Vocabulary prevents fragmentation

- **WHEN** two users describe the same skill differently
- **THEN** the controlled vocabulary maps them to one searchable entry

### Requirement: Visibility

Users SHALL control who can see each part of their profile, choosing at minimum between visible to anyone permitted to browse the directory, visible to server members only, and hidden.

Contact details MUST default to the most restrictive setting.

#### Scenario: Hidden profile is not discoverable

- **WHEN** a user hides their profile
- **THEN** it does not appear in directory browsing or search results

#### Scenario: Contact details default to restricted

- **WHEN** a user creates a profile without adjusting visibility
- **THEN** their contact details are not publicly visible

#### Scenario: Visibility applies consistently

- **WHEN** a profile field is hidden
- **THEN** it is absent from every interface and response, not merely hidden in one view

### Requirement: Availability

Users SHALL be able to state whether they are open to work, and to describe the kind of engagement they are seeking.

Availability MUST be able to expire, so a stale profile does not misrepresent someone indefinitely.

#### Scenario: Availability is searchable

- **WHEN** a client filters the directory by availability
- **THEN** only users currently indicating availability are returned

#### Scenario: Stale availability expires

- **WHEN** a user's stated availability reaches its expiry
- **THEN** they are no longer returned as available until they restate it

### Requirement: Verification

The server SHALL support marking a profile or a claim on it as verified by a moderator, and MUST distinguish verified from self-asserted information.

Verification MUST be revocable and MUST be recorded in the audit log.

#### Scenario: Verified status is distinguishable

- **WHEN** a profile carries a verified claim
- **THEN** viewers can distinguish it from a self-asserted claim

#### Scenario: Verification is auditable and revocable

- **WHEN** a moderator grants or revokes verification
- **THEN** the action is recorded in the audit log with the actor and reason

#### Scenario: Editing a verified claim clears verification

- **WHEN** a user edits a claim that had been verified
- **THEN** the verification is cleared pending re-verification

### Requirement: Browsing and search

Users permitted to access the directory SHALL be able to browse and search it by skill, availability, location, and company.

Results MUST respect each profile's visibility, and the directory MUST NOT be usable to enumerate every account on the server.

#### Scenario: Search respects visibility

- **WHEN** a user searches the directory
- **THEN** results exclude hidden profiles and hidden fields

#### Scenario: Enumeration is resisted

- **WHEN** a client issues directory queries at an abusive rate or pattern
- **THEN** the server rate-limits and does not permit exhaustive enumeration of accounts

### Requirement: Portability

Users SHALL be able to export their own profile in a documented, machine-readable format, and to delete it without deleting their account.

#### Scenario: Profile is exportable

- **WHEN** a user exports their profile
- **THEN** they receive its content in a documented machine-readable format

#### Scenario: Profile deletion preserves the account

- **WHEN** a user deletes their profile
- **THEN** the profile is removed from the directory
- **AND** their account and message history remain intact
