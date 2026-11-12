/*
Since when a replica's home server has not answered, or NULL while it does.

A replica asks its home for anything it missed every minute, so the answer
doubles as a check on the home itself. Readers are told when it stopped
answering: what they see may be behind, and a change they make is carried
there and cannot be made until it answers again.
*/
ALTER TABLE projects ADD COLUMN home_unreachable_since TIMESTAMPTZ;
