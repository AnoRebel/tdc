-- When somebody in a call began recording it on their own device, while they
-- are. Set by the server before their client starts, so everybody in the call
-- is told first; cleared when they stop, leave or drop.
ALTER TABLE call_participants ADD COLUMN recording_since TIMESTAMPTZ;
