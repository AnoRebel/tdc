/*
Whether a project's stream alone describes it.

Before every change to a project was an event, some were not: its labels, its
priority scale, a milestone's description. A project made then has a history
that leaves those out, and replaying it would not arrive at what the project
holds. Such a project is given one baseline event — its rows as they stand,
signed by this server — and from then on its stream is complete. This column
says which projects have one, or never needed one.

Every project already here starts incomplete; every project made from now on
starts complete, because everything about it is an event from its first.
*/
ALTER TABLE projects ADD COLUMN stream_complete BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE projects ALTER COLUMN stream_complete SET DEFAULT TRUE;
