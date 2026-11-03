## Why

Roles and permissions could be managed only through the API. Setting up a moderator meant using curl, and an operator who did not would leave every permission with the one person who set the server up. The user guide listed it under "What is not here yet". The rules are not obvious, either: Member is a baseline any other role overrides, a denial beats every grant, and administration implies everything not denied. Getting a role right meant knowing all three.

## What Changes

- **A roles screen** in the shared layer, so the web and desktop clients both have it. It is reached from settings by holders of `role.manage` or `role.assign`. It lists every role with its holder count and a summary of what it allows, denies and says for particular channels. A role can be created, renamed and described, and deleted after a confirmation that says who holds it and what they lose. Built-in roles cannot be deleted and keep their names.
- **A role editor.** Every permission in the server's catalogue is shown, grouped by area and described in the reader's language. Each is set to allow, deny or unset, with what the choice will mean under the server's rules shown beside it. Holders are added by searching for them and removed from the same list.
- **What a person may do.** Opened from a role, from the people list, or by search. It lists every permission, server-wide or in one chosen channel, with the role entries that decided it and any baseline the rules set aside. The person's roles can be given and taken away there.
- **Who may do what in a channel**, in the channel's settings: each role's entries for that channel alone.
- **Server additions.** Role list entries now carry their holder count and every entry with its scope. New routes: rename or describe a role (`PATCH /api/admin/roles/:id`), clear an entry (`DELETE /api/admin/roles/:id/permissions/:permission`), list a role's holders (`GET /api/admin/roles/:id/members`), and explain a person's permissions (`GET /api/admin/users/:id/permissions`). Reading roles and the catalogue is open to `role.assign` holders as well as `role.manage`.
- **The administrator floor counts who would keep administration.** It used to count everybody holding it. So withdrawing it from a role every administrator shared, deleting such a role, or denying it on another role they all held went through, and left nobody able to administer the server.

## Capabilities

### Modified Capabilities

- `identity/authorization`: roles administered from the interface, unset as a state distinct from denied, explainable effective permissions, and an administrator floor that holds for a shared role.

## Impact

- Server: `internal/authz` (floor, clear, rename, explanation, decision basis), `internal/httpapi` role routes, new queries in `roles.sql`, an audit action `role.updated`.
- Clients: pages under `packages/ui/app/pages/roles/`, components under `components/roles/`, the channel settings panel, settings and people links, about 160 strings in each locale.
- Docs: hosting (setting up a moderator from the screen), using TDC, integrating.
