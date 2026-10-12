Scope note: these tasks implement the "Roles are administered from the interface" and "Effective permissions are explainable" requirements of `identity/authorization`, and the shared-role scenario of its administrator floor. The canonical spec is edited directly.

**Documentation rule: documentation is written as the work is done.**

## 1. Server

- [x] 1.1 Role list carries holder counts and every entry with its scope (`server`, `channel`, `project`) and scope name; verify a channel grant is listed with its channel
- [x] 1.2 `PATCH /api/admin/roles/:id` renames and describes; a built-in role keeps its name; a name another role has is refused; audited as `role.updated`
- [x] 1.3 `DELETE /api/admin/roles/:id/permissions/:permission` clears an entry, server-wide or with `?channel=`; clearing an administrative server-wide grant is checked as a withdrawal; verify clearing a denial gives the decision back to Member
- [x] 1.4 `GET /api/admin/roles/:id/members`, bounded to 500 with `truncated`
- [x] 1.5 `GET /api/admin/users/:id/permissions`, every permission decided as the server decides it, with its basis and the entries behind it; verify the explanation of a channel denial matches a refused post
- [x] 1.6 Administrator floor counts who would keep administration without the role being changed; verify withdrawing from a shared role, denying on a role all administrators hold, and deleting a shared custom role are each refused; sabotage-verified
- [x] 1.7 Reading roles and the catalogue opened to `role.assign`; verify an assigner can hand out a role and cannot change one
- [x] 1.8 Routes documented in the API catalogue and the OpenAPI bodies regenerated

## 2. Clients

- [x] 2.1 Grouping, entry state, consequence and explanation rules in `lib/roles.ts`, with tests
- [x] 2.2 Roles list with create, holder counts and summaries, the refusal and loading states; linked from settings
- [x] 2.3 Role editor: details, delete with what is lost, three-state permissions with consequences, channel entries, holders with search; changes shown at once and put back on refusal; the self-lockout question asked and the change resent only on yes
- [x] 2.4 A person's permissions, server-wide or in a channel, with deciding and set-aside entries; their roles given and taken away; linked from the people list
- [x] 2.5 Who may do what in a channel, in the channel's settings, loaded only for `role.manage`
- [x] 2.6 Every string in English and Swahili, permission descriptions included, with the server's English as the fallback for a permission added later
- [x] 2.7 Checked in a browser: a moderator role made, given and found to take effect for its holder; a channel denial refusing a post

## 3. Documentation

- [x] 3.1 Hosting: setting up a moderator from the screen, with the API notes kept
- [x] 3.2 Using TDC: deciding who may do what, and the item removed from "What is not here yet"
- [x] 3.3 Integrating: the role and permission endpoints
