/*
Calls carried by the forwarding unit.

A call is a mesh — everybody sends to everybody — until it grows past the mesh
limit on a server whose forwarding unit is on; then it is forwarded, and stays
forwarded until it ends. Its forwarding runs on one node, the one named here,
because every participant's media must meet in one process; participants
connected to other nodes reach it through the nodes' private channel for
negotiation and directly, by UDP, for media.
*/
ALTER TABLE calls
    ADD COLUMN mode TEXT NOT NULL DEFAULT 'mesh' CHECK (mode IN ('mesh', 'sfu')),
    ADD COLUMN forwarding_node TEXT;

/*
calls.max_participants used to be the mesh's size, since a mesh was all there
was. It is now the cap on any call, and calls.mesh_limit the size a mesh may
reach; a server that had chosen a size keeps it as its mesh limit, so nothing
it allowed before is refused now.
*/
INSERT INTO server_config (key, value, updated_by)
SELECT 'calls.mesh_limit', value, updated_by FROM server_config WHERE key = 'calls.max_participants'
ON CONFLICT (key) DO NOTHING;
