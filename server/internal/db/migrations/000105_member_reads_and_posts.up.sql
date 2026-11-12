/*
Reading a channel and posting in it become permissions the Member role holds.

Both were in the permission catalogue and neither was checked for people —
only applications were held to them — so a role without them could still post
anywhere it was a member, and an operator could not make a channel read-only
for anybody. They are now enforced, which would refuse every ordinary member on
every existing server unless the role everybody holds is given them first. This
is that: the built-in Member role gains both at server scope, as new servers'
Member role now does from creation.

An operator who wants an announcement channel withdraws channel.post from the
role for that channel alone, which the channel-scoped grant already allows.
*/
INSERT INTO role_permissions (role_id, permission, scope_id, granted)
SELECT id, permission, NULL, TRUE
FROM roles
CROSS JOIN (VALUES ('channel.read'), ('channel.post')) AS added(permission)
WHERE roles.name = 'Member' AND roles.built_in
ON CONFLICT (role_id, permission, COALESCE(scope_id, '00000000-0000-0000-0000-000000000000'::uuid))
DO NOTHING;
