## Purpose

Defines how TDC decides what an authenticated actor is allowed to do, through roles and granular permissions scoped to a server, a channel, or an individual resource, so operators can delegate moderation and administration without granting unrelated power.

## ADDED Requirements

### Requirement: Permissions are granular and named

Authority SHALL be expressed as named, individually grantable permissions rather than coarse account types. Every action that changes state or reveals non-public information MUST be governed by a named permission.

#### Scenario: Every guarded action names its permission

- **WHEN** a request is denied for insufficient authority
- **THEN** the response names the permission that would have allowed it

#### Scenario: Permissions are enumerable

- **WHEN** an administrator inspects the permission catalogue
- **THEN** every permission is listed with a description of what it allows

### Requirement: Roles group permissions

The server SHALL support roles that bundle permissions and can be assigned to users. Operators MUST be able to create, modify, and delete roles, and a user MUST be able to hold more than one role.

Deleting a role MUST NOT delete the users holding it.

#### Scenario: Role change takes effect promptly

- **WHEN** a permission is removed from a role
- **THEN** users holding only that role lose the ability to perform the corresponding action without needing to sign in again

#### Scenario: Multiple roles combine

- **WHEN** a user holds two roles
- **THEN** the user's effective permissions are the union of both roles' grants, subject to explicit denials

#### Scenario: Deleting a role preserves users

- **WHEN** a role held by users is deleted
- **THEN** those users remain, having lost only the permissions that role granted

### Requirement: Scoped authority

Permissions SHALL be grantable at server scope and at channel scope. A grant at channel scope MUST NOT confer authority in any other channel.

Where scopes conflict, the more specific scope governs, and an explicit denial MUST override a grant at a broader scope.

#### Scenario: Channel moderator has no server authority

- **WHEN** a user is granted moderation permissions in one channel
- **THEN** they can moderate that channel
- **AND** they cannot moderate any other channel or change server settings

#### Scenario: Specific denial overrides broad grant

- **WHEN** a user holds a server-scope grant and an explicit channel-scope denial for the same permission
- **THEN** the action is denied in that channel and allowed elsewhere

### Requirement: Enforcement is server-side

Authorization SHALL be enforced by the server on every request. Client behaviour, including hiding interface elements, MUST NOT be relied upon for enforcement.

#### Scenario: Direct request without authority is denied

- **WHEN** a client issues a request for an action the user lacks permission to perform, bypassing the user interface
- **THEN** the server denies the request

#### Scenario: Hidden does not mean unenforced

- **WHEN** an interface element is hidden because the user lacks a permission
- **THEN** the corresponding server-side action is independently denied for that user

### Requirement: Administrator floor

There SHALL always be at least one account able to administer the server. Any change that would leave the server with no such account MUST be refused.

Administrative permissions MUST be individually revocable rather than tied irrevocably to an account.

#### Scenario: Last administrator cannot be demoted

- **WHEN** an action would remove administrative authority from the last account holding it
- **THEN** the action is refused with an explanation

#### Scenario: Administrator actions are recorded

- **WHEN** an account exercises an administrative permission
- **THEN** the action is written to the audit log with the actor, target, and permission used

### Requirement: Authority over one's own content

Users SHALL be able to edit and delete their own messages, and to manage their own profile and sessions, without holding moderation permissions.

An operator MUST be able to restrict self-editing and self-deletion windows by policy.

#### Scenario: User edits own message

- **WHEN** a user edits a message they authored, within any configured window
- **THEN** the edit succeeds and is recorded as an edit

#### Scenario: User cannot edit another's message

- **WHEN** a user attempts to edit a message authored by someone else without moderation permission
- **THEN** the request is denied

#### Scenario: Policy window is enforced

- **WHEN** an operator configures a time limit for self-deletion and a user attempts deletion after it
- **THEN** the request is denied with the policy explained

### Requirement: Guest and unauthenticated access

The server SHALL default to requiring authentication for all non-public endpoints. Any capability an operator chooses to expose without authentication MUST be explicitly enabled and clearly documented as public.

#### Scenario: Unauthenticated request is denied by default

- **WHEN** an unauthenticated request is made to a resource the operator has not explicitly made public
- **THEN** the request is denied

#### Scenario: Public exposure is deliberate

- **WHEN** an operator makes a channel publicly readable
- **THEN** the setting is recorded in the audit log and shown clearly in the channel's settings
