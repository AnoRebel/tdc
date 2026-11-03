## 1. Server

- [x] 1.1 `scope` on subscriptions (channel, project, server), room optional for the server; migration 000427
- [x] 1.2 The catalogue, with groups, scopes and permissions, published with availability per caller
- [x] 1.3 Every stored event queued from the rooms and projects services, after commit; joins, leaves and channel creation now delivered
- [x] 1.4 Derived types: `issue.commented`, `issue.closed`, `issue.reopened`; the state change records `wasCompleted`
- [x] 1.5 `account.created` and `account.role_changed` for `user.read.all`, checked at queueing and at sending
- [x] 1.6 Applications may own subscriptions, held to their channels, project memberships and grant
- [x] 1.7 Tests for each, with the visibility guards sabotaged to show the tests catch them

## 2. Clients

- [x] 2.1 Settings → Integrations offers the three scopes and lists types grouped and described, in English and Swahili

## 3. Documentation

- [x] 3.1 The event catalogue, payload examples and required permissions in docs/webhooks-and-apps.md; docs/integrating.md points to it
