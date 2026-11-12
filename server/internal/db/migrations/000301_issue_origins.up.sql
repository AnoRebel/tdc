/*
Finding the issues made from a room's messages.

An issue made from a message records the message as a reference in its own
creation event, and nothing is written to the message or its room: the room's
members may not be able to see the project, and a note in their conversation
naming an issue in a private project would tell them it exists. So the other
direction — "which of this room's messages became issues" — is answered by
looking the reference up, per reader, and this index is what makes that a
lookup rather than a scan of every project's stream.
*/
CREATE INDEX events_issue_origin_idx ON events ((content->>'originChannel'), sequence)
    WHERE type = 'issue.created';
