Scope note: these tasks implement the "Account names" requirement in `identity/authentication`. Tasks reference the spec for what to build and `design.md` for how.

**Documentation rule: documentation is written as the work is done.**

## 1. Server

- [x] 1.1 `username_history` and `user_named(text)`; `GetUserByUsername` and the directory lookups resolve former names through it
- [x] 1.2 Display name rules (`identity.ValidateDisplayName`), applied at registration and on change; OAuth provider names used only when they pass
- [x] 1.3 `accounts.username_changes`, `accounts.username_change_days`, `accounts.username_hold_days`; held names refused at registration in `identity.CreateUser`
- [x] 1.4 `PUT /api/me/display-name`, `GET /api/me/username`, `GET /api/me/username/availability`, `PUT /api/me/username`: password or recent sign-in, tokens refused, rate limited, refused in federated rooms, transactional under name and account locks, audited with both names
- [x] 1.5 `formerAddresses` on `GET /api/me`; `formerUsernames` on the administrators' list of people
- [x] 1.6 API catalogue entries and the regenerated OpenAPI bodies
- [x] 1.7 API tests against the database for each guard, with the important ones sabotage-checked

## 2. Clients

- [x] 2.1 "Your name" in settings: display name form, username with debounced availability, confirmation step stating the consequences and asking for proof
- [x] 2.2 Messages sent under a former address are the person's own in the chat
- [x] 2.3 A profile opened by a former name moves to the current one
- [x] 2.4 Former names beside each account in the administrators' list; the setting group in server settings
- [x] 2.5 Every string in English and Swahili
- [x] 2.6 The desktop application gets all of it through the layer, over its own transport

## 3. Documentation

- [x] 3.1 User guide: how to change names and what changes; removed from "What is not here yet"
- [x] 3.2 Hosting guide: the three settings
- [x] 3.3 Integration guide: the endpoints and their answers
